package kp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	_ "time/tzdata"
)

type Reporter interface {
	Generate(context.Context, Article) (Generated, error)
}

type Messenger interface {
	Send(context.Context, string, string) error
}

type WorkerConfig struct {
	BaseURL, PublishChannel string
	EditorIDs               []string
	Photos                  PhotoService
}

// Worker runs serially in a single application instance. Failed articles require
// explicit editor retry. Delivery is at-least-once: a lost network acknowledgement
// or a crash before recording success can duplicate a message, not publication.
type Worker struct {
	store           *Store
	reporter        Reporter
	messenger       Messenger
	cfg             WorkerConfig
	logger          *slog.Logger
	now             func() time.Time
	nextMaintenance time.Time
}

func NewWorker(store *Store, reporter Reporter, messenger Messenger, cfg WorkerConfig, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{store: store, reporter: reporter, messenger: messenger, cfg: cfg, logger: logger, now: time.Now}
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.store.RecoverProcessing(); err != nil {
		return err
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.maintain(ctx); err != nil {
			return err
		}
		if err := w.process(ctx); err != nil {
			return err
		}
		if err := w.processImage(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) process(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := w.store.ClaimNext()
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	generateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	g, generateErr := w.reporter.Generate(generateCtx, *a)
	if generateErr == nil {
		generateErr = generateCtx.Err()
	}
	cancel()
	// Shutdown leaves the claim intact for recovery on the next startup.
	if err := ctx.Err(); err != nil {
		return err
	}
	category := "generation"
	if generateErr == nil {
		generateErr = ValidateGenerated(a.Kind, g)
		category = "validation"
	}
	if generateErr != nil {
		err = w.store.FailArticle(a.ID, a.Revision)
		if errors.Is(err, ErrConflict) {
			return nil
		}
		if err == nil {
			fields := []any{"article_id", a.ID, "category", category}
			var failure *reporterError
			if errors.As(generateErr, &failure) {
				fields = append(fields, "reason", failure.reason, "http_status", failure.status, "api_code", failure.code, "api_param", failure.param)
			}
			w.logger.Warn("article generation failed", fields...)
		}
		return err
	}
	err = w.store.CompleteArticle(a.ID, a.Revision, g)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

func (w *Worker) send(ctx context.Context, recipient, text string) error {
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.messenger.Send(sendCtx, recipient, text)
}

func (w *Worker) processImage(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.cfg.Photos == nil {
		return nil
	}
	a, err := w.store.ClaimNextImage()
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	excludeID := ""
	if a.Photo != nil {
		excludeID = a.Photo.ID
	}
	imageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	photo, imageErr := w.cfg.Photos.Search(imageCtx, a.ImageQuery, excludeID)
	if err := ctx.Err(); err != nil {
		return err
	}
	stage := "search"
	if imageErr == nil {
		imageErr = ValidatePhoto(photo)
		stage = "validation"
	}
	if imageErr == nil {
		// Avoid tracking or attaching a result after the editor changed the draft.
		current, err := w.store.GetArticle(a.ID)
		if err != nil {
			return err
		}
		if current.Revision != a.Revision || current.Status != StatusReady || current.ImageStatus != StatusProcessing {
			return nil
		}
		issue, err := w.store.Issue(a.IssueID)
		if err != nil {
			return err
		}
		if issue.PublishedAt != nil {
			return nil
		}
		stage = "tracking"
		imageErr = w.cfg.Photos.Track(imageCtx, photo)
	}
	if err := ctx.Err(); err != nil {
		// The startup recovery will requeue this claim after shutdown.
		return err
	}
	if imageErr == nil {
		imageErr = imageCtx.Err()
	}
	if imageErr != nil {
		err = w.store.FailImage(a.ID, a.Revision)
		if errors.Is(err, ErrConflict) {
			return nil
		}
		if err == nil {
			fields := []any{"article_id", a.ID, "stage", stage}
			var failure *photoError
			if errors.As(imageErr, &failure) {
				fields = append(fields, "reason", failure.reason, "http_status", failure.httpStatus)
			} else if errors.Is(imageErr, ErrNoPhoto) {
				fields = append(fields, "reason", "no suitable photo found")
			}
			w.logger.Warn("article image lookup failed", fields...)
		}
		return err
	}
	err = w.store.CompleteImage(a.ID, a.Revision, photo)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

func (w *Worker) issueLink(id int) string {
	return fmt.Sprintf("%s/issues/%d", strings.TrimRight(w.cfg.BaseURL, "/"), id)
}

func (w *Worker) maintain(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.now().Before(w.nextMaintenance) {
		return nil
	}
	// Measure from completion so slow sends cannot cause back-to-back retries.
	defer func() { w.nextMaintenance = w.now().Add(time.Minute) }()
	i, err := w.store.PendingNotification()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err = w.send(ctx, w.cfg.PublishChannel, "Ett nytt nummer av Kumpanposten finns att läsa: "+w.issueLink(i.ID)); err == nil {
			if err = w.store.MarkNotified(i.ID); err != nil {
				return err
			}
		} else {
			w.logger.Warn("publication notification failed", "issue_id", i.ID, "category", "delivery")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	location, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		return err
	}
	now := w.now().In(location)
	if now.Weekday() != time.Friday || now.Hour() < 9 || len(w.cfg.EditorIDs) == 0 {
		return nil
	}
	date := now.Format("2006-01-02")
	latest, err := w.store.LatestPublished()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && latest.PublishedAt.In(location).Format("2006-01-02") == date {
		return nil
	}
	draft, err := w.store.CurrentDraft()
	if err != nil {
		return err
	}
	for _, editorID := range w.cfg.EditorIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := date + ":" + editorID
		sent, err := w.store.ReminderSent(key)
		if err != nil {
			return err
		}
		if sent {
			continue
		}
		if err := w.send(ctx, editorID, "Fredag! Granska utkastet till Kumpanposten och publicera när det är klart: "+strings.TrimRight(w.cfg.BaseURL, "/")+"/draft"); err != nil {
			w.logger.Warn("editor reminder failed", "editor_id", editorID, "issue_id", draft.ID, "category", "delivery")
			continue
		}
		if err := w.store.MarkReminderSent(key); err != nil {
			return err
		}
	}
	return ctx.Err()
}
