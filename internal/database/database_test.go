package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSimple(t *testing.T) {
	// Create a temporary directory for test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Test: Create new database connection
	db, err := NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Database file was not created")
	}

	// Test: Connection should be pingable
	if err := db.Ping(); err != nil {
		t.Fatalf("Database ping failed: %v", err)
	}
}

func TestNewWithConfig(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := Config{
		DataSourceName:  dbPath,
		MaxOpenConns:    10,
		MaxIdleConns:    2,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with config failed: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Database ping failed: %v", err)
	}
}

func TestNewWithInvalidPath(t *testing.T) {
	// Test: Invalid path should return error
	_, err := NewSimple("/invalid/path/that/cannot/exist.db")
	if err == nil {
		t.Fatal("Expected error for invalid path, got nil")
	}
}

func TestMigrate(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	// Test: Migration should succeed
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Test: Check that tables were created
	tables := []string{"questions", "submissions", "newsletter_issues", "schema_migrations"}
	for _, table := range tables {
		var count int
		query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		err := db.QueryRow(query, table).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("Table %s was not created", table)
		}
	}

	// Test: Migration should be idempotent (safe to run multiple times)
	if err := db.Migrate(); err != nil {
		t.Fatalf("Second migration failed: %v", err)
	}
}

func TestSubmissionCRUD(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// First, create a test question (submissions need a question_id)
	questionID, err := db.CreateQuestion("What's your favorite programming language?", "tech")
	if err != nil {
		t.Fatalf("Failed to create test question: %v", err)
	}

	// Test: Create submission
	submission := &Submission{
		UserID:     "user123",
		QuestionID: questionID,
		Content:    "Go is pretty neat, though verbose",
	}

	submissionID, err := db.CreateSubmission(submission)
	if err != nil {
		t.Fatalf("CreateSubmission() failed: %v", err)
	}

	if submissionID <= 0 {
		t.Fatal("Expected valid submission ID")
	}

	// Test: Read submission
	retrieved, err := db.GetSubmission(submissionID)
	if err != nil {
		t.Fatalf("GetSubmission() failed: %v", err)
	}

	if retrieved.UserID != submission.UserID {
		t.Errorf("Expected UserID %s, got %s", submission.UserID, retrieved.UserID)
	}

	if retrieved.Content != submission.Content {
		t.Errorf("Expected Content %s, got %s", submission.Content, retrieved.Content)
	}

	// Test: List submissions
	submissions, err := db.ListSubmissions()
	if err != nil {
		t.Fatalf("ListSubmissions() failed: %v", err)
	}

	if len(submissions) != 1 {
		t.Errorf("Expected 1 submission, got %d", len(submissions))
	}

	// Test: Delete submission
	if err := db.DeleteSubmission(submissionID); err != nil {
		t.Fatalf("DeleteSubmission() failed: %v", err)
	}

	// Verify deletion
	_, err = db.GetSubmission(submissionID)
	if err == nil {
		t.Fatal("Expected error when getting deleted submission")
	}
}
