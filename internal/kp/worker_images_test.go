package kp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type workerImageService struct {
	generate func(context.Context, string, string) (ImageAsset, error)
	discard  func(ImageAsset) error
}

func (s workerImageService) Generate(ctx context.Context, kind, prompt string) (ImageAsset, error) {
	return s.generate(ctx, kind, prompt)
}

func (s workerImageService) Discard(asset ImageAsset) error { return s.discard(asset) }

func TestWorkerImages(t *testing.T) {
	for _, outcome := range []string{"success", "API error", "API error with asset", "invalid asset", "removed", "prompt changed", "published", "edited", "canceled", "cancel error", "database error", "failure database error", "cleanup error"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestReady(t, s, "PRIVATE original")
			const prompt = "PRIVATE description of colleagues sharing coffee beside an open office window."
			if err := s.QueueImage(a.ID, a.Revision, prompt); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			generations, discards := 0, 0
			asset := storeTestImage()
			service := workerImageService{
				generate: func(imageCtx context.Context, kind, gotPrompt string) (ImageAsset, error) {
					generations++
					if kind != a.Kind || gotPrompt != prompt {
						t.Fatalf("unexpected generation %q %q", kind, gotPrompt)
					}
					deadline, ok := imageCtx.Deadline()
					if remaining := time.Until(deadline); !ok || remaining > 4*time.Minute || remaining < 3*time.Minute {
						t.Fatal("image generation must have a four-minute deadline")
					}
					current := storeTestGet(t, s, a.ID)
					if current.ImageStatus != StatusProcessing || current.Status != StatusReady || current.Revision <= a.Revision {
						t.Fatal("generation did not claim a ready article")
					}
					switch outcome {
					case "API error":
						return ImageAsset{}, errors.New("PRIVATE provider payload")
					case "API error with asset":
						return asset, errors.New("PRIVATE provider payload")
					case "cancel error":
						cancel()
						return ImageAsset{}, context.Canceled
					case "invalid asset":
						return ImageAsset{Name: "../PRIVATE.jpg", Width: 1536, Height: 1024}, nil
					case "removed", "cleanup error":
						if err := s.RemoveImage(a.ID, current.Revision); err != nil {
							t.Fatal(err)
						}
					case "published":
						if _, err := s.Publish(a.IssueID); err != nil {
							t.Fatal(err)
						}
					case "prompt changed":
						if err := s.QueueImage(a.ID, current.Revision, "A different scene with colleagues walking in a park."); err != nil {
							t.Fatal(err)
						}
						changed := storeTestGet(t, s, a.ID)
						if changed.Revision != current.Revision+1 || changed.ImageRevision != current.ImageRevision+1 {
							t.Fatal("prompt change did not invalidate the image claim")
						}
					case "edited":
						if err := s.SaveArticle(a.ID, current.Revision, Generated{Headline: "Edited", Body: "Text"}); err != nil {
							t.Fatal(err)
						}
						edited := storeTestGet(t, s, a.ID)
						if edited.Revision != current.Revision+1 || edited.ImageRevision != current.ImageRevision || edited.ImageStatus != StatusProcessing || edited.ImagePrompt != prompt {
							t.Fatal("text-only edit invalidated the paid image claim")
						}
					case "canceled":
						cancel()
					case "database error":
						// A non-conflict completion error does not prove the asset is unattached.
						if _, err := s.DB.Exec(`CREATE TRIGGER reject_image_completion BEFORE UPDATE OF generated_image ON kp_articles BEGIN SELECT RAISE(ABORT, 'PRIVATE database error'); END`); err != nil {
							t.Fatal(err)
						}
					case "failure database error":
						if _, err := s.DB.Exec(`CREATE TRIGGER reject_image_failure BEFORE UPDATE OF image_status ON kp_articles WHEN NEW.image_status = 'failed' BEGIN SELECT RAISE(ABORT, 'PRIVATE database error'); END`); err != nil {
							t.Fatal(err)
						}
						return ImageAsset{}, errors.New("PRIVATE provider payload")
					}
					return asset, nil
				},
				discard: func(got ImageAsset) error {
					discards++
					if got != asset {
						t.Fatalf("discarded wrong asset: %+v", got)
					}
					if outcome == "cleanup error" {
						return errors.New("PRIVATE filesystem error")
					}
					return nil
				},
			}
			var logs bytes.Buffer
			worker := NewWorker(s, nil, nil, WorkerConfig{Images: service}, slog.New(slog.NewTextHandler(&logs, nil)))
			err := worker.processImage(ctx)
			switch outcome {
			case "canceled", "cancel error":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case "database error", "failure database error":
				if err == nil || errors.Is(err, ErrConflict) {
					t.Fatalf("expected non-conflict database error: %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			got := storeTestGet(t, s, a.ID)
			want, wantDiscards := StatusFailed, 0
			switch outcome {
			case "success", "edited", "canceled":
				want = StatusReady
			case "API error with asset":
				wantDiscards = 1
			case "removed", "cleanup error":
				want, wantDiscards = StatusRemoved, 1
			case "published":
				want, wantDiscards = StatusProcessing, 1
			case "prompt changed":
				want, wantDiscards = StatusPending, 1
			case "database error", "failure database error":
				want = StatusProcessing
			}
			if got.Status != StatusReady || got.ImageStatus != want || discards != wantDiscards || generations != 1 {
				t.Fatalf("image=%s text=%s generations=%d discards=%d", got.ImageStatus, got.Status, generations, discards)
			}
			if want == StatusReady {
				if got.Image == nil || *got.Image != asset {
					t.Fatal("generated asset not attached")
				}
			} else if got.Image != nil {
				t.Fatal("unattached asset stored")
			}
			if outcome == "edited" && (got.Headline != "Edited" || got.Body != "Text") {
				t.Fatal("image completion overwrote the text edit")
			}
			if strings.Contains(logs.String(), "PRIVATE") {
				t.Fatal("private generation information logged")
			}
			if outcome != "prompt changed" {
				if err := worker.processImage(context.Background()); err != nil || generations != 1 || discards != wantDiscards {
					t.Fatal("unnecessary paid retry or cleanup", err)
				}
			}
			if outcome == "canceled" || outcome == "cancel error" {
				if err := s.RecoverProcessing(); err != nil {
					t.Fatal(err)
				}
				if recovered := storeTestGet(t, s, a.ID); recovered.ImageStatus != want || recovered.ImageRevision != got.ImageRevision {
					t.Fatal("startup changed the persisted canceled attempt")
				}
				if err := worker.processImage(context.Background()); err != nil || generations != 1 || discards != 0 {
					t.Fatal("startup retried or discarded a canceled paid attempt", err)
				}
			}
			if outcome == "database error" || outcome == "failure database error" {
				if _, err := s.DB.Exec("DROP TRIGGER IF EXISTS reject_image_failure"); err != nil {
					t.Fatal(err)
				}
				if err := s.RecoverProcessing(); err != nil {
					t.Fatal(err)
				}
				if recovered := storeTestGet(t, s, a.ID); recovered.ImageStatus != StatusFailed || recovered.ImageRevision <= got.ImageRevision {
					t.Fatal("ambiguous database failure was not marked failed on startup")
				}
				if err := worker.processImage(context.Background()); err != nil || generations != 1 || discards != 0 {
					t.Fatal("startup retried an ambiguous paid attempt or discarded its artifact", err)
				}
			}
		})
	}
}

func TestWorkerImageReplacementKeepsPreviousImage(t *testing.T) {
	for _, outcome := range []string{"API error", "fresh canceled", "old canceled", "old conflict", "old API error"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestReady(t, s, "Article")
			previous := storeTestImage()
			if err := s.QueueImage(a.ID, a.Revision, "An office scene with colleagues sharing coffee."); err != nil {
				t.Fatal(err)
			}
			claim, err := s.ClaimNextImage()
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteImage(a.ID, claim.ImageRevision, previous); err != nil {
				t.Fatal(err)
			}
			a = storeTestGet(t, s, a.ID)
			const prompt = "Colleagues holding coffee cups beside a sunny office window."
			if err := s.QueueImage(a.ID, a.Revision, prompt); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			discards := 0
			fresh := previous
			fresh.Name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
			service := workerImageService{
				generate: func(_ context.Context, kind, gotPrompt string) (ImageAsset, error) {
					if kind != a.Kind || gotPrompt != prompt {
						t.Fatal("replacement prompt changed")
					}
					switch outcome {
					case "API error":
						return ImageAsset{}, errors.New("PRIVATE provider error")
					case "old API error":
						return previous, errors.New("PRIVATE provider error")
					case "fresh canceled":
						cancel()
						return fresh, nil
					case "old canceled":
						cancel()
					case "old conflict":
						current := storeTestGet(t, s, a.ID)
						if err := s.QueueImage(a.ID, current.Revision, "A new scene with colleagues sharing lunch."); err != nil {
							t.Fatal(err)
						}
					}
					return previous, nil
				},
				discard: func(asset ImageAsset) error {
					discards++
					if asset != fresh {
						t.Fatal("previous referenced image discarded")
					}
					return nil
				},
			}
			worker := NewWorker(s, nil, nil, WorkerConfig{Images: service}, nil)
			err = worker.processImage(ctx)
			if strings.HasSuffix(outcome, "canceled") {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got := storeTestGet(t, s, a.ID)
			want := StatusFailed
			wantAsset := previous
			if strings.HasSuffix(outcome, "canceled") {
				want = StatusReady
			}
			if outcome == "fresh canceled" {
				wantAsset = fresh
			}
			if outcome == "old conflict" {
				want = StatusPending
			}
			if got.Image == nil || *got.Image != wantAsset || got.ImageStatus != want || got.Status != StatusReady || discards != 0 {
				t.Fatalf("replacement attached or discarded the wrong image: %+v, discards=%d", got, discards)
			}
			if _, err := s.Publish(a.IssueID); err != nil {
				t.Fatal("image failure blocked publication", err)
			}
		})
	}
}

func TestWorkerImagesNilServiceAndRecovery(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "Article")
	if err := s.QueueImage(a.ID, a.Revision, "An empty office desk beside a large window."); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(s, nil, nil, WorkerConfig{}, nil)
	if err := worker.processImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if storeTestGet(t, s, a.ID).ImageStatus != StatusPending {
		t.Fatal("nil test service consumed a job")
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	recovered := storeTestGet(t, s, a.ID)
	if recovered.ImageStatus != StatusFailed || recovered.Revision != job.Revision+1 || recovered.ImageRevision != job.ImageRevision+1 {
		t.Fatal("abandoned paid claim was not marked failed")
	}
	generations := 0
	worker.cfg.Images = workerImageService{
		generate: func(context.Context, string, string) (ImageAsset, error) {
			generations++
			return storeTestImage(), nil
		},
		discard: func(ImageAsset) error { t.Fatal("attached image discarded"); return nil },
	}
	if err := worker.processImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if generations != 0 || storeTestGet(t, s, a.ID).ImageStatus != StatusFailed {
		t.Fatal("startup automatically retried a possibly billed image attempt")
	}
	if err := s.CompleteImage(job.ID, job.ImageRevision, storeTestImage()); !errors.Is(err, ErrConflict) {
		t.Fatalf("recovered claim accepted stale completion: %v", err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("recovered claim accepted stale failure: %v", err)
	}
	if err := s.QueueImage(a.ID, job.Revision, recovered.ImagePrompt); !errors.Is(err, ErrConflict) {
		t.Fatalf("manual retry accepted stale overall revision: %v", err)
	}
	if err := s.QueueImage(a.ID, recovered.Revision, recovered.ImagePrompt); err != nil {
		t.Fatal(err)
	}
	if err := worker.processImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := storeTestGet(t, s, a.ID)
	if generations != 1 || got.ImageStatus != StatusReady || got.ImageRevision <= recovered.ImageRevision || got.Image == nil {
		t.Fatal("explicit manual retry did not generate exactly once")
	}
	if err := worker.processImage(context.Background()); err != nil || generations != 1 {
		t.Fatal("completed manual retry generated again", err)
	}
}

func TestWorkerImageAlreadyCanceled(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "Article")
	if err := s.QueueImage(a.ID, a.Revision, "Colleagues sharing coffee beside an office window."); err != nil {
		t.Fatal(err)
	}
	queued := storeTestGet(t, s, a.ID)
	worker := NewWorker(s, nil, nil, WorkerConfig{Images: workerImageService{
		generate: func(context.Context, string, string) (ImageAsset, error) {
			t.Fatal("already canceled context started a paid request")
			return ImageAsset{}, nil
		},
		discard: func(ImageAsset) error { t.Fatal("no asset should be discarded"); return nil },
	}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.processImage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	got := storeTestGet(t, s, a.ID)
	if got.ImageStatus != StatusPending || got.Revision != queued.Revision || got.ImageRevision != queued.ImageRevision {
		t.Fatal("already canceled worker consumed an image claim")
	}
}

func TestWorkerImageDatabaseErrorPreservesPaidFilesOnRecovery(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "Article")
	if err := s.QueueImage(a.ID, a.Revision, "Colleagues sharing coffee beside an office window."); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	asset := storeTestImage()
	contents := webTestImageFile(t, directory, asset.Name)
	for _, width := range []int{400, 800} {
		webTestImageFile(t, directory, imageVariantName(asset.Name, width))
	}
	generations := 0
	worker := NewWorker(s, nil, nil, WorkerConfig{Images: workerImageService{
		generate: func(context.Context, string, string) (ImageAsset, error) {
			generations++
			if _, err := s.DB.Exec(`CREATE TRIGGER reject_paid_image BEFORE UPDATE OF generated_image ON kp_articles BEGIN SELECT RAISE(ABORT, 'PRIVATE database error'); END`); err != nil {
				t.Fatal(err)
			}
			return asset, nil
		},
		discard: func(ImageAsset) error { t.Fatal("ambiguous completion deleted paid files"); return nil },
	}}, nil)
	if err := worker.processImage(context.Background()); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("expected non-conflict completion error: %v", err)
	}
	claim := storeTestGet(t, s, a.ID)
	if claim.ImageStatus != StatusProcessing || claim.Image != nil {
		t.Fatal("database error unexpectedly completed or failed the claim")
	}
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	if err := worker.processImage(context.Background()); err != nil || generations != 1 {
		t.Fatal("startup retried an ambiguous paid request", err)
	}
	if got := storeTestGet(t, s, a.ID); got.ImageStatus != StatusFailed || got.Image != nil {
		t.Fatal("startup did not fail the abandoned claim")
	}
	for _, width := range []int{0, 400, 800} {
		got, err := os.ReadFile(filepath.Join(directory, imageVariantName(asset.Name, width)))
		if err != nil || !bytes.Equal(got, contents) {
			t.Fatalf("paid artifact changed on recovery: width=%d err=%v", width, err)
		}
	}
}
