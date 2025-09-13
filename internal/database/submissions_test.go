package database

import (
	"context"
	"path/filepath"
	"testing"
)

// TDD: Test for SubmissionManager interface
func TestSubmissionManager_CreateNewsSubmission(t *testing.T) {
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

	// Create SubmissionManager - this should FAIL initially
	manager := NewSubmissionManager(db.DB)

	// Test: Create news submission
	userID := "U123456789"
	content := "Our team launched the mobile app this week!"

	submission, err := manager.CreateNewsSubmission(context.Background(), userID, content)
	if err != nil {
		t.Fatalf("CreateNewsSubmission() failed: %v", err)
	}

	if submission.ID <= 0 {
		t.Fatal("Expected valid submission ID")
	}

	if submission.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, submission.UserID)
	}

	if submission.Content != content {
		t.Errorf("Expected Content %s, got %s", content, submission.Content)
	}

	if submission.QuestionID != nil {
		t.Errorf("Expected QuestionID to be nil for news submission, got %v", *submission.QuestionID)
	}
}

// TDD: Test for getting submissions by user
func TestSubmissionManager_GetSubmissionsByUser(t *testing.T) {
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

	manager := NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Create multiple submissions for different users
	user1 := "U111111111"
	user2 := "U222222222"

	_, err = manager.CreateNewsSubmission(ctx, user1, "User 1 news story 1")
	if err != nil {
		t.Fatalf("Failed to create submission for user1: %v", err)
	}

	_, err = manager.CreateNewsSubmission(ctx, user1, "User 1 news story 2")
	if err != nil {
		t.Fatalf("Failed to create second submission for user1: %v", err)
	}

	_, err = manager.CreateNewsSubmission(ctx, user2, "User 2 news story")
	if err != nil {
		t.Fatalf("Failed to create submission for user2: %v", err)
	}

	// Test: Get submissions for user1 only
	user1Submissions, err := manager.GetSubmissionsByUser(ctx, user1)
	if err != nil {
		t.Fatalf("GetSubmissionsByUser() failed: %v", err)
	}

	if len(user1Submissions) != 2 {
		t.Errorf("Expected 2 submissions for user1, got %d", len(user1Submissions))
	}

	// Verify all returned submissions belong to user1
	for _, sub := range user1Submissions {
		if sub.UserID != user1 {
			t.Errorf("Expected UserID %s, got %s", user1, sub.UserID)
		}
	}
}

// TDD: Test for getting all submissions (for admin)
func TestSubmissionManager_GetAllSubmissions(t *testing.T) {
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

	manager := NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Create submissions from multiple users
	_, err = manager.CreateNewsSubmission(ctx, "U111111111", "News 1")
	if err != nil {
		t.Fatalf("Failed to create submission: %v", err)
	}

	_, err = manager.CreateNewsSubmission(ctx, "U222222222", "News 2")
	if err != nil {
		t.Fatalf("Failed to create submission: %v", err)
	}

	_, err = manager.CreateNewsSubmission(ctx, "U333333333", "News 3")
	if err != nil {
		t.Fatalf("Failed to create submission: %v", err)
	}

	// Test: Get all submissions (admin function)
	allSubmissions, err := manager.GetAllSubmissions(ctx)
	if err != nil {
		t.Fatalf("GetAllSubmissions() failed: %v", err)
	}

	if len(allSubmissions) != 3 {
		t.Errorf("Expected 3 total submissions, got %d", len(allSubmissions))
	}

	// Verify submissions are ordered by creation time (newest first)
	if len(allSubmissions) >= 2 {
		if allSubmissions[0].CreatedAt.Before(allSubmissions[1].CreatedAt) {
			t.Error("Expected submissions to be ordered by creation time (newest first)")
		}
	}
}
