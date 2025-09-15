package database

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestBodyMindPoolManager(t *testing.T) {
	// Create a temporary database for testing
	tempFile := "/tmp/test_body_mind_pool.db"
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

	pm := NewBodyMindPoolManager(db)

	t.Run("EmptyPoolStatus", testEmptyPoolStatus(pm))
	t.Run("AddQuestionsToPool", testAddQuestionsToPool(pm))
	t.Run("SelectQuestionForNewsletter", testSelectQuestionForNewsletter(pm))
	t.Run("BulkAddQuestions", testBulkAddQuestions(pm))
	t.Run("PoolMetrics", testPoolMetrics(pm))
	t.Run("SlackFormatting", testSlackFormatting(pm))
}

func testEmptyPoolStatus(pm *BodyMindPoolManager) func(t *testing.T) {
	return func(t *testing.T) {
		status, err := pm.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get empty pool status: %v", err)
		}

		if status.TotalActive != 0 {
			t.Errorf("Expected 0 active questions, got %d", status.TotalActive)
		}

		if !status.LowPoolWarning {
			t.Error("Expected low pool warning for empty pool")
		}

		if !strings.Contains(status.RecommendedAction, "URGENT") {
			t.Errorf("Expected urgent action for empty pool, got: %s", status.RecommendedAction)
		}

		// Test selecting from empty pool should fail
		_, err = pm.SelectQuestionForNewsletter()
		if err == nil {
			t.Error("Expected error when selecting from empty pool")
		}
	}
}

func testAddQuestionsToPool(pm *BodyMindPoolManager) func(t *testing.T) {
	return func(t *testing.T) {
		// Test adding valid questions
		questions := []struct {
			text     string
			category string
		}{
			{"How do you manage work stress?", "wellness"},
			{"What's your favorite mindfulness practice?", "mental_health"},
			{"How do you maintain work-life balance?", "work_life_balance"},
			{"What helps you stay motivated?", "wellness"},
			{"How do you handle difficult conversations?", "mental_health"},
		}

		var addedQuestions []*BodyMindQuestion
		for _, q := range questions {
			question, err := pm.AddQuestionToPool(q.text, q.category)
			if err != nil {
				t.Fatalf("Failed to add question '%s': %v", q.text, err)
			}

			if question.QuestionText != q.text {
				t.Errorf("Expected question text '%s', got '%s'", q.text, question.QuestionText)
			}

			if question.Category != q.category {
				t.Errorf("Expected category '%s', got '%s'", q.category, question.Category)
			}

			if question.Status != "active" {
				t.Errorf("Expected status 'active', got '%s'", question.Status)
			}

			addedQuestions = append(addedQuestions, question)
		}

		// Test pool status after adding questions
		status, err := pm.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get pool status after adding questions: %v", err)
		}

		if status.TotalActive != len(questions) {
			t.Errorf("Expected %d active questions, got %d", len(questions), status.TotalActive)
		}

		// Verify category breakdown
		expectedBreakdown := map[string]int{
			"wellness":          2,
			"mental_health":     2,
			"work_life_balance": 1,
		}

		for category, expectedCount := range expectedBreakdown {
			if status.CategoryBreakdown[category] != expectedCount {
				t.Errorf("Expected %d questions in category '%s', got %d",
					expectedCount, category, status.CategoryBreakdown[category])
			}
		}

		// Pool should no longer have urgent warning
		if strings.Contains(status.RecommendedAction, "URGENT") {
			t.Errorf("Pool should not have urgent warning with %d questions", status.TotalActive)
		}

		// Test adding question with invalid category
		_, err = pm.AddQuestionToPool("Test question", "invalid_category")
		if err == nil {
			t.Error("Expected error when adding question with invalid category")
		}
	}
}

func testSelectQuestionForNewsletter(pm *BodyMindPoolManager) func(t *testing.T) {
	return func(t *testing.T) {
		// Get initial pool status
		initialStatus, err := pm.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get initial pool status: %v", err)
		}

		initialCount := initialStatus.TotalActive

		// Select a question
		selectedQuestion, err := pm.SelectQuestionForNewsletter()
		if err != nil {
			t.Fatalf("Failed to select question for newsletter: %v", err)
		}

		if selectedQuestion.QuestionText == "" {
			t.Error("Selected question should have text")
		}

		// Verify question was marked as used
		if selectedQuestion.Status != "used" {
			t.Errorf("Expected selected question status to be 'used', got '%s'", selectedQuestion.Status)
		}

		// Verify pool count decreased
		newStatus, err := pm.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get pool status after selection: %v", err)
		}

		expectedCount := initialCount - 1
		if newStatus.TotalActive != expectedCount {
			t.Errorf("Expected %d active questions after selection, got %d",
				expectedCount, newStatus.TotalActive)
		}

		// Test FIFO behavior by selecting multiple questions
		_, err = pm.SelectQuestionForNewsletter()
		if err != nil {
			t.Fatalf("Failed to select first question: %v", err)
		}

		// Add a small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)

		// Add a new question
		newQuestion, err := pm.AddQuestionToPool("New test question", "wellness")
		if err != nil {
			t.Fatalf("Failed to add new question: %v", err)
		}

		question2, err := pm.SelectQuestionForNewsletter()
		if err != nil {
			t.Fatalf("Failed to select second question: %v", err)
		}

		// The older question should be selected before the newer one
		if question2.ID == newQuestion.ID {
			t.Error("FIFO order not maintained - newer question was selected before older ones")
		}
	}
}

func testBulkAddQuestions(pm *BodyMindPoolManager) func(t *testing.T) {
	return func(t *testing.T) {
		// Prepare bulk questions
		bulkQuestions := []struct {
			Text     string
			Category string
		}{
			{"What's your go-to stress relief technique?", "wellness"},
			{"How do you practice gratitude daily?", "mental_health"},
			{"What boundary helps you most at work?", "work_life_balance"},
			{"How do you recharge after a long day?", "wellness"},
		}

		// Add questions in bulk
		addedQuestions, err := pm.BulkAddQuestions(bulkQuestions)
		if err != nil {
			t.Fatalf("Failed to bulk add questions: %v", err)
		}

		if len(addedQuestions) != len(bulkQuestions) {
			t.Errorf("Expected %d questions to be added, got %d", len(bulkQuestions), len(addedQuestions))
		}

		// Verify all questions were added correctly
		for i, question := range addedQuestions {
			expectedText := bulkQuestions[i].Text
			expectedCategory := bulkQuestions[i].Category

			if question.QuestionText != expectedText {
				t.Errorf("Question %d: expected text '%s', got '%s'", i, expectedText, question.QuestionText)
			}

			if question.Category != expectedCategory {
				t.Errorf("Question %d: expected category '%s', got '%s'", i, expectedCategory, question.Category)
			}
		}

		// Test bulk add with some invalid questions
		mixedQuestions := []struct {
			Text     string
			Category string
		}{
			{"Valid question", "wellness"},
			{"Invalid category question", "invalid_category"},
			{"Another valid question", "mental_health"},
		}

		addedMixed, err := pm.BulkAddQuestions(mixedQuestions)
		if err == nil {
			t.Error("Expected error when bulk adding questions with invalid categories")
		}

		// Should still add the valid ones
		if len(addedMixed) != 2 {
			t.Errorf("Expected 2 valid questions to be added from mixed batch, got %d", len(addedMixed))
		}
	}
}

func testPoolMetrics(pm *BodyMindPoolManager) func(t *testing.T) {
	return func(t *testing.T) {
		// Get pool metrics
		metrics, err := pm.GetPoolMetrics()
		if err != nil {
			t.Fatalf("Failed to get pool metrics: %v", err)
		}

		// Verify basic structure
		if metrics.PoolStatus.TotalActive <= 0 {
			t.Error("Expected some active questions in metrics")
		}

		if len(metrics.PoolStatus.CategoryBreakdown) == 0 {
			t.Error("Expected category breakdown in metrics")
		}

		// Verify usage stats structure
		if metrics.UsageStats.MostUsedCategory == "" {
			// This is OK if no questions have been used yet
		}

		// Verify timestamp is recent
		if time.Since(metrics.LastUpdated) > time.Minute {
			t.Error("Metrics timestamp should be recent")
		}
	}
}

func testSlackFormatting(pm *BodyMindPoolManager) func(t *testing.T) {
	return func(t *testing.T) {
		// Get pool status
		status, err := pm.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get pool status for formatting test: %v", err)
		}

		// Format for Slack
		slackMessage := pm.FormatPoolStatusForSlack(status)

		// Verify message structure
		if !strings.Contains(slackMessage, "Body/Mind Question Pool Status") {
			t.Error("Slack message should contain title")
		}

		if !strings.Contains(slackMessage, "Available Questions:") {
			t.Error("Slack message should contain available questions count")
		}

		// Verify categories are formatted properly
		expectedCategories := []string{"Wellness", "Mental Health", "Work-Life Balance"}
		for _, category := range expectedCategories {
			if !strings.Contains(slackMessage, category) {
				t.Errorf("Slack message should contain formatted category: %s", category)
			}
		}

		// Verify recommended action is included
		if !strings.Contains(slackMessage, status.RecommendedAction) {
			t.Error("Slack message should contain recommended action")
		}

		// Test with empty pool
		emptyDB, err := NewSimple("/tmp/test_empty_pool.db")
		if err != nil {
			t.Fatalf("Failed to create empty test database: %v", err)
		}
		defer emptyDB.Close()
		defer os.Remove("/tmp/test_empty_pool.db")

		if err := emptyDB.Migrate(); err != nil {
			t.Fatalf("Failed to migrate empty test database: %v", err)
		}

		emptyPM := NewBodyMindPoolManager(emptyDB)
		emptyStatus, err := emptyPM.GetPoolStatus()
		if err != nil {
			t.Fatalf("Failed to get empty pool status: %v", err)
		}

		emptyMessage := emptyPM.FormatPoolStatusForSlack(emptyStatus)
		if !strings.Contains(emptyMessage, "URGENT") {
			t.Error("Empty pool message should contain urgent warning")
		}
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("FormatCategoryName", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"wellness", "Wellness"},
			{"mental_health", "Mental Health"},
			{"work_life_balance", "Work-Life Balance"},
			{"unknown", "unknown"},
		}

		for _, test := range tests {
			result := formatCategoryName(test.input)
			if result != test.expected {
				t.Errorf("formatCategoryName(%s): expected %s, got %s", test.input, test.expected, result)
			}
		}
	})

	t.Run("FormatDaysAgo", func(t *testing.T) {
		tests := []struct {
			input    int
			expected string
		}{
			{0, "Today"},
			{1, "1 day ago"},
			{3, "3 days ago"},
			{7, "1 week ago"},
			{14, "2 weeks ago"},
		}

		for _, test := range tests {
			result := formatDaysAgo(test.input)
			if result != test.expected {
				t.Errorf("formatDaysAgo(%d): expected %s, got %s", test.input, test.expected, result)
			}
		}
	})
}
