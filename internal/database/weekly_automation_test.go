package database

import (
	"os"
	"testing"
	"time"
)

func TestWeeklyAutomationDatabase(t *testing.T) {
	// Create a temporary database for testing
	tempFile := "/tmp/test_weekly_automation.db"
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

	t.Run("CreateWeeklyNewsletterIssue", testCreateWeeklyNewsletterIssue(db))
	t.Run("PersonAssignments", testPersonAssignments(db))
	t.Run("BodyMindQuestions", testBodyMindQuestions(db))
	t.Run("PersonRotationHistory", testPersonRotationHistory(db))
}

func testCreateWeeklyNewsletterIssue(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Test creating a weekly newsletter issue
		weekNumber := 37
		year := 2025

		issue, err := db.CreateWeeklyNewsletterIssue(weekNumber, year)
		if err != nil {
			t.Fatalf("Failed to create weekly newsletter issue: %v", err)
		}

		// Validate the created issue
		if issue.WeekNumber != weekNumber {
			t.Errorf("Expected week number %d, got %d", weekNumber, issue.WeekNumber)
		}

		if issue.Year != year {
			t.Errorf("Expected year %d, got %d", year, issue.Year)
		}

		if issue.Status != IssueStatusDraft {
			t.Errorf("Expected status %s, got %s", IssueStatusDraft, issue.Status)
		}

		expectedTitle := "Week 37 Newsletter - 2025"
		if issue.Title != expectedTitle {
			t.Errorf("Expected title %s, got %s", expectedTitle, issue.Title)
		}

		// Test GetOrCreateWeeklyIssue with existing issue
		existingIssue, err := db.GetOrCreateWeeklyIssue(weekNumber, year)
		if err != nil {
			t.Fatalf("Failed to get existing weekly issue: %v", err)
		}

		if existingIssue.ID != issue.ID {
			t.Errorf("Expected same issue ID %d, got %d", issue.ID, existingIssue.ID)
		}

		// Test GetOrCreateWeeklyIssue with new week
		newIssue, err := db.GetOrCreateWeeklyIssue(38, year)
		if err != nil {
			t.Fatalf("Failed to create new weekly issue: %v", err)
		}

		if newIssue.WeekNumber != 38 {
			t.Errorf("Expected week number 38, got %d", newIssue.WeekNumber)
		}

		// Test publication date calculation (Thursday at 9:30 AM)
		if newIssue.PublicationDate.Weekday() != time.Thursday {
			t.Errorf("Expected publication date to be Thursday, got %s", newIssue.PublicationDate.Weekday())
		}

		if newIssue.PublicationDate.Hour() != 9 || newIssue.PublicationDate.Minute() != 30 {
			t.Errorf("Expected publication time 9:30 AM, got %02d:%02d",
				newIssue.PublicationDate.Hour(), newIssue.PublicationDate.Minute())
		}
	}
}

func testPersonAssignments(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// First create a newsletter issue
		issue, err := db.CreateWeeklyNewsletterIssue(39, 2025)
		if err != nil {
			t.Fatalf("Failed to create newsletter issue: %v", err)
		}

		// Test creating person assignments
		assignments := []PersonAssignment{
			{
				IssueID:     issue.ID,
				PersonID:    "U123456",
				ContentType: ContentTypeFeature,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue.ID,
				PersonID:    "U234567",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
			{
				IssueID:     issue.ID,
				PersonID:    "U345678",
				ContentType: ContentTypeGeneral,
				AssignedAt:  time.Now(),
			},
		}

		var createdIDs []int
		for _, assignment := range assignments {
			id, err := db.CreatePersonAssignment(assignment)
			if err != nil {
				t.Fatalf("Failed to create person assignment: %v", err)
			}
			createdIDs = append(createdIDs, id)
		}

		// Test retrieving assignments by issue
		retrievedAssignments, err := db.GetPersonAssignmentsByIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to get person assignments: %v", err)
		}

		if len(retrievedAssignments) != len(assignments) {
			t.Errorf("Expected %d assignments, got %d", len(assignments), len(retrievedAssignments))
		}

		// Verify the assignments match what we created
		for i, assignment := range retrievedAssignments {
			if assignment.IssueID != issue.ID {
				t.Errorf("Assignment %d: expected issue ID %d, got %d", i, issue.ID, assignment.IssueID)
			}

			expectedPersonID := assignments[i].PersonID
			if assignment.PersonID != expectedPersonID {
				t.Errorf("Assignment %d: expected person ID %s, got %s", i, expectedPersonID, assignment.PersonID)
			}

			expectedContentType := assignments[i].ContentType
			if assignment.ContentType != expectedContentType {
				t.Errorf("Assignment %d: expected content type %s, got %s", i, expectedContentType, assignment.ContentType)
			}
		}

		// Test validation
		invalidAssignment := PersonAssignment{
			IssueID:     0, // Invalid
			PersonID:    "U123456",
			ContentType: ContentTypeFeature,
			AssignedAt:  time.Now(),
		}

		_, err = db.CreatePersonAssignment(invalidAssignment)
		if err == nil {
			t.Error("Expected validation error for invalid assignment, got nil")
		}
	}
}

func testBodyMindQuestions(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Test creating body/mind questions
		questions := []struct {
			text     string
			category string
		}{
			{"How do you manage stress during busy periods?", "wellness"},
			{"What's your favorite way to disconnect after work?", "work_life_balance"},
			{"How do you practice mindfulness in your daily routine?", "mental_health"},
		}

		var createdIDs []int
		for _, q := range questions {
			id, err := db.CreateBodyMindQuestion(q.text, q.category)
			if err != nil {
				t.Fatalf("Failed to create body/mind question: %v", err)
			}
			createdIDs = append(createdIDs, id)
		}

		// Test getting active questions
		activeQuestions, err := db.GetActiveBodyMindQuestions()
		if err != nil {
			t.Fatalf("Failed to get active body/mind questions: %v", err)
		}

		if len(activeQuestions) != len(questions) {
			t.Errorf("Expected %d active questions, got %d", len(questions), len(activeQuestions))
		}

		// Test getting questions by category
		wellnessQuestions, err := db.GetBodyMindQuestionsByCategory("wellness")
		if err != nil {
			t.Fatalf("Failed to get wellness questions: %v", err)
		}

		if len(wellnessQuestions) != 1 {
			t.Errorf("Expected 1 wellness question, got %d", len(wellnessQuestions))
		}

		if wellnessQuestions[0].Category != "wellness" {
			t.Errorf("Expected wellness category, got %s", wellnessQuestions[0].Category)
		}

		// Test marking question as used
		questionID := createdIDs[0]
		err = db.MarkBodyMindQuestionUsed(questionID)
		if err != nil {
			t.Fatalf("Failed to mark question as used: %v", err)
		}

		// Verify the question is no longer active
		activeQuestions, err = db.GetActiveBodyMindQuestions()
		if err != nil {
			t.Fatalf("Failed to get active questions after marking one as used: %v", err)
		}

		if len(activeQuestions) != len(questions)-1 {
			t.Errorf("Expected %d active questions after marking one as used, got %d",
				len(questions)-1, len(activeQuestions))
		}
	}
}

func testPersonRotationHistory(db *DB) func(t *testing.T) {
	return func(t *testing.T) {
		personID := "U123456"
		contentType := ContentTypeGeneral

		// Add some rotation history entries
		weeks := []struct {
			week int
			year int
		}{
			{35, 2025},
			{32, 2025},
			{29, 2025},
		}

		for _, w := range weeks {
			err := db.AddPersonRotationHistory(personID, contentType, w.week, w.year)
			if err != nil {
				t.Fatalf("Failed to add rotation history for week %d: %v", w.week, err)
			}
		}

		// Test getting rotation history
		history, err := db.GetPersonRotationHistory(personID, contentType, 10)
		if err != nil {
			t.Fatalf("Failed to get person rotation history: %v", err)
		}

		if len(history) != len(weeks) {
			t.Errorf("Expected %d history entries, got %d", len(weeks), len(history))
		}

		// Verify entries are returned in descending order (most recent first)
		for i := 0; i < len(history)-1; i++ {
			current := history[i]
			next := history[i+1]

			// Current should be more recent than next
			if current.Year < next.Year || (current.Year == next.Year && current.WeekNumber < next.WeekNumber) {
				t.Errorf("History not sorted correctly: entry %d (week %d, year %d) should be after entry %d (week %d, year %d)",
					i, current.WeekNumber, current.Year, i+1, next.WeekNumber, next.Year)
			}
		}

		// Test with different person - should return empty
		emptyHistory, err := db.GetPersonRotationHistory("U999999", contentType, 10)
		if err != nil {
			t.Fatalf("Failed to get rotation history for non-existent person: %v", err)
		}

		if len(emptyHistory) != 0 {
			t.Errorf("Expected empty history for non-existent person, got %d entries", len(emptyHistory))
		}
	}
}

func TestWeeklyIssueValidation(t *testing.T) {
	tests := []struct {
		name        string
		issue       WeeklyNewsletterIssue
		expectError bool
	}{
		{
			name: "Valid issue",
			issue: WeeklyNewsletterIssue{
				WeekNumber: 25,
				Year:       2025,
				Status:     IssueStatusDraft,
			},
			expectError: false,
		},
		{
			name: "Invalid week number - too low",
			issue: WeeklyNewsletterIssue{
				WeekNumber: 0,
				Year:       2025,
				Status:     IssueStatusDraft,
			},
			expectError: true,
		},
		{
			name: "Invalid week number - too high",
			issue: WeeklyNewsletterIssue{
				WeekNumber: 54,
				Year:       2025,
				Status:     IssueStatusDraft,
			},
			expectError: true,
		},
		{
			name: "Invalid year - too low",
			issue: WeeklyNewsletterIssue{
				WeekNumber: 25,
				Year:       2019,
				Status:     IssueStatusDraft,
			},
			expectError: true,
		},
		{
			name: "Invalid status",
			issue: WeeklyNewsletterIssue{
				WeekNumber: 25,
				Year:       2025,
				Status:     "invalid_status",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.issue.Validate()
			if tt.expectError && err == nil {
				t.Error("Expected validation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no validation error, got %v", err)
			}
		})
	}
}

func TestPersonAssignmentValidation(t *testing.T) {
	tests := []struct {
		name        string
		assignment  PersonAssignment
		expectError bool
	}{
		{
			name: "Valid assignment",
			assignment: PersonAssignment{
				IssueID:     1,
				PersonID:    "U123456",
				ContentType: ContentTypeFeature,
			},
			expectError: false,
		},
		{
			name: "Invalid content type",
			assignment: PersonAssignment{
				IssueID:     1,
				PersonID:    "U123456",
				ContentType: "invalid_type",
			},
			expectError: true,
		},
		{
			name: "Empty person ID",
			assignment: PersonAssignment{
				IssueID:     1,
				PersonID:    "",
				ContentType: ContentTypeFeature,
			},
			expectError: true,
		},
		{
			name: "Invalid issue ID",
			assignment: PersonAssignment{
				IssueID:     0,
				PersonID:    "U123456",
				ContentType: ContentTypeFeature,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.assignment.Validate()
			if tt.expectError && err == nil {
				t.Error("Expected validation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no validation error, got %v", err)
			}
		})
	}
}

func TestBodyMindQuestionValidation(t *testing.T) {
	tests := []struct {
		name        string
		question    BodyMindQuestion
		expectError bool
	}{
		{
			name: "Valid question",
			question: BodyMindQuestion{
				QuestionText: "How do you stay motivated?",
				Category:     "wellness",
				Status:       "active",
			},
			expectError: false,
		},
		{
			name: "Empty question text",
			question: BodyMindQuestion{
				QuestionText: "",
				Category:     "wellness",
				Status:       "active",
			},
			expectError: true,
		},
		{
			name: "Invalid category",
			question: BodyMindQuestion{
				QuestionText: "How do you stay motivated?",
				Category:     "invalid_category",
				Status:       "active",
			},
			expectError: true,
		},
		{
			name: "Invalid status",
			question: BodyMindQuestion{
				QuestionText: "How do you stay motivated?",
				Category:     "wellness",
				Status:       "invalid_status",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.question.Validate()
			if tt.expectError && err == nil {
				t.Error("Expected validation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no validation error, got %v", err)
			}
		})
	}
}
