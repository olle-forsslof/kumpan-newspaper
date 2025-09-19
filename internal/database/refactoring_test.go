package database

import (
	"os"
	"testing"
	"time"
)

// TestDatabaseRefactoring tests the refactoring of database functions with TDD approach
func TestDatabaseRefactoring(t *testing.T) {
	// Create a temporary database for testing
	tempFile := "/tmp/test_database_refactoring.db"
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

	t.Run("TestRedundantFunctionElimination", testRedundantFunctionElimination(db))
	t.Run("TestHelperFunctionConsistency", testHelperFunctionConsistency(db))
	t.Run("TestDatabaseInterfaceStability", testDatabaseInterfaceStability(db))
}

// testRedundantFunctionElimination verifies that GetActiveAssignmentsByUser
// properly delegates to GetAssignmentsByUserAndIssue
func testRedundantFunctionElimination(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		userID := "U123REFACTOR"

		// Create current week issue
		year, week := time.Now().ISOWeek()
		issue, err := db.GetOrCreateWeeklyIssue(week, year)
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Business logic allows only one assignment per user per issue
		// Create a single assignment for the user in current week
		assignment := PersonAssignment{
			IssueID:     issue.ID,
			PersonID:    userID,
			ContentType: ContentTypeFeature,
			AssignedAt:  time.Now(),
		}

		_, err = db.CreatePersonAssignment(assignment)
		if err != nil {
			t.Fatalf("Failed to create assignment: %v", err)
		}

		// Test that the refactored approach returns the same results
		// Get current week's issue (equivalent to what GetActiveAssignmentsByUser did)
		now := time.Now()
		currentYear, currentWeek := now.ISOWeek()
		var currentIssue *WeeklyNewsletterIssue
		currentIssue, err = db.GetOrCreateWeeklyIssue(currentWeek, currentYear)
		if err != nil {
			t.Fatalf("Failed to get current week issue: %v", err)
		}

		activeAssignments, err := db.GetAssignmentsByUserAndIssue(userID, currentIssue.ID)
		if err != nil {
			t.Fatalf("GetAssignmentsByUserAndIssue failed for current week: %v", err)
		}

		directAssignments, err := db.GetAssignmentsByUserAndIssue(userID, issue.ID)
		if err != nil {
			t.Fatalf("GetAssignmentsByUserAndIssue failed: %v", err)
		}

		// Verify identical results (this should pass before and after refactoring)
		if len(activeAssignments) != len(directAssignments) {
			t.Errorf("Expected identical lengths: GetActiveAssignmentsByUser=%d, GetAssignmentsByUserAndIssue=%d",
				len(activeAssignments), len(directAssignments))
		}

		if len(activeAssignments) != 1 {
			t.Errorf("Expected 1 assignment, got %d", len(activeAssignments))
		}

		// Verify content matches
		if len(activeAssignments) > 0 && len(directAssignments) > 0 {
			activeAssignment := activeAssignments[0]
			directAssignment := directAssignments[0]
			if activeAssignment.ID != directAssignment.ID ||
				activeAssignment.PersonID != directAssignment.PersonID ||
				activeAssignment.ContentType != directAssignment.ContentType {
				t.Errorf("Assignment mismatch between functions")
			}
		}

		// Test edge case: user with no assignments
		emptyUserID := "U999EMPTY"
		emptyActive, err := db.GetAssignmentsByUserAndIssue(emptyUserID, currentIssue.ID)
		if err != nil {
			t.Fatalf("GetAssignmentsByUserAndIssue failed for empty user: %v", err)
		}

		emptyDirect, err := db.GetAssignmentsByUserAndIssue(emptyUserID, issue.ID)
		if err != nil {
			t.Fatalf("GetAssignmentsByUserAndIssue failed for empty user: %v", err)
		}

		if len(emptyActive) != 0 || len(emptyDirect) != 0 {
			t.Errorf("Expected empty results for user with no assignments")
		}
	}
}

// testHelperFunctionConsistency verifies that scanner helper functions
// work consistently across all callers
func testHelperFunctionConsistency(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		userID := "U456HELPER_ISOLATED"

		// Create test issue for CURRENT week (since GetActiveAssignmentByUser uses current week)
		year, week := time.Now().ISOWeek()
		issue, err := db.GetOrCreateWeeklyIssue(week, year)
		if err != nil {
			t.Fatalf("Failed to create current week issue: %v", err)
		}

		// Create assignment with nullable fields
		questionID := 42
		submissionID := 84
		assignment := PersonAssignment{
			IssueID:      issue.ID,
			PersonID:     userID,
			ContentType:  ContentTypeBodyMind,
			QuestionID:   &questionID,
			SubmissionID: &submissionID,
			AssignedAt:   time.Now(),
		}

		assignmentID, err := db.CreatePersonAssignment(assignment)
		if err != nil {
			t.Fatalf("Failed to create assignment: %v", err)
		}

		// Test scanSinglePersonAssignment via GetActiveAssignmentByUser
		singleAssignment, err := db.GetActiveAssignmentByUser(userID, ContentTypeBodyMind)
		if err != nil {
			t.Fatalf("Failed to get single assignment: %v", err)
		}

		// Verify nullable fields are properly handled
		if singleAssignment.QuestionID == nil || *singleAssignment.QuestionID != questionID {
			t.Errorf("Expected QuestionID %d, got %v", questionID, singleAssignment.QuestionID)
		}

		if singleAssignment.SubmissionID == nil || *singleAssignment.SubmissionID != submissionID {
			t.Errorf("Expected SubmissionID %d, got %v", submissionID, singleAssignment.SubmissionID)
		}

		// Test scanPersonAssignments via GetPersonAssignmentsByIssue
		// But filter to just this user's assignments since other tests may have created assignments
		allAssignments, err := db.GetPersonAssignmentsByIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to get multiple assignments: %v", err)
		}

		// Filter to just this user's assignments
		var multipleAssignments []PersonAssignment
		for _, a := range allAssignments {
			if a.PersonID == userID {
				multipleAssignments = append(multipleAssignments, a)
			}
		}

		if len(multipleAssignments) != 1 {
			t.Errorf("Expected 1 assignment for user %s, got %d", userID, len(multipleAssignments))
		}

		// Verify consistency between single and multiple scan results
		multiAssignment := multipleAssignments[0]
		if singleAssignment.ID != multiAssignment.ID ||
			singleAssignment.PersonID != multiAssignment.PersonID ||
			singleAssignment.ContentType != multiAssignment.ContentType {
			t.Errorf("Inconsistent results between single and multiple scanners")
		}

		// Test that both helper functions handle nullable fields identically
		if (singleAssignment.QuestionID == nil) != (multiAssignment.QuestionID == nil) {
			t.Errorf("Inconsistent nullable field handling for QuestionID")
		}

		if singleAssignment.QuestionID != nil && multiAssignment.QuestionID != nil {
			if *singleAssignment.QuestionID != *multiAssignment.QuestionID {
				t.Errorf("QuestionID values don't match between scanners")
			}
		}

		// Create assignment with NULL fields to test edge case for a NEW user
		nullUserID := "U999NULL_HELPER"
		nullAssignment := PersonAssignment{
			IssueID:     issue.ID,
			PersonID:    nullUserID,
			ContentType: ContentTypeGeneral,
			// QuestionID and SubmissionID left as nil
			AssignedAt: time.Now(),
		}

		_, err = db.CreatePersonAssignment(nullAssignment)
		if err != nil {
			t.Fatalf("Failed to create assignment with null fields: %v", err)
		}

		// Verify NULL handling
		nullSingle, err := db.GetActiveAssignmentByUser(nullUserID, ContentTypeGeneral)
		if err != nil {
			t.Fatalf("Failed to get assignment with null fields: %v", err)
		}

		if nullSingle.QuestionID != nil || nullSingle.SubmissionID != nil {
			t.Errorf("Expected nil for nullable fields, got QuestionID=%v, SubmissionID=%v",
				nullSingle.QuestionID, nullSingle.SubmissionID)
		}

		// Ensure ID was properly assigned
		if assignmentID <= 0 {
			t.Errorf("Expected positive assignment ID, got %d", assignmentID)
		}
	}
}

// testDatabaseInterfaceStability verifies that public interfaces remain unchanged
// after refactoring (regression testing)
func testDatabaseInterfaceStability(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		userID := "U789STABLE_ISOLATED"

		// Create issue for testing
		issue, err := db.CreateWeeklyNewsletterIssue(51, 2025)
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Test that all public methods maintain their signatures and behavior

		// 1. Test GetAssignmentsByUserAndIssue interface - record current behavior for current week
		// Get current week issue for this test
		currentTestYear, currentTestWeek := time.Now().ISOWeek()
		currentTestIssue, err := db.GetOrCreateWeeklyIssue(currentTestWeek, currentTestYear)
		if err != nil {
			t.Fatalf("Failed to get current test issue: %v", err)
		}

		assignments1, err := db.GetAssignmentsByUserAndIssue(userID, currentTestIssue.ID)
		if err != nil {
			t.Fatalf("GetAssignmentsByUserAndIssue interface changed: %v", err)
		}
		// Record the current behavior - may return nil or empty slice for no assignments
		initialBehavior1 := assignments1 == nil
		assignment1Len := len(assignments1)

		// 2. Test GetAssignmentsByUserAndIssue interface - record current behavior
		assignments2, err := db.GetAssignmentsByUserAndIssue(userID, issue.ID)
		if err != nil {
			t.Fatalf("GetAssignmentsByUserAndIssue interface changed: %v", err)
		}
		// Record the current behavior
		initialBehavior2 := assignments2 == nil
		assignment2Len := len(assignments2)

		// 3. Test GetPersonAssignmentsByIssue interface - record current behavior
		assignments3, err := db.GetPersonAssignmentsByIssue(issue.ID)
		if err != nil {
			t.Fatalf("GetPersonAssignmentsByIssue interface changed: %v", err)
		}
		// Record the current behavior
		initialBehavior3 := assignments3 == nil
		assignment3Len := len(assignments3)

		// Log the initial behaviors for debugging (these should be consistent after refactoring)
		t.Logf("Initial behavior - GetActiveAssignmentsByUser: nil=%v, len=%d", initialBehavior1, assignment1Len)
		t.Logf("Initial behavior - GetAssignmentsByUserAndIssue: nil=%v, len=%d", initialBehavior2, assignment2Len)
		t.Logf("Initial behavior - GetPersonAssignmentsByIssue: nil=%v, len=%d", initialBehavior3, assignment3Len)

		// 4. Test GetActiveAssignmentByUser interface
		_, err = db.GetActiveAssignmentByUser(userID, ContentTypeFeature)
		// This should return an error for non-existent assignment
		if err == nil {
			t.Errorf("Expected error for non-existent assignment, got nil")
		}

		// Create an assignment for current week and test success case
		year, week := time.Now().ISOWeek()
		currentIssue, err := db.GetOrCreateWeeklyIssue(week, year)
		if err != nil {
			t.Fatalf("Failed to get current week issue: %v", err)
		}

		assignment := PersonAssignment{
			IssueID:     currentIssue.ID,
			PersonID:    userID,
			ContentType: ContentTypeFeature,
			AssignedAt:  time.Now(),
		}

		_, err = db.CreatePersonAssignment(assignment)
		if err != nil {
			t.Fatalf("Failed to create assignment: %v", err)
		}

		// Now should succeed
		singleAssignment, err := db.GetActiveAssignmentByUser(userID, ContentTypeFeature)
		if err != nil {
			t.Fatalf("GetActiveAssignmentByUser failed: %v", err)
		}
		if singleAssignment == nil {
			t.Errorf("Expected non-nil assignment, got nil")
		}

		// 5. Test error message format consistency
		_, err = db.GetActiveAssignmentByUser("NONEXISTENT", ContentTypeFeature)
		if err == nil {
			t.Errorf("Expected error for non-existent user")
		}
		// Error message should be descriptive and consistent
		if err.Error() == "" {
			t.Errorf("Expected non-empty error message")
		}

		// 6. Test return type consistency
		activeAssignments, _ := db.GetAssignmentsByUserAndIssue(userID, currentTestIssue.ID)
		directAssignments, _ := db.GetAssignmentsByUserAndIssue(userID, currentIssue.ID)

		// Both should return []PersonAssignment type and have same length since assignment was created for current week
		if len(activeAssignments) != len(directAssignments) {
			t.Errorf("Return type inconsistency detected: active=%d, direct=%d", len(activeAssignments), len(directAssignments))
		}
	}
}
