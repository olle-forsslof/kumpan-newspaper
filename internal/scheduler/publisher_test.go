package scheduler

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

func setupTestDB(t *testing.T, path string) *database.DB {
	db, err := database.NewSimple(path)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return db
}

func TestNewNewsletterPublisher(t *testing.T) {
	tempFile := "/tmp/test_publisher_new.db"
	defer os.Remove(tempFile)

	db := setupTestDB(t, tempFile)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	publisher, err := NewNewsletterPublisher(db, logger)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	if publisher.db == nil {
		t.Error("Expected database to be set")
	}

	if publisher.logger == nil {
		t.Error("Expected logger to be set")
	}

	if publisher.location == nil {
		t.Error("Expected location to be set")
	}

	expectedLocation := "Europe/Stockholm"
	if publisher.location.String() != expectedLocation {
		t.Errorf("Expected location %s, got %s", expectedLocation, publisher.location.String())
	}
}

func TestPublishCurrentWeek(t *testing.T) {
	tempFile := "/tmp/test_publish_current_week.db"
	defer os.Remove(tempFile)

	db := setupTestDB(t, tempFile)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	publisher, err := NewNewsletterPublisher(db, logger)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	t.Run("No current week issue exists", func(t *testing.T) {
		err := publisher.PublishCurrentWeek()
		if err == nil {
			t.Error("Expected error when no current week issue exists")
		}
	})

	t.Run("Publish current week successfully", func(t *testing.T) {
		now := time.Now()
		year, week := now.ISOWeek()

		issue, err := db.CreateWeeklyNewsletterIssue(week, year)
		if err != nil {
			t.Fatalf("Failed to create current week issue: %v", err)
		}

		if issue.Status != database.IssueStatusDraft {
			t.Errorf("Expected draft status, got %s", issue.Status)
		}

		err = publisher.PublishCurrentWeek()
		if err != nil {
			t.Fatalf("Failed to publish current week: %v", err)
		}

		publishedIssue, err := db.GetWeeklyNewsletterIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to get published issue: %v", err)
		}

		if publishedIssue.Status != database.IssueStatusPublished {
			t.Errorf("Expected published status, got %s", publishedIssue.Status)
		}

		if publishedIssue.PublishedAt == nil {
			t.Error("Expected published_at to be set")
		}
	})

	t.Run("Already published issue returns no error", func(t *testing.T) {
		err := publisher.PublishCurrentWeek()
		if err != nil {
			t.Errorf("Expected no error when issue already published, got: %v", err)
		}
	})
}

func TestPublisherStartStop(t *testing.T) {
	tempFile := "/tmp/test_publisher_start_stop.db"
	defer os.Remove(tempFile)

	db := setupTestDB(t, tempFile)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	publisher, err := NewNewsletterPublisher(db, logger)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	go publisher.Start()

	time.Sleep(100 * time.Millisecond)

	publisher.Stop()
}

func TestCheckAndPublish(t *testing.T) {
	tempFile := "/tmp/test_check_and_publish.db"
	defer os.Remove(tempFile)

	db := setupTestDB(t, tempFile)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	publisher, err := NewNewsletterPublisher(db, logger)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	t.Run("Check and publish on non-Friday does nothing", func(t *testing.T) {
		now := time.Now()
		year, week := now.ISOWeek()

		_, err := db.CreateWeeklyNewsletterIssue(week, year)
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		publisher.checkAndPublish()

		issue, err := db.GetCurrentWeekIssue()
		if err != nil {
			t.Fatalf("Failed to get current week issue: %v", err)
		}

		if now.Weekday() != time.Friday {
			if issue.Status == database.IssueStatusPublished {
				t.Error("Issue should not be published on non-Friday")
			}
		}
	})
}
