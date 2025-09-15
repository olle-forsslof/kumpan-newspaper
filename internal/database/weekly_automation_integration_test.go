package database

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestWeeklyAutomationIntegration tests the complete weekly newsletter automation workflow
func TestWeeklyAutomationIntegration(t *testing.T) {
	// Create a temporary database for testing
	tempFile := "/tmp/test_weekly_integration.db"
	defer os.Remove(tempFile)

	db, err := NewSimple(tempFile)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Run migrations to set up the schema
	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	ctx := context.Background()

	t.Run("EndToEndNewsletterWorkflow", testEndToEndNewsletterWorkflow(ctx, db))
	t.Run("PersonRotationAlgorithm", testPersonRotationAlgorithm(ctx, db))
	t.Run("BodyMindQuestionPoolIntegration", testBodyMindQuestionPoolIntegration(ctx, db))
	t.Run("WeeklyIssueAndAssignmentIntegration", testWeeklyIssueAndAssignmentIntegration(ctx, db))
	t.Run("NewsletterPublicationWorkflow", testNewsletterPublicationWorkflow(ctx, db))
}

// testEndToEndNewsletterWorkflow tests the complete workflow from assignment to publication
func testEndToEndNewsletterWorkflow(ctx context.Context, db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Create a weekly newsletter issue
		weekNumber := 40
		year := 2025

		issue, err := db.CreateWeeklyNewsletterIssue(weekNumber, year)
		if err != nil {
			t.Fatalf("Failed to create weekly newsletter issue: %v", err)
		}

		// Create person assignments for different content types
		assignments := []PersonAssignment{
			{
				IssueID:     issue.ID,
				PersonID:    "U123FEATURE",
				ContentType: ContentTypeFeature,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue.ID,
				PersonID:    "U456GENERAL1",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue.ID,
				PersonID:    "U789GENERAL2",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
		}

		// Create assignments and track rotation history
		for _, assignment := range assignments {
			assignmentID, err := db.CreatePersonAssignment(assignment)
			if err != nil {
				t.Fatalf("Failed to create assignment: %v", err)
			}

			// Verify assignment was created
			if assignmentID <= 0 {
				t.Error("Expected valid assignment ID")
			}

			// Add rotation history
			err = db.AddPersonRotationHistory(assignment.PersonID, assignment.ContentType, weekNumber, year)
			if err != nil {
				t.Fatalf("Failed to add rotation history: %v", err)
			}
		}

		// Create some user submissions
		submissions := []struct {
			userID  string
			content string
		}{
			{"U123FEATURE", "We launched a major new feature this week that improves user onboarding by 40%."},
			{"U456GENERAL1", "Our engineering team completed the security audit with zero critical issues found."},
			{"U789GENERAL2", "The marketing team organized a successful tech talk series this quarter."},
		}

		var submissionIDs []int
		for _, sub := range submissions {
			id, err := db.CreateNewsSubmission(sub.userID, sub.content)
			if err != nil {
				t.Fatalf("Failed to create submission for user %s: %v", sub.userID, err)
			}
			submissionIDs = append(submissionIDs, id)
		}

		// Link submissions to assignments
		retrievedAssignments, err := db.GetPersonAssignmentsByIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to get assignments: %v", err)
		}

		if len(retrievedAssignments) != len(assignments) {
			t.Errorf("Expected %d assignments, got %d", len(assignments), len(retrievedAssignments))
		}

		// Verify the weekly issue is properly configured
		if issue.WeekNumber != weekNumber {
			t.Errorf("Expected week number %d, got %d", weekNumber, issue.WeekNumber)
		}

		if issue.Year != year {
			t.Errorf("Expected year %d, got %d", year, issue.Year)
		}

		if issue.Status != IssueStatusDraft {
			t.Errorf("Expected status %s, got %s", IssueStatusDraft, issue.Status)
		}

		// Verify publication date is correctly calculated (Thursday at 9:30 AM)
		if issue.PublicationDate.Weekday() != time.Thursday {
			t.Errorf("Expected publication on Thursday, got %s", issue.PublicationDate.Weekday())
		}

		if issue.PublicationDate.Hour() != 9 || issue.PublicationDate.Minute() != 30 {
			t.Errorf("Expected publication time 9:30 AM, got %02d:%02d",
				issue.PublicationDate.Hour(), issue.PublicationDate.Minute())
		}
	}
}

// testPersonRotationAlgorithm tests the intelligent person assignment rotation
func testPersonRotationAlgorithm(ctx context.Context, db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Create multiple weeks of assignments to test rotation
		testUsers := []string{"U100USER1", "U200USER2", "U300USER3", "U400USER4"}
		baseWeek := 35
		baseYear := 2025

		// Create 4 weeks of assignments to build rotation history
		for weekOffset := 0; weekOffset < 4; weekOffset++ {
			currentWeek := baseWeek + weekOffset

			issue, err := db.CreateWeeklyNewsletterIssue(currentWeek, baseYear)
			if err != nil {
				t.Fatalf("Failed to create issue for week %d: %v", currentWeek, err)
			}

			// Assign different users to feature content each week
			featureUserIndex := weekOffset % len(testUsers)
			assignment := PersonAssignment{
				IssueID:     issue.ID,
				PersonID:    testUsers[featureUserIndex],
				ContentType: ContentTypeFeature,
				AssignedAt:  time.Now(),
			}

			_, err = db.CreatePersonAssignment(assignment)
			if err != nil {
				t.Fatalf("Failed to create assignment for week %d: %v", currentWeek, err)
			}

			// Add rotation history
			err = db.AddPersonRotationHistory(testUsers[featureUserIndex], ContentTypeFeature, currentWeek, baseYear)
			if err != nil {
				t.Fatalf("Failed to add rotation history for week %d: %v", currentWeek, err)
			}
		}

		// Test rotation history retrieval for each user
		for _, userID := range testUsers {
			history, err := db.GetPersonRotationHistory(userID, ContentTypeFeature, 6) // Look back 6 weeks
			if err != nil {
				t.Fatalf("Failed to get rotation history for user %s: %v", userID, err)
			}

			// Each user should have exactly 1 assignment in the history
			if len(history) != 1 {
				t.Errorf("Expected 1 assignment in history for user %s, got %d", userID, len(history))
			}

			if len(history) > 0 {
				// Verify the assignment matches what we created
				if history[0].PersonID != userID {
					t.Errorf("Expected person ID %s in history, got %s", userID, history[0].PersonID)
				}

				if history[0].ContentType != ContentTypeFeature {
					t.Errorf("Expected content type %s, got %s", ContentTypeFeature, history[0].ContentType)
				}
			}
		}

		// Test that rotation avoids recent assignments
		// User who was assigned in week 38 should be identified in rotation history
		recentHistory, err := db.GetPersonRotationHistory(testUsers[3], ContentTypeFeature, 2) // Look back 2 weeks
		if err != nil {
			t.Fatalf("Failed to get recent rotation history: %v", err)
		}

		if len(recentHistory) == 0 {
			t.Error("Expected to find recent assignment in rotation history")
		}
	}
}

// testBodyMindQuestionPoolIntegration tests the anonymous question pool workflow
func testBodyMindQuestionPoolIntegration(ctx context.Context, db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		poolManager := NewBodyMindPoolManager(db)

		// Add questions to the pool
		testQuestions := []struct {
			text     string
			category string
		}{
			{"How do you manage stress during tight deadlines?", "wellness"},
			{"What's your favorite way to practice mindfulness at work?", "mental_health"},
			{"How do you maintain boundaries between work and personal life?", "work_life_balance"},
			{"What motivates you when facing challenging projects?", "wellness"},
			{"How do you handle difficult conversations with colleagues?", "mental_health"},
		}

		for _, q := range testQuestions {
			_, err := poolManager.AddQuestionToPool(q.text, q.category)
			if err != nil {
				t.Fatalf("Failed to add question to pool: %v", err)
			}
		}

		// Test pool status
		status, err := poolManager.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get pool status: %v", err)
		}

		if status.TotalActive != len(testQuestions) {
			t.Errorf("Expected %d active questions, got %d", len(testQuestions), status.TotalActive)
		}

		// Verify category breakdown
		expectedBreakdown := map[string]int{
			"wellness":          2,
			"mental_health":     2,
			"work_life_balance": 1,
		}

		for category, expectedCount := range expectedBreakdown {
			if status.CategoryBreakdown[category] != expectedCount {
				t.Errorf("Expected %d questions in category %s, got %d",
					expectedCount, category, status.CategoryBreakdown[category])
			}
		}

		// Test question selection for newsletter (FIFO)
		firstQuestion, err := poolManager.SelectQuestionForNewsletter()
		if err != nil {
			t.Fatalf("Failed to select question for newsletter: %v", err)
		}

		if firstQuestion.Status != "used" {
			t.Errorf("Expected selected question status 'used', got '%s'", firstQuestion.Status)
		}

		// Pool should now have one less active question
		newStatus, err := poolManager.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get updated pool status: %v", err)
		}

		expectedActive := len(testQuestions) - 1
		if newStatus.TotalActive != expectedActive {
			t.Errorf("Expected %d active questions after selection, got %d",
				expectedActive, newStatus.TotalActive)
		}

		// Test bulk question addition
		bulkQuestions := []struct {
			Text     string
			Category string
		}{
			{"What helps you stay focused during remote work?", "work_life_balance"},
			{"How do you celebrate small wins at work?", "wellness"},
		}

		addedQuestions, err := poolManager.BulkAddQuestions(bulkQuestions)
		if err != nil {
			t.Fatalf("Failed to bulk add questions: %v", err)
		}

		if len(addedQuestions) != len(bulkQuestions) {
			t.Errorf("Expected %d bulk questions added, got %d", len(bulkQuestions), len(addedQuestions))
		}

		// Test Slack formatting
		slackMessage := poolManager.FormatPoolStatusForSlack(newStatus)
		if slackMessage == "" {
			t.Error("Expected non-empty Slack formatted message")
		}

		// Message should contain key information
		expectedStrings := []string{
			"Body/Mind Question Pool Status",
			"Available Questions:",
			"Wellness",
			"Mental Health",
			"Work-Life Balance",
		}

		for _, expected := range expectedStrings {
			if !contains(slackMessage, expected) {
				t.Errorf("Slack message missing expected text: %s", expected)
			}
		}
	}
}

// testWeeklyIssueAndAssignmentIntegration tests issue creation and assignment management
func testWeeklyIssueAndAssignmentIntegration(ctx context.Context, db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Test GetOrCreateWeeklyIssue functionality
		weekNumber := 45
		year := 2025

		// First call should create the issue
		issue1, err := db.GetOrCreateWeeklyIssue(weekNumber, year)
		if err != nil {
			t.Fatalf("Failed to create weekly issue: %v", err)
		}

		// Second call should return existing issue
		issue2, err := db.GetOrCreateWeeklyIssue(weekNumber, year)
		if err != nil {
			t.Fatalf("Failed to get existing weekly issue: %v", err)
		}

		if issue1.ID != issue2.ID {
			t.Errorf("Expected same issue ID, got %d and %d", issue1.ID, issue2.ID)
		}

		// Test complex assignment scenarios
		complexAssignments := []PersonAssignment{
			{
				IssueID:     issue1.ID,
				PersonID:    "U111ALPHA",
				ContentType: ContentTypeFeature,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue1.ID,
				PersonID:    "U222BETA",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue1.ID,
				PersonID:    "U333GAMMA",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue1.ID,
				PersonID:    "U444DELTA",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
		}

		// Create all assignments
		for i, assignment := range complexAssignments {
			id, err := db.CreatePersonAssignment(assignment)
			if err != nil {
				t.Fatalf("Failed to create assignment %d: %v", i, err)
			}

			if id <= 0 {
				t.Errorf("Expected valid assignment ID for assignment %d", i)
			}
		}

		// Retrieve and verify assignments
		assignments, err := db.GetPersonAssignmentsByIssue(issue1.ID)
		if err != nil {
			t.Fatalf("Failed to get assignments: %v", err)
		}

		if len(assignments) != len(complexAssignments) {
			t.Errorf("Expected %d assignments, got %d", len(complexAssignments), len(assignments))
		}

		// Verify content type distribution
		contentTypeCounts := make(map[ContentType]int)
		for _, assignment := range assignments {
			contentTypeCounts[assignment.ContentType]++
		}

		expectedCounts := map[ContentType]int{
			ContentTypeFeature: 1,
			ContentTypeGeneral: 3,
		}

		for contentType, expectedCount := range expectedCounts {
			if contentTypeCounts[contentType] != expectedCount {
				t.Errorf("Expected %d assignments of type %s, got %d",
					expectedCount, contentType, contentTypeCounts[contentType])
			}
		}

		// Test assignment validation (basic validation, not FK constraints)
		invalidAssignment := PersonAssignment{
			IssueID:     0, // Invalid issue ID (application-level validation)
			PersonID:    "U555INVALID",
			ContentType: ContentTypeFeature,
			AssignedAt:  time.Now(),
		}

		_, err = db.CreatePersonAssignment(invalidAssignment)
		if err == nil {
			t.Error("Expected validation error for assignment with invalid issue ID")
		}

		// Test invalid content type
		invalidContentTypeAssignment := PersonAssignment{
			IssueID:     issue1.ID,
			PersonID:    "U666INVALID",
			ContentType: "invalid_type",
			AssignedAt:  time.Now(),
		}

		_, err = db.CreatePersonAssignment(invalidContentTypeAssignment)
		if err == nil {
			t.Error("Expected validation error for assignment with invalid content type")
		}
	}
}

// testNewsletterPublicationWorkflow tests the complete publication workflow
func testNewsletterPublicationWorkflow(ctx context.Context, db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Create a newsletter issue
		weekNumber := 47
		year := 2025

		issue, err := db.CreateWeeklyNewsletterIssue(weekNumber, year)
		if err != nil {
			t.Fatalf("Failed to create newsletter issue: %v", err)
		}

		// Create assignments
		assignment := PersonAssignment{
			IssueID:     issue.ID,
			PersonID:    "U999PUBLISHER",
			ContentType: ContentTypeFeature,
			AssignedAt:  time.Now(),
		}

		assignmentID, err := db.CreatePersonAssignment(assignment)
		if err != nil {
			t.Fatalf("Failed to create assignment: %v", err)
		}

		// Create a submission
		submissionID, err := db.CreateNewsSubmission("U999PUBLISHER", "This is a test feature story for publication workflow.")
		if err != nil {
			t.Fatalf("Failed to create submission: %v", err)
		}

		// Create a processed article for the submission
		processedArticle := ProcessedArticle{
			SubmissionID:      submissionID,
			NewsletterIssueID: &issue.ID,
			JournalistType:    "feature",
			ProcessedContent:  `{"headline": "Test Feature Story", "lead": "This is a test lead", "body": "This is the test body content", "byline": "Feature Writer"}`,
			ProcessingPrompt:  "Test processing prompt",
			TemplateFormat:    "hero",
			ProcessingStatus:  ProcessingStatusSuccess,
			WordCount:         25,
		}

		articleID, err := db.CreateProcessedArticle(processedArticle)
		if err != nil {
			t.Fatalf("Failed to create processed article: %v", err)
		}

		// Verify the complete workflow chain
		// 1. Issue exists
		retrievedIssue, err := db.GetWeeklyNewsletterIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrievedIssue.Status != IssueStatusDraft {
			t.Errorf("Expected issue status %s, got %s", IssueStatusDraft, retrievedIssue.Status)
		}

		// 2. Assignment exists and links to issue
		retrievedAssignments, err := db.GetPersonAssignmentsByIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve assignments: %v", err)
		}

		if len(retrievedAssignments) != 1 {
			t.Fatalf("Expected 1 assignment, got %d", len(retrievedAssignments))
		}

		if retrievedAssignments[0].ID != assignmentID {
			t.Errorf("Expected assignment ID %d, got %d", assignmentID, retrievedAssignments[0].ID)
		}

		// 3. Submission exists
		submission, err := db.GetSubmission(submissionID)
		if err != nil {
			t.Fatalf("Failed to retrieve submission: %v", err)
		}

		if submission.UserID != "U999PUBLISHER" {
			t.Errorf("Expected submission from U999PUBLISHER, got %s", submission.UserID)
		}

		// 4. Processed article exists and links to both submission and issue
		retrievedArticle, err := db.GetProcessedArticle(articleID)
		if err != nil {
			t.Fatalf("Failed to retrieve processed article: %v", err)
		}

		if retrievedArticle.SubmissionID != submissionID {
			t.Errorf("Expected article submission ID %d, got %d", submissionID, retrievedArticle.SubmissionID)
		}

		if retrievedArticle.NewsletterIssueID == nil || *retrievedArticle.NewsletterIssueID != issue.ID {
			t.Errorf("Expected article newsletter issue ID %d, got %v", issue.ID, retrievedArticle.NewsletterIssueID)
		}

		// 5. JSON content validation
		if err := retrievedArticle.ValidateJSONContent(); err != nil {
			t.Errorf("Processed article JSON validation failed: %v", err)
		}

		// 6. Test content extraction
		headline, err := retrievedArticle.GetHeadline()
		if err != nil {
			t.Fatalf("Failed to get headline: %v", err)
		}

		if headline != "Test Feature Story" {
			t.Errorf("Expected headline 'Test Feature Story', got '%s'", headline)
		}

		byline, err := retrievedArticle.GetByline()
		if err != nil {
			t.Fatalf("Failed to get byline: %v", err)
		}

		if byline != "Feature Writer" {
			t.Errorf("Expected byline 'Feature Writer', got '%s'", byline)
		}
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsHelper(s, substr))))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
