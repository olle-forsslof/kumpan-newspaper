package slack

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TDD: Test admin command to list all submissions
func TestAdminHandler_ListSubmissions(t *testing.T) {
	// Set up test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Create SubmissionManager and add test data
	submissionManager := database.NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Add some test submissions
	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "First news story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	_, err = submissionManager.CreateNewsSubmission(ctx, "U222222222", "Second news story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create admin handler - THIS WILL FAIL initially because it doesn't accept SubmissionManager
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test admin list-submissions command
	cmd := &AdminCommand{
		Action: "list-submissions",
		Args:   []string{},
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify response contains both submissions
	if !strings.Contains(response.Text, "First news story") {
		t.Error("Expected response to contain first news story")
	}

	if !strings.Contains(response.Text, "Second news story") {
		t.Error("Expected response to contain second news story")
	}

	if !strings.Contains(response.Text, "U111111111") {
		t.Error("Expected response to contain first user ID")
	}

	if !strings.Contains(response.Text, "U222222222") {
		t.Error("Expected response to contain second user ID")
	}
}

// TDD: Test admin command to list submissions by user
func TestAdminHandler_ListSubmissionsByUser(t *testing.T) {
	// Set up test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Create SubmissionManager and add test data
	submissionManager := database.NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Add submissions from different users
	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "User 1 story 1")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "User 1 story 2")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	_, err = submissionManager.CreateNewsSubmission(ctx, "U222222222", "User 2 story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create admin handler
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test admin list-submissions command with user filter
	cmd := &AdminCommand{
		Action: "list-submissions",
		Args:   []string{"U111111111"}, // Filter by specific user
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify response contains only User 1's submissions
	if !strings.Contains(response.Text, "User 1 story 1") {
		t.Error("Expected response to contain User 1 story 1")
	}

	if !strings.Contains(response.Text, "User 1 story 2") {
		t.Error("Expected response to contain User 1 story 2")
	}

	if strings.Contains(response.Text, "User 2 story") {
		t.Error("Expected response to NOT contain User 2 story when filtering by User 1")
	}
}

// TDD: Test admin command to remove user submissions
func TestAdminHandler_RemoveSubmission(t *testing.T) {
	// Set up test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Create SubmissionManager and add test data
	submissionManager := database.NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Add submissions from different users
	submission1, err := submissionManager.CreateNewsSubmission(ctx, "U111111111", "User 1 story to remove")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	submission2, err := submissionManager.CreateNewsSubmission(ctx, "U111111111", "User 1 another story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	submission3, err := submissionManager.CreateNewsSubmission(ctx, "U222222222", "User 2 story should remain")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create admin handler
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test admin remove-submission command
	cmd := &AdminCommand{
		Action: "remove-submission",
		Args:   []string{"U111111111"}, // Remove submissions for this user
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify response indicates successful removal
	if !strings.Contains(response.Text, "removed") {
		t.Errorf("Expected response to indicate removal, got: %s", response.Text)
	}

	// Verify User 1's submissions are gone
	user1Submissions, err := submissionManager.GetSubmissionsByUser(ctx, "U111111111")
	if err != nil {
		t.Fatalf("Failed to get user submissions: %v", err)
	}

	if len(user1Submissions) != 0 {
		t.Errorf("Expected 0 submissions for User 1 after removal, got %d", len(user1Submissions))
	}

	// Verify User 2's submissions remain
	user2Submissions, err := submissionManager.GetSubmissionsByUser(ctx, "U222222222")
	if err != nil {
		t.Fatalf("Failed to get user submissions: %v", err)
	}

	if len(user2Submissions) != 1 {
		t.Errorf("Expected 1 submission for User 2 to remain, got %d", len(user2Submissions))
	}

	// Verify the remaining submission is the correct one
	if len(user2Submissions) > 0 && user2Submissions[0].Content != "User 2 story should remain" {
		t.Errorf("Expected User 2's submission to remain unchanged")
	}

	// Test edge case: removing submissions for user with no submissions
	cmdEmpty := &AdminCommand{
		Action: "remove-submission",
		Args:   []string{"U333333333"}, // User with no submissions
	}

	responseEmpty, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmdEmpty)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed for empty case: %v", err)
	}

	// Should handle gracefully
	if !strings.Contains(responseEmpty.Text, "No submissions found") && !strings.Contains(responseEmpty.Text, "removed 0") {
		t.Errorf("Expected graceful handling of user with no submissions, got: %s", responseEmpty.Text)
	}

	// Suppress unused variable warnings for the test
	_ = submission1
	_ = submission2
	_ = submission3
}

// TDD: Test remove-submission command with username resolution
func TestAdminHandler_RemoveSubmissionWithUsernameResolution(t *testing.T) {
	// Set up test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Create SubmissionManager and add test data
	submissionManager := database.NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Add submissions using user ID
	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "User story to remove via username")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create admin handler with weekly automation (includes broadcast manager)
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithWeeklyAutomation(questionSelector, adminUsers, submissionManager, db, "fake-token")

	// Test case 1: Using user ID (should work as before)
	cmd := &AdminCommand{
		Action: "remove-submission",
		Args:   []string{"U111111111"}, // User ID format
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	if !strings.Contains(response.Text, "removed") {
		t.Errorf("Expected response to indicate removal for user ID, got: %s", response.Text)
	}

	// Verify submission was removed
	submissions, err := submissionManager.GetSubmissionsByUser(ctx, "U111111111")
	if err != nil {
		t.Fatalf("Failed to get user submissions: %v", err)
	}

	if len(submissions) != 0 {
		t.Errorf("Expected 0 submissions after removal via user ID, got %d", len(submissions))
	}

	// Test case 2: Username lookup (when no broadcast manager available in basic handler)
	// Re-create submission for username test
	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "Another story for username test")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create basic admin handler without broadcast manager
	basicAdminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	cmdUsername := &AdminCommand{
		Action: "remove-submission",
		Args:   []string{"testuser"}, // Username format (should fail gracefully)
	}

	responseUsername, err := basicAdminHandler.HandleAdminCommand(ctx, "U999999999", cmdUsername)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Should handle gracefully when broadcast manager not available
	if !strings.Contains(responseUsername.Text, "cannot lookup user") && !strings.Contains(responseUsername.Text, "broadcast manager not available") {
		t.Errorf("Expected error about broadcast manager not available, got: %s", responseUsername.Text)
	}
}

// TDD: Test remove-submission command with invalid arguments
func TestAdminHandler_RemoveSubmissionInvalidArgs(t *testing.T) {
	// Set up minimal test environment
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	submissionManager := database.NewSubmissionManager(db.DB)
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test command without username argument
	cmd := &AdminCommand{
		Action: "remove-submission",
		Args:   []string{}, // Missing username
	}

	response, err := adminHandler.HandleAdminCommand(context.Background(), "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Should return usage message
	if !strings.Contains(response.Text, "Usage:") {
		t.Errorf("Expected usage message for invalid args, got: %s", response.Text)
	}
}

// TDD: Test remove-submission cleans up assignment records to allow new assignments
func TestAdminHandler_RemoveSubmissionCleansUpAssignments(t *testing.T) {
	// Set up test database with weekly automation support
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	submissionManager := database.NewSubmissionManager(db.DB)
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	ctx := context.Background()

	// Create admin handler with weekly automation (needed for assignment functionality)
	adminHandler := NewAdminHandlerWithWeeklyAutomation(questionSelector, adminUsers, submissionManager, db, "fake-token")

	// Step 1: Create a weekly issue for current week (simulate real scenario)
	currentYear, currentWeek := 2025, 38
	issue, err := db.CreateWeeklyNewsletterIssue(currentWeek, currentYear)
	if err != nil {
		t.Fatalf("Failed to create weekly issue: %v", err)
	}

	// Step 2: Create an assignment for the user (simulating /pp assign-question feature @olle)
	userID := "U09EDEQSCV9"
	assignment := database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeFeature,
		QuestionID:  nil, // No specific question for this test
	}

	assignmentID, err := db.CreatePersonAssignment(assignment)
	if err != nil {
		t.Fatalf("Failed to create assignment: %v", err)
	}

	// Step 3: Create submissions for the user (simulating user submitting content)
	submission1, err := submissionManager.CreateNewsSubmission(ctx, userID, "Feature story content")
	if err != nil {
		t.Fatalf("Failed to create submission: %v", err)
	}

	submission2, err := submissionManager.CreateNewsSubmission(ctx, userID, "Another feature story")
	if err != nil {
		t.Fatalf("Failed to create submission: %v", err)
	}

	// Step 4: Link submissions to assignment (simulating submission processing)
	err = db.LinkSubmissionToAssignment(assignmentID, submission1.ID)
	if err != nil {
		t.Fatalf("Failed to link submission to assignment: %v", err)
	}

	// Step 5: Verify that user cannot get a new assignment (reproduces the bug)
	_, err = db.CreatePersonAssignment(database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeGeneral,
	})
	if err == nil {
		t.Fatal("Expected error when creating duplicate assignment, but got none")
	}
	if !strings.Contains(err.Error(), "already has an assignment") {
		t.Fatalf("Expected 'already has an assignment' error, got: %v", err)
	}

	// Step 6: Use remove-submission command
	cmd := &AdminCommand{
		Action: "remove-submission",
		Args:   []string{userID},
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	if !strings.Contains(response.Text, "removed 2") {
		t.Errorf("Expected to remove 2 submissions, got: %s", response.Text)
	}

	// Step 7: Verify submissions are gone
	submissions, err := submissionManager.GetSubmissionsByUser(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to get user submissions: %v", err)
	}
	if len(submissions) != 0 {
		t.Errorf("Expected 0 submissions after removal, got %d", len(submissions))
	}

	// Step 8: CRITICAL TEST - User should now be able to get a new assignment
	// This will FAIL until we implement assignment cleanup
	_, err = db.CreatePersonAssignment(database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeGeneral,
	})
	if err != nil {
		t.Errorf("Expected to be able to create new assignment after remove-submission, but got error: %v", err)
	}

	// Suppress unused variable warning
	_ = submission2
}

// TDD: Debug the real-world assignment issue
func TestDebugAssignmentIssue(t *testing.T) {
	// Set up test database that matches production scenario
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	submissionManager := database.NewSubmissionManager(db.DB)
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}

	// Create admin handler with weekly automation (for future use)
	_ = NewAdminHandlerWithWeeklyAutomation(questionSelector, adminUsers, submissionManager, db, "fake-token")

	// Create the same weekly issue as in production (Week 38, 2025)
	issue, err := db.CreateWeeklyNewsletterIssue(38, 2025)
	if err != nil {
		t.Fatalf("Failed to create weekly issue: %v", err)
	}

	userID := "U09EDEQSCV9"

	// Verify no assignments exist initially
	assignments, err := db.GetAssignmentsByUserAndIssue(userID, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get assignments: %v", err)
	}
	t.Logf("Initial assignments for user %s in issue %d: %d", userID, issue.ID, len(assignments))

	// Test 1: Try to create assignment directly (should work)
	_, err = db.CreatePersonAssignment(database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeFeature,
	})
	if err != nil {
		t.Fatalf("Failed to create first assignment: %v", err)
	}
	t.Logf("Successfully created first assignment")

	// Test 2: Try to create another assignment (should fail)
	_, err = db.CreatePersonAssignment(database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeGeneral,
	})
	if err == nil {
		t.Fatal("Expected error when creating duplicate assignment")
	}
	t.Logf("Correctly got error for duplicate assignment: %v", err)

	// Test 3: Remove assignments using our new method
	err = db.DeletePersonAssignmentsByUser(userID, issue.ID)
	if err != nil {
		t.Fatalf("Failed to delete assignments: %v", err)
	}
	t.Logf("Successfully deleted assignments")

	// Test 4: Verify assignments are gone
	assignments, err = db.GetAssignmentsByUserAndIssue(userID, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get assignments after deletion: %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("Expected 0 assignments after deletion, got %d", len(assignments))
	}
	t.Logf("Confirmed assignments are deleted")

	// Test 5: Try to create assignment again (should work now)
	_, err = db.CreatePersonAssignment(database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeGeneral,
	})
	if err != nil {
		t.Fatalf("Failed to create assignment after cleanup: %v", err)
	}
	t.Logf("Successfully created assignment after cleanup")
}

// TDD: Test unauthorized user cannot access submission management
func TestAdminHandler_UnauthorizedSubmissionAccess(t *testing.T) {
	// Set up test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Create admin handler
	submissionManager := database.NewSubmissionManager(db.DB)
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"} // Only this user is authorized
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test unauthorized user trying to list submissions
	cmd := &AdminCommand{
		Action: "list-submissions",
		Args:   []string{},
	}

	unauthorizedUserID := "U888888888"
	response, err := adminHandler.HandleAdminCommand(context.Background(), unauthorizedUserID, cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify unauthorized access is denied
	if !strings.Contains(response.Text, "not authorized") {
		t.Errorf("Expected unauthorized message, got: %s", response.Text)
	}
}
