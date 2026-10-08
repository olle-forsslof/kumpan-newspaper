package kp

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type workerReporterFunc func(context.Context, Article) (Generated, error)

func (f workerReporterFunc) Generate(ctx context.Context, a Article) (Generated, error) {
	return f(ctx, a)
}

type workerMessengerFunc func(context.Context, string, string) error

func (f workerMessengerFunc) Send(ctx context.Context, recipient, text string) error {
	return f(ctx, recipient, text)
}

func workerTestDeadline(t *testing.T, ctx context.Context, limit time.Duration) {
	t.Helper()
	d, ok := ctx.Deadline()
	if !ok || time.Until(d) > limit || time.Until(d) < limit-time.Second {
		t.Fatalf("deadline %v, want approximately %v", d, limit)
	}
}

func TestWorkerGeneration(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "invalid", "removed-success", "removed-failure"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestSubmit(t, s, "job")
			var logs bytes.Buffer
			calls := 0
			w := NewWorker(s, workerReporterFunc(func(ctx context.Context, job Article) (Generated, error) {
				calls++
				workerTestDeadline(t, ctx, 2*time.Minute)
				if job.ID != a.ID || job.Status != StatusProcessing {
					t.Fatalf("unexpected claim: %+v", job)
				}
				if strings.HasPrefix(outcome, "removed") {
					if err := s.RemoveArticle(job.ID, job.Revision); err != nil {
						t.Fatal(err)
					}
				}
				if strings.HasSuffix(outcome, "failure") {
					return Generated{}, errors.New("SECRET external error and original text")
				}
				if outcome == "invalid" {
					return Generated{}, nil
				}
				return Generated{Headline: "Headline", Body: "Body"}, nil
			}), nil, WorkerConfig{}, slog.New(slog.NewTextHandler(&logs, nil)))
			if err := w.process(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetArticle(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := StatusReady
			if outcome == "failure" || outcome == "invalid" {
				want = StatusFailed
			}
			if strings.HasPrefix(outcome, "removed") {
				want = StatusRemoved
			}
			if got.Status != want {
				t.Fatalf("status %s, want %s", got.Status, want)
			}
			if err := w.process(context.Background()); err != nil || calls != 1 {
				t.Fatalf("automatic retry: calls %d, err %v", calls, err)
			}
			if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), a.Original) || strings.Contains(got.Error, "SECRET") {
				t.Fatal("external content leaked")
			}
		})
	}
}

func TestWorkerReporterDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, response, want string
		status               int
	}{
		{"authentication", `{"error":{"code":"invalid_api_key","message":"PRIVATE secret-key"}}`, "api_code=invalid_api_key", 401},
		{"quota", `{"error":{"code":"insufficient_quota","message":"PRIVATE original question"}}`, "api_code=insufficient_quota", 429},
		{"schema", `{"error":{"code":"invalid_json_schema","param":"text.format.schema","message":"PRIVATE original question"}}`, "api_param=text.format.schema", 400},
		{"untrusted fields", `{"error":{"code":"PRIVATE","param":"PRIVATE","message":"PRIVATE"}}`, "api_code=unknown", 400},
		{"bad generated JSON", "", `reason="invalid reporter JSON"`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestSubmit(t, s, "PRIVATE original question")
			reporter := testReporter(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.status == 200 {
					reporterResponse(w, "completed", "not JSON")
					return
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.response))
			})
			var logs bytes.Buffer
			worker := NewWorker(s, reporter, nil, WorkerConfig{}, slog.New(slog.NewTextHandler(&logs, nil)))
			if err := worker.process(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), tc.want) {
				t.Fatalf("missing diagnostic %q in %s", tc.want, logs.String())
			}
			if strings.Contains(logs.String(), "PRIVATE") || strings.Contains(logs.String(), "secret-key") || strings.Contains(logs.String(), "explicit-test-key") {
				t.Fatal("private content leaked into logs")
			}
			got, err := s.GetArticle(a.ID)
			if err != nil || got.Status != StatusFailed || strings.Contains(got.Error, "PRIVATE") {
				t.Fatalf("unsafe persisted failure: %+v, %v", got, err)
			}
		})
	}
}

func TestWorkerCancellationAndStartupRecovery(t *testing.T) {
	s, path := storeTestOpen(t)
	a := storeTestSubmit(t, s, "job")
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWorker(s, workerReporterFunc(func(ctx context.Context, _ Article) (Generated, error) {
		cancel()
		<-ctx.Done()
		return Generated{Headline: "Late", Body: "Late"}, nil
	}), nil, WorkerConfig{}, nil)
	w.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
	got, err := s.GetArticle(a.ID)
	if err != nil || got.Status != StatusProcessing {
		t.Fatalf("canceled claim: %+v, %v", got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	abandonedRevision := got.Revision
	w = NewWorker(s, workerReporterFunc(func(_ context.Context, job Article) (Generated, error) {
		if job.Revision <= abandonedRevision {
			t.Fatal("startup did not recover abandoned claim")
		}
		return Generated{Headline: "Recovered", Body: "Body"}, nil
	}), nil, WorkerConfig{}, nil)
	w.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	finished := make(chan error, 1)
	go func() { finished <- w.Run(ctx) }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			got, err = s.GetArticle(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status == StatusReady {
				cancel()
				if err := <-finished; !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown: %v", err)
				}
				return
			}
		case <-deadline.C:
			cancel()
			<-finished
			t.Fatal("recovered article did not complete")
		}
	}
}

func TestWorkerNotificationRetry(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "ready")
	i, err := s.Publish(a.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	calls := 0
	w := NewWorker(s, nil, workerMessengerFunc(func(ctx context.Context, recipient, text string) error {
		workerTestDeadline(t, ctx, 10*time.Second)
		calls++
		if recipient != "Cpublish" || !strings.HasSuffix(text, "https://example.test/issues/1") {
			t.Fatalf("message: %q %q", recipient, text)
		}
		if calls == 1 {
			return errors.New("SECRET")
		}
		return nil
	}), WorkerConfig{BaseURL: "https://example.test/", PublishChannel: "Cpublish"}, nil)
	w.now = func() time.Time { return now }
	if err := w.maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingNotification(); err != nil || pending.ID != i.ID {
		t.Fatalf("failed send acknowledged: %+v, %v", pending, err)
	}
	now = now.Add(time.Minute - time.Nanosecond)
	if err := w.maintain(context.Background()); err != nil || calls != 1 {
		t.Fatalf("early retry: %d, %v", calls, err)
	}
	now = now.Add(time.Nanosecond)
	if err := w.maintain(context.Background()); err != nil || calls != 2 {
		t.Fatalf("retry: %d, %v", calls, err)
	}
	if _, err := s.PendingNotification(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("successful send not acknowledged: %v", err)
	}
	now = now.Add(time.Minute)
	if err := w.maintain(context.Background()); err != nil || calls != 2 {
		t.Fatalf("duplicate acknowledged notification: %d, %v", calls, err)
	}
}

func TestWorkerRunImmediateMaintenance(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "ready")
	if _, err := s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	w := NewWorker(s, nil, workerMessengerFunc(func(context.Context, string, string) error {
		calls++
		cancel()
		return nil
	}), WorkerConfig{PublishChannel: "Cpublish"}, nil)
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("initial maintenance: calls %d, err %v", calls, err)
	}
	if _, err := s.PendingNotification(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("acknowledged send not persisted: %v", err)
	}
}

func TestWorkerFridaySchedule(t *testing.T) {
	for _, tc := range []struct {
		utc  string
		want int
	}{
		{"2026-03-27T07:59:59Z", 0}, // Before spring DST: Stockholm is UTC+1.
		{"2026-03-27T08:00:00Z", 1},
		{"2026-04-03T06:59:59Z", 0}, // After spring DST: Stockholm is UTC+2.
		{"2026-04-03T07:00:00Z", 1},
		{"2026-10-23T07:00:00Z", 1},
		{"2026-10-30T07:59:59Z", 0}, // After autumn DST: UTC+1 again.
		{"2026-10-30T08:00:00Z", 1},
		{"2026-10-30T22:59:59Z", 1}, // Late Friday catchup.
		{"2026-10-30T23:00:00Z", 0}, // Saturday locally.
	} {
		t.Run(tc.utc, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			now, err := time.Parse(time.RFC3339, tc.utc)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			w := NewWorker(s, nil, workerMessengerFunc(func(_ context.Context, recipient, text string) error {
				calls++
				if recipient != "Ueditor" || !strings.Contains(text, "https://example.test/draft") {
					t.Fatalf("reminder: %q %q", recipient, text)
				}
				return nil
			}), WorkerConfig{BaseURL: "https://example.test", EditorIDs: []string{"Ueditor"}}, nil)
			w.now = func() time.Time { return now }
			if err := w.maintain(context.Background()); err != nil || calls != tc.want {
				t.Fatalf("reminders %d, want %d, err %v", calls, tc.want, err)
			}
			if _, err := s.LatestPublished(); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("worker published: %v", err)
			}
		})
	}
}

func TestWorkerRemindersPerRecipientAcrossRestart(t *testing.T) {
	s, path := storeTestOpen(t)
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	calls := map[string]int{}
	messenger := workerMessengerFunc(func(ctx context.Context, recipient, _ string) error {
		workerTestDeadline(t, ctx, 10*time.Second)
		calls[recipient]++
		if recipient == "U2" && calls[recipient] == 1 {
			return errors.New("delivery failed")
		}
		return nil
	})
	cfg := WorkerConfig{BaseURL: "https://example.test", EditorIDs: []string{"U1", "U2"}}
	w := NewWorker(s, nil, messenger, cfg, nil)
	w.now = func() time.Time { return now }
	if err := w.maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(59 * time.Second)
	if err := w.maintain(context.Background()); err != nil || calls["U2"] != 1 {
		t.Fatalf("early reminder retry: %v, %v", calls, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now = now.Add(time.Second)
	w = NewWorker(s, nil, messenger, cfg, nil)
	w.now = func() time.Time { return now }
	if err := w.maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := w.maintain(context.Background()); err != nil || calls["U1"] != 1 || calls["U2"] != 2 {
		t.Fatalf("recipient dedupe: %v, %v", calls, err)
	}
	if _, err := s.LatestPublished(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reminder published draft: %v", err)
	}
}

func TestWorkerSkipsReminderAfterFridayPublication(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "ready")
	// Set the publication timestamp directly before freezing the issue.
	if _, err := s.DB.Exec("UPDATE kp_issues SET published_at = ? WHERE id = ?", time.Date(2026, 10, 30, 23, 0, 0, 0, time.FixedZone("Stockholm", 3600)), a.IssueID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNotified(a.IssueID); err != nil {
		t.Fatal(err)
	}
	storeTestSubmit(t, s, "new draft")
	w := NewWorker(s, nil, workerMessengerFunc(func(context.Context, string, string) error {
		t.Fatal("reminded after Friday publication")
		return nil
	}), WorkerConfig{EditorIDs: []string{"U1"}}, nil)
	w.now = func() time.Time { return time.Date(2026, 10, 30, 22, 30, 0, 0, time.UTC) }
	if err := w.maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerDatabaseErrorsPropagate(t *testing.T) {
	for _, stage := range []string{"startup", "claim", "complete", "fail", "notification", "reminder"} {
		t.Run(stage, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			w := NewWorker(s, nil, nil, WorkerConfig{}, nil)
			w.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
			var err error
			switch stage {
			case "startup":
				s.Close()
				err = w.Run(context.Background())
			case "claim":
				s.Close()
				err = w.process(context.Background())
			case "complete", "fail":
				storeTestSubmit(t, s, "job")
				w.reporter = workerReporterFunc(func(context.Context, Article) (Generated, error) {
					s.Close()
					if stage == "fail" {
						return Generated{}, errors.New("external failure")
					}
					return Generated{Headline: "H", Body: "B"}, nil
				})
				err = w.process(context.Background())
			case "notification":
				a := storeTestReady(t, s, "ready")
				if _, err := s.Publish(a.IssueID); err != nil {
					t.Fatal(err)
				}
				w.messenger = workerMessengerFunc(func(context.Context, string, string) error { s.Close(); return nil })
				err = w.maintain(context.Background())
			case "reminder":
				w.now = func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
				w.cfg.EditorIDs = []string{"U1"}
				w.messenger = workerMessengerFunc(func(context.Context, string, string) error { s.Close(); return nil })
				err = w.maintain(context.Background())
			}
			if err == nil {
				t.Fatal("database error swallowed")
			}
		})
	}
}
