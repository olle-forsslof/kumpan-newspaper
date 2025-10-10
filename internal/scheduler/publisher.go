package scheduler

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

type NewsletterPublisher struct {
	db       *database.DB
	logger   *slog.Logger
	location *time.Location
	stopChan chan struct{}
	done     chan struct{}
}

func NewNewsletterPublisher(db *database.DB, logger *slog.Logger) (*NewsletterPublisher, error) {
	location, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		return nil, fmt.Errorf("failed to load Stockholm timezone: %w", err)
	}

	return &NewsletterPublisher{
		db:       db,
		logger:   logger,
		location: location,
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
	}, nil
}

func (np *NewsletterPublisher) Start() {
	np.logger.Info("Newsletter publisher scheduler started")

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-np.stopChan:
			np.logger.Info("Newsletter publisher scheduler stopping")
			close(np.done)
			return
		case <-ticker.C:
			np.checkAndPublish()
		}
	}
}

func (np *NewsletterPublisher) Stop() {
	np.logger.Info("Stopping newsletter publisher scheduler")
	close(np.stopChan)
	<-np.done
	np.logger.Info("Newsletter publisher scheduler stopped")
}

func (np *NewsletterPublisher) checkAndPublish() {
	now := time.Now().In(np.location)

	if now.Weekday() != time.Friday {
		return
	}

	if now.Hour() != 9 || now.Minute() != 0 {
		return
	}

	np.logger.Info("Publishing time reached, attempting to publish current week's newsletter")

	if err := np.PublishCurrentWeek(); err != nil {
		np.logger.Error("Failed to publish current week's newsletter", "error", err)
		np.retryPublish()
	}
}

func (np *NewsletterPublisher) retryPublish() {
	maxRetries := 2
	retryDelay := 1 * time.Minute

	for attempt := 1; attempt <= maxRetries; attempt++ {
		np.logger.Info("Retrying newsletter publish", "attempt", attempt, "max_retries", maxRetries)
		time.Sleep(retryDelay)

		if err := np.PublishCurrentWeek(); err != nil {
			np.logger.Error("Retry failed", "attempt", attempt, "error", err)
			if attempt == maxRetries {
				np.logger.Error("All publish retries exhausted, giving up")
			}
		} else {
			np.logger.Info("Newsletter published successfully on retry", "attempt", attempt)
			return
		}
	}
}

func (np *NewsletterPublisher) PublishCurrentWeek() error {
	issue, err := np.db.GetCurrentWeekIssue()
	if err != nil {
		return fmt.Errorf("failed to get current week issue: %w", err)
	}

	if issue.Status == database.IssueStatusPublished {
		np.logger.Info("Current week's newsletter already published", "week", issue.WeekNumber, "year", issue.Year)
		return nil
	}

	if err := np.db.PublishNewsletterIssue(issue.ID); err != nil {
		return fmt.Errorf("failed to publish newsletter issue: %w", err)
	}

	np.logger.Info("Newsletter published successfully",
		"issue_id", issue.ID,
		"week", issue.WeekNumber,
		"year", issue.Year,
	)

	return nil
}
