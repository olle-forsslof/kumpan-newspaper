package database

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TDD RED Phase - These tests will fail until we implement the functionality

func TestProcessedArticleMigration(t *testing.T) {
	// Test that the migration creates the processed_articles table
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Check that processed_articles table exists
	var tableExists int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master 
		WHERE type='table' AND name='processed_articles'
	`).Scan(&tableExists)

	if err != nil {
		t.Fatalf("Failed to check table existence: %v", err)
	}

	if tableExists != 1 {
		t.Fatalf("Expected processed_articles table to exist, but it doesn't")
	}

	// Check that indexes were created
	var indexCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master 
		WHERE type='index' AND name LIKE 'idx_processed_articles_%'
	`).Scan(&indexCount)

	if err != nil {
		t.Fatalf("Failed to check indexes: %v", err)
	}

	if indexCount < 3 {
		t.Fatalf("Expected at least 3 indexes for processed_articles, got %d", indexCount)
	}
}

func TestCreateProcessedArticle(t *testing.T) {
	// Test creating a new processed article
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

	// Create a test submission first (foreign key requirement)
	submissionID, err := db.CreateNewsSubmission("U123456", "Test submission content")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Test creating processed article
	now := time.Now()
	article := ProcessedArticle{
		SubmissionID:     submissionID,
		JournalistType:   "feature",
		ProcessedContent: "This is the AI-processed content for the feature story.",
		ProcessingPrompt: "You are an engaging feature writer. Transform this submission...",
		TemplateFormat:   "hero",
		ProcessingStatus: ProcessingStatusSuccess,
		WordCount:        12,
		ProcessedAt:      &now,
	}

	// This method doesn't exist yet - will cause RED phase failure
	articleID, err := db.CreateProcessedArticle(article)
	if err != nil {
		t.Fatalf("CreateProcessedArticle() failed: %v", err)
	}

	if articleID <= 0 {
		t.Fatalf("Expected positive article ID, got %d", articleID)
	}

	// Verify the article was created correctly
	retrieved, err := db.GetProcessedArticle(articleID)
	if err != nil {
		t.Fatalf("GetProcessedArticle() failed: %v", err)
	}

	if retrieved.SubmissionID != submissionID {
		t.Errorf("Expected SubmissionID %d, got %d", submissionID, retrieved.SubmissionID)
	}

	if retrieved.JournalistType != "feature" {
		t.Errorf("Expected JournalistType 'feature', got %s", retrieved.JournalistType)
	}

	if retrieved.ProcessingStatus != ProcessingStatusSuccess {
		t.Errorf("Expected ProcessingStatus %s, got %s", ProcessingStatusSuccess, retrieved.ProcessingStatus)
	}

	if retrieved.WordCount != 12 {
		t.Errorf("Expected WordCount 12, got %d", retrieved.WordCount)
	}
}

func TestProcessedArticleValidation(t *testing.T) {
	// Test validation rules
	tests := []struct {
		name    string
		article ProcessedArticle
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid article",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessedContent: "Some content",
				ProcessingStatus: ProcessingStatusSuccess,
				TemplateFormat:   "hero",
			},
			wantErr: false,
		},
		{
			name: "invalid status",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessingStatus: "invalid_status",
				TemplateFormat:   "hero",
			},
			wantErr: true,
			errMsg:  "invalid processing status",
		},
		{
			name: "missing submission_id",
			article: ProcessedArticle{
				SubmissionID:     0,
				JournalistType:   "feature",
				ProcessingStatus: ProcessingStatusPending,
				TemplateFormat:   "hero",
			},
			wantErr: true,
			errMsg:  "submission_id is required",
		},
		{
			name: "successful status without content",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessedContent: "",
				ProcessingStatus: ProcessingStatusSuccess,
				TemplateFormat:   "hero",
			},
			wantErr: true,
			errMsg:  "processed_content required",
		},
		{
			name: "missing journalist type",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "",
				ProcessingStatus: ProcessingStatusPending,
				TemplateFormat:   "hero",
			},
			wantErr: true,
			errMsg:  "journalist_type is required",
		},
		{
			name: "missing template format",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessingStatus: ProcessingStatusPending,
				TemplateFormat:   "",
			},
			wantErr: true,
			errMsg:  "template_format is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.article.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errMsg)
				} else if err.Error() == "" {
					t.Errorf("Expected non-empty error message")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

func TestUpdateProcessedArticleStatus(t *testing.T) {
	// Test updating processing status and retry count
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

	// Create test submission and article
	submissionID, err := db.CreateNewsSubmission("U123456", "Test content")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	article := ProcessedArticle{
		SubmissionID:     submissionID,
		JournalistType:   "general",
		ProcessingStatus: ProcessingStatusPending,
		TemplateFormat:   "column",
	}

	articleID, err := db.CreateProcessedArticle(article)
	if err != nil {
		t.Fatalf("CreateProcessedArticle() failed: %v", err)
	}

	// Test updating status to failed with error message
	errorMsg := "AI API timeout"
	err = db.UpdateProcessedArticleStatus(articleID, ProcessingStatusFailed, &errorMsg, 1)
	if err != nil {
		t.Fatalf("UpdateProcessedArticleStatus() failed: %v", err)
	}

	// Verify the update
	updated, err := db.GetProcessedArticle(articleID)
	if err != nil {
		t.Fatalf("GetProcessedArticle() failed: %v", err)
	}

	if updated.ProcessingStatus != ProcessingStatusFailed {
		t.Errorf("Expected status %s, got %s", ProcessingStatusFailed, updated.ProcessingStatus)
	}

	if updated.ErrorMessage == nil || *updated.ErrorMessage != errorMsg {
		t.Errorf("Expected error message '%s', got %v", errorMsg, updated.ErrorMessage)
	}

	if updated.RetryCount != 1 {
		t.Errorf("Expected retry count 1, got %d", updated.RetryCount)
	}
}

func TestGetProcessedArticlesByStatus(t *testing.T) {
	// Test querying articles by processing status
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

	// Create test submission
	submissionID, err := db.CreateNewsSubmission("U123456", "Test content")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create articles with different statuses
	testArticles := []ProcessedArticle{
		{
			SubmissionID:     submissionID,
			JournalistType:   "general",
			ProcessingStatus: ProcessingStatusPending,
			TemplateFormat:   "column",
		},
		{
			SubmissionID:     submissionID,
			JournalistType:   "general",
			ProcessingStatus: ProcessingStatusFailed,
			TemplateFormat:   "column",
		},
		{
			SubmissionID:     submissionID,
			JournalistType:   "general",
			ProcessedContent: "Successfully processed content",
			ProcessingStatus: ProcessingStatusSuccess,
			TemplateFormat:   "column",
		},
	}
	articleIDs := make([]int, len(testArticles))

	for i, article := range testArticles {
		articleIDs[i], err = db.CreateProcessedArticle(article)
		if err != nil {
			t.Fatalf("CreateProcessedArticle() failed: %v", err)
		}
	}

	// Test getting failed articles
	failedArticles, err := db.GetProcessedArticlesByStatus(ProcessingStatusFailed)
	if err != nil {
		t.Fatalf("GetProcessedArticlesByStatus() failed: %v", err)
	}

	if len(failedArticles) != 1 {
		t.Errorf("Expected 1 failed article, got %d", len(failedArticles))
	}

	if len(failedArticles) > 0 && failedArticles[0].ProcessingStatus != ProcessingStatusFailed {
		t.Errorf("Expected failed status, got %s", failedArticles[0].ProcessingStatus)
	}
}

func TestGetProcessedArticlesBySubmissionID(t *testing.T) {
	// Test getting processed articles for a specific submission
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

	// Create test submissions
	submissionID1, err := db.CreateNewsSubmission("U123456", "Test content 1")
	if err != nil {
		t.Fatalf("Failed to create test submission 1: %v", err)
	}

	submissionID2, err := db.CreateNewsSubmission("U789012", "Test content 2")
	if err != nil {
		t.Fatalf("Failed to create test submission 2: %v", err)
	}

	// Create articles for submission 1
	for i := 0; i < 2; i++ {
		article := ProcessedArticle{
			SubmissionID:     submissionID1,
			JournalistType:   "general",
			ProcessedContent: fmt.Sprintf("Processed content for article %d", i+1),
			ProcessingStatus: ProcessingStatusSuccess,
			TemplateFormat:   "column",
		}

		_, err = db.CreateProcessedArticle(article)
		if err != nil {
			t.Fatalf("CreateProcessedArticle() failed: %v", err)
		}
	}

	// Create one article for submission 2
	article := ProcessedArticle{
		SubmissionID:     submissionID2,
		JournalistType:   "feature",
		ProcessedContent: "Feature article processed content",
		ProcessingStatus: ProcessingStatusSuccess,
		TemplateFormat:   "hero",
	}

	_, err = db.CreateProcessedArticle(article)
	if err != nil {
		t.Fatalf("CreateProcessedArticle() failed: %v", err)
	}

	// Test getting articles for submission 1
	articles, err := db.GetProcessedArticlesBySubmissionID(submissionID1)
	if err != nil {
		t.Fatalf("GetProcessedArticlesBySubmissionID() failed: %v", err)
	}

	if len(articles) != 2 {
		t.Errorf("Expected 2 articles for submission 1, got %d", len(articles))
	}

	for _, article := range articles {
		if article.SubmissionID != submissionID1 {
			t.Errorf("Expected SubmissionID %d, got %d", submissionID1, article.SubmissionID)
		}
	}
}
