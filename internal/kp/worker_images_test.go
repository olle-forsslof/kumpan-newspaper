package kp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type workerPhotoService struct {
	search func(context.Context, string, string) (Photo, error)
	track  func(context.Context, Photo) error
}

func (p workerPhotoService) Search(ctx context.Context, query, exclude string) (Photo, error) {
	return p.search(ctx, query, exclude)
}

func (p workerPhotoService) Track(ctx context.Context, photo Photo) error {
	return p.track(ctx, photo)
}

func TestWorkerImages(t *testing.T) {
	for _, outcome := range []string{"success", "search failure", "tracking failure", "invalid photo", "removed", "published", "edited", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestReady(t, s, "PRIVATE original")
			if err := s.QueueImage(a.ID, a.Revision, "open window office"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			searches, tracks := 0, 0
			service := workerPhotoService{
				search: func(ctx context.Context, query, exclude string) (Photo, error) {
					searches++
					if query != "open window office" || exclude != "" {
						t.Fatalf("unexpected search %q %q", query, exclude)
					}
					current := storeTestGet(t, s, a.ID)
					switch outcome {
					case "search failure":
						return Photo{}, errors.New("PRIVATE provider payload")
					case "invalid photo":
						photo := storeTestPhoto()
						photo.DownloadURL = "https://private.example/download"
						return photo, nil
					case "removed":
						if err := s.RemoveImage(a.ID, current.Revision); err != nil {
							t.Fatal(err)
						}
					case "published":
						if _, err := s.Publish(a.IssueID); err != nil {
							t.Fatal(err)
						}
					case "edited":
						if err := s.SaveArticle(a.ID, current.Revision, Generated{Headline: "Edited", Body: "Text"}); err != nil {
							t.Fatal(err)
						}
					case "canceled":
						cancel()
					}
					return storeTestPhoto(), nil
				},
				track: func(ctx context.Context, photo Photo) error {
					tracks++
					if photo.ID != "photo-id" {
						t.Fatal("wrong photo tracked")
					}
					if outcome == "tracking failure" {
						return errors.New("PRIVATE access key")
					}
					return nil
				},
			}
			var logs bytes.Buffer
			worker := NewWorker(s, nil, nil, WorkerConfig{Photos: service}, slog.New(slog.NewTextHandler(&logs, nil)))
			err := worker.processImage(ctx)
			if outcome == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got := storeTestGet(t, s, a.ID)
			want, wantTracks := StatusFailed, 0
			switch outcome {
			case "success":
				want, wantTracks = StatusReady, 1
			case "tracking failure":
				wantTracks = 1
			case "removed":
				want = StatusRemoved
			case "published", "canceled":
				want = StatusProcessing
			case "edited":
				want = StatusPending
			}
			if got.Status != StatusReady || got.ImageStatus != want || tracks != wantTracks || searches != 1 {
				t.Fatalf("outcome=%s image=%s text=%s searches=%d tracks=%d", outcome, got.ImageStatus, got.Status, searches, tracks)
			}
			if (got.Photo != nil) != (outcome == "success") {
				t.Fatal("untracked photo attached")
			}
			if strings.Contains(logs.String(), "PRIVATE") {
				t.Fatal("private provider information logged")
			}
			if outcome != "canceled" && outcome != "edited" {
				if err := worker.processImage(ctx); err != nil || searches != 1 {
					t.Fatal("unnecessary repeated lookup", err)
				}
			}
		})
	}
}

func TestWorkerImageReplacementKeepsPreviousPhotoOnFailure(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "Article")
	if err := s.QueueImage(a.ID, a.Revision, "coffee cups"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImage(job.ID, job.Revision, storeTestPhoto()); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if err := s.QueueImage(a.ID, a.Revision, "coffee cups"); err != nil {
		t.Fatal(err)
	}
	service := workerPhotoService{
		search: func(_ context.Context, query, exclude string) (Photo, error) {
			if exclude != "photo-id" || query != "coffee cups" {
				t.Fatal("previous photo was not excluded")
			}
			return Photo{}, ErrNoPhoto
		},
		track: func(context.Context, Photo) error { t.Fatal("no result must not be tracked"); return nil },
	}
	worker := NewWorker(s, nil, nil, WorkerConfig{Photos: service}, nil)
	if err := worker.processImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.Photo == nil || a.Photo.ID != "photo-id" || a.ImageStatus != StatusFailed || a.Status != StatusReady {
		t.Fatal("replacement lost the previous photo or failed the article")
	}
	if _, err := s.Publish(a.IssueID); err != nil {
		t.Fatal("image lookup failure blocked publication", err)
	}
}

func TestWorkerImagesDisabledAndRecovery(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "Article")
	if err := s.QueueImage(a.ID, a.Revision, "office desk"); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(s, nil, nil, WorkerConfig{}, nil)
	if err := worker.processImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if storeTestGet(t, s, a.ID).ImageStatus != StatusPending {
		t.Fatal("disabled service consumed a job")
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	service := workerPhotoService{
		search: func(context.Context, string, string) (Photo, error) { return storeTestPhoto(), nil },
		track:  func(context.Context, Photo) error { return nil },
	}
	worker.cfg.Photos = service
	if err := worker.processImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := storeTestGet(t, s, a.ID)
	if got.ImageStatus != StatusReady || got.Revision <= job.Revision || got.Photo == nil {
		t.Fatal("abandoned image lookup did not recover")
	}
}
