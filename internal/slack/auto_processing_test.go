package slack

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/ai"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TDD: Test automatic AI processing when submission is created
func TestSlackBot_AutoProcessSubmission(t *testing.T) {
	// This test should FAIL initially as auto-processing doesn't exist

	// Setup real test database for AI processing
	tempDir := t.TempDir()
	dbPath := fmt.Sprintf("%s/test.db", tempDir)

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Create mock services
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}

	// Create enhanced bot with AI processing capability and database
	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		nil,                  // question selector
		[]string{"U1234567"}, // admin users
		mockSubmissionManager,
		mockAIService,
		db, // Use real database for AI processing
	)

	// Simulate news submission command
	command := SlashCommand{
		Command: "/pp",
		Text:    "submit Our team launched a new analytics dashboard!",
		UserID:  "U987654321",
	}

	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Give async processing more time to complete and retry if needed
	var processCallCount int
	for i := 0; i < 10; i++ { // Try up to 10 times with 200ms intervals
		time.Sleep(200 * time.Millisecond)
		processCallCount = len(mockAIService.ProcessAndSaveCalls)
		if processCallCount > 0 {
			break
		}
	}

	// Verify submission was stored
	if len(mockSubmissionManager.CreatedSubmissions) != 1 {
		t.Errorf("Expected 1 created submission, got %d", len(mockSubmissionManager.CreatedSubmissions))
	}

	// Verify AI processing was triggered automatically
	if processCallCount != 1 {
		t.Errorf("Expected 1 processed submission, got %d", processCallCount)
	}

	// Verify response indicates both storage and processing
	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	// Response should indicate successful processing
	responseText := response.Text
	if responseText == "" {
		t.Error("Expected non-empty response text")
	}
}

// TDD: Test automatic processing with user information enrichment
func TestSlackBot_AutoProcessWithUserInfo(t *testing.T) {
	// This test should FAIL initially as user info enrichment doesn't exist in auto-processing

	// Setup real test database for AI processing
	tempDir := t.TempDir()
	dbPath := fmt.Sprintf("%s/test.db", tempDir)

	testDB, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testDB.Close()

	if err := testDB.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}

	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		nil,
		[]string{"U1234567"},
		mockSubmissionManager,
		mockAIService,
		testDB,
	)

	command := SlashCommand{
		Command: "/pp",
		Text:    "submit Our team launched a new analytics dashboard!",
		UserID:  "U987654321",
	}

	_, cmdErr := bot.HandleSlashCommand(context.Background(), command)
	if cmdErr != nil {
		t.Fatalf("HandleSlashCommand failed: %v", cmdErr)
	}

	// Give async processing more time to complete and retry if needed
	var processCallCount int
	for i := 0; i < 10; i++ { // Try up to 10 times with 200ms intervals
		time.Sleep(200 * time.Millisecond)
		processCallCount = len(mockAIService.ProcessAndSaveCalls)
		if processCallCount > 0 {
			break
		}
	}

	// Verify AI processing was called with user information
	if processCallCount != 1 {
		t.Errorf("Expected 1 user-enriched processing call, got %d", processCallCount)
	}

	// Check that user info was passed correctly
	processCall := mockAIService.ProcessAndSaveCalls[0]
	if processCall.AuthorName == "" {
		t.Error("Expected non-empty author name in processing call")
	}

	if processCall.AuthorDepartment == "" {
		t.Error("Expected non-empty author department in processing call")
	}
}

// TDD: Test automatic journalist type selection for news submissions (no question)
// All news submissions without questions should default to "general"
func TestSlackBot_AutoJournalistSelection(t *testing.T) {
	testCases := []struct {
		name               string
		content            string
		expectedJournalist string
	}{
		{
			name:               "Feature story - should default to general",
			content:            "Our team launched an amazing new feature that transforms how users interact with our platform",
			expectedJournalist: "general", // Changed expectation - no question means general
		},
		{
			name:               "Interview content - should default to general",
			content:            "I'm Sarah Johnson, new software developer. I studied at UBC and worked at startups before joining here",
			expectedJournalist: "general", // Changed expectation - no question means general
		},
		{
			name:               "General announcement",
			content:            "The office parking lot will be closed next week for maintenance",
			expectedJournalist: "general", // Stays the same
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSubmissionManager := &MockSubmissionManager{}
			mockAIService := &MockAIService{}
			mockDB := NewMockDatabase()

			// Mock submission manager should return submission with NO question ID (news submission)
			mockSubmissionManager.NextSubmission = &database.Submission{
				ID:         1,
				UserID:     "U987654321",
				QuestionID: nil, // No question - general news submission
				Content:    tc.content,
			}

			bot := NewBotWithDatabase(
				SlackConfig{Token: "test-token"},
				nil, // No question selector needed for news submissions
				[]string{"U1234567"},
				mockSubmissionManager,
				mockAIService,
				mockDB,
			)

			command := SlashCommand{
				Command: "/pp",
				Text:    "submit " + tc.content,
				UserID:  "U987654321",
			}

			_, cmdErr := bot.HandleSlashCommand(context.Background(), command)
			if cmdErr != nil {
				t.Fatalf("HandleSlashCommand failed: %v", cmdErr)
			}

			// Verify correct journalist type was selected
			if len(mockAIService.ProcessAndSaveCalls) != 1 {
				t.Fatalf("Expected 1 processing call, got %d", len(mockAIService.ProcessAndSaveCalls))
			}

			processCall := mockAIService.ProcessAndSaveCalls[0]
			if processCall.JournalistType != tc.expectedJournalist {
				t.Errorf("Expected journalist type %s, got %s", tc.expectedJournalist, processCall.JournalistType)
			}
		})
	}
}

// Mock structures for testing

type MockSubmissionManager struct {
	CreatedSubmissions []database.Submission
	NextSubmission     *database.Submission // Pre-configured submission for testing
	Error              error
}

func (m *MockSubmissionManager) CreateNewsSubmission(ctx context.Context, userID, content string) (*database.Submission, error) {
	if m.Error != nil {
		return nil, m.Error
	}

	var submission database.Submission
	if m.NextSubmission != nil {
		// Use the pre-configured submission for testing
		submission = *m.NextSubmission
		submission.UserID = userID
		submission.Content = content
	} else {
		// Default behavior
		submission = database.Submission{
			ID:      len(m.CreatedSubmissions) + 1,
			UserID:  userID,
			Content: content,
		}
	}

	m.CreatedSubmissions = append(m.CreatedSubmissions, submission)
	return &submission, nil
}

func (m *MockSubmissionManager) GetSubmissionsByUser(ctx context.Context, userID string) ([]database.Submission, error) {
	return nil, nil // Not needed for these tests
}

func (m *MockSubmissionManager) GetAllSubmissions(ctx context.Context) ([]database.Submission, error) {
	return nil, nil // Not needed for these tests
}

type MockAIService struct {
	ProcessedSubmissions  []database.Submission
	ProcessedWithUserInfo []ProcessWithUserInfoCall
	ProcessAndSaveCalls   []ProcessAndSaveCall
	Error                 error
}

type ProcessWithUserInfoCall struct {
	Submission       database.Submission
	AuthorName       string
	AuthorDepartment string
	JournalistType   string
}

type ProcessAndSaveCall struct {
	Submission        database.Submission
	AuthorName        string
	AuthorDepartment  string
	JournalistType    string
	NewsletterIssueID *int
}

func (m *MockAIService) ProcessSubmissionWithUserInfo(ctx context.Context, submission database.Submission, authorName, authorDepartment, journalistType string) (*database.ProcessedArticle, error) {
	// Log that this method is being called for debugging
	fmt.Printf("DEBUG: MockAIService.ProcessSubmissionWithUserInfo called with submission ID: %d\n", submission.ID)

	if m.Error != nil {
		return nil, m.Error
	}

	call := ProcessWithUserInfoCall{
		Submission:       submission,
		AuthorName:       authorName,
		AuthorDepartment: authorDepartment,
		JournalistType:   journalistType,
	}
	m.ProcessedWithUserInfo = append(m.ProcessedWithUserInfo, call)

	// Return mock processed article
	return &database.ProcessedArticle{
		ID:               1,
		SubmissionID:     submission.ID,
		JournalistType:   journalistType,
		ProcessedContent: `{"headline": "Test", "content": "Test content", "byline": "Test Writer"}`,
		ProcessingStatus: database.ProcessingStatusSuccess,
		WordCount:        10,
	}, nil
}

// Implement other AIService methods as no-ops for testing
func (m *MockAIService) ProcessSubmission(ctx context.Context, submission database.Submission, journalistType string) (*database.ProcessedArticle, error) {
	return nil, nil
}

func (m *MockAIService) GetAvailableJournalists() []string {
	return []string{"feature", "interview", "general", "body_mind"}
}

func (m *MockAIService) ValidateJournalistType(journalistType string) bool {
	return true
}

func (m *MockAIService) GetJournalistProfile(journalistType string) (*ai.JournalistProfile, error) {
	return nil, nil
}

// Add missing ProcessAndSaveSubmission method to implement AIProcessor interface
func (m *MockAIService) ProcessAndSaveSubmission(
	ctx context.Context,
	db *database.DB,
	submission database.Submission,
	authorName, authorDepartment, journalistType string,
	newsletterIssueID *int,
) error {
	if m.Error != nil {
		return m.Error
	}

	// Track the call
	call := ProcessAndSaveCall{
		Submission:        submission,
		AuthorName:        authorName,
		AuthorDepartment:  authorDepartment,
		JournalistType:    journalistType,
		NewsletterIssueID: newsletterIssueID,
	}
	m.ProcessAndSaveCalls = append(m.ProcessAndSaveCalls, call)

	// For basic mock, just return nil (success)
	// Individual tests can override this behavior if needed
	return nil
}

// Ensure MockAIService implements AIProcessor interface
var _ AIProcessor = (*MockAIService)(nil)

// TDD Phase 1: AUTO-ASSIGNMENT FAILING TESTS

// MockDatabase for testing newsletter issue auto-assignment
// This mock needs to be castable to *database.DB to fix type assertion issues
type MockDatabase struct {
	WeeklyIssues                map[string]*database.WeeklyNewsletterIssue // key: "week-year"
	ProcessedArticles           []database.ProcessedArticle
	GetOrCreateWeeklyIssueCalls []GetOrCreateWeeklyIssueCall
	UpdateProcessedArticleCalls []UpdateProcessedArticleCall

	// Add embedded anonymous field to make this castable
	*database.DB
}

type GetOrCreateWeeklyIssueCall struct {
	WeekNumber int
	Year       int
}

type UpdateProcessedArticleCall struct {
	Article database.ProcessedArticle
}

func NewMockDatabase() *MockDatabase {
	// Create a temporary DB for embedding (won't be used, just for type compatibility)
	tempDB, _ := database.NewSimple(":memory:")

	return &MockDatabase{
		WeeklyIssues:                make(map[string]*database.WeeklyNewsletterIssue),
		ProcessedArticles:           []database.ProcessedArticle{},
		GetOrCreateWeeklyIssueCalls: []GetOrCreateWeeklyIssueCall{},
		UpdateProcessedArticleCalls: []UpdateProcessedArticleCall{},
		DB:                          tempDB, // Embed for type compatibility
	}
}

func (m *MockDatabase) GetOrCreateWeeklyIssue(weekNumber, year int) (*database.WeeklyNewsletterIssue, error) {
	key := fmt.Sprintf("%d-%d", weekNumber, year)

	// Track the call
	m.GetOrCreateWeeklyIssueCalls = append(m.GetOrCreateWeeklyIssueCalls, GetOrCreateWeeklyIssueCall{
		WeekNumber: weekNumber,
		Year:       year,
	})

	// Return existing issue if found
	if issue, exists := m.WeeklyIssues[key]; exists {
		return issue, nil
	}

	// Create new issue
	issue := &database.WeeklyNewsletterIssue{
		ID:         len(m.WeeklyIssues) + 1,
		WeekNumber: weekNumber,
		Year:       year,
		Title:      fmt.Sprintf("Week %d, %d Newsletter", weekNumber, year),
		Status:     database.IssueStatusDraft,
	}
	m.WeeklyIssues[key] = issue

	return issue, nil
}

// Add missing CreateProcessedArticle method to implement DatabaseInterface
func (m *MockDatabase) CreateProcessedArticle(article database.ProcessedArticle) (int, error) {
	// Generate a new ID
	newID := len(m.ProcessedArticles) + 1
	article.ID = newID

	// Add to our mock storage
	m.ProcessedArticles = append(m.ProcessedArticles, article)

	return newID, nil
}

// GetUnderlyingDB returns the embedded *database.DB for testing
func (m *MockDatabase) GetUnderlyingDB() *database.DB {
	return m.DB
}

// Ensure MockDatabase implements DatabaseInterface
var _ DatabaseInterface = (*MockDatabase)(nil)

// TDD Test 1: processSubmissionAsync should auto-assign articles to current week's newsletter
func TestProcessSubmissionAsync_AutoAssignsToCurrentWeek(t *testing.T) {
	// This test should FAIL initially - processSubmissionAsync doesn't exist yet

	// Setup mocks
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}
	mockDB := NewMockDatabase()

	// Create submission
	submission := database.Submission{
		ID:      1,
		UserID:  "U12345",
		Content: "Our team launched a new feature this week!",
	}

	// Create a test slackBot with database access
	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		nil,                  // question selector
		[]string{"U1234567"}, // admin users
		mockSubmissionManager,
		mockAIService,
		mockDB,
	)

	// Call through the public interface by submitting a command
	command := SlashCommand{
		Command:     "/pp",
		Text:        "submit " + submission.Content,
		UserID:      submission.UserID,
		ResponseURL: "https://hooks.slack.com/test",
	}

	_, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Give async processing time to complete and retry if needed
	var getOrCreateCallCount int
	var processCallCount int
	for i := 0; i < 10; i++ { // Try up to 10 times with 200ms intervals
		time.Sleep(200 * time.Millisecond)
		getOrCreateCallCount = len(mockDB.GetOrCreateWeeklyIssueCalls)
		processCallCount = len(mockAIService.ProcessAndSaveCalls)
		if getOrCreateCallCount > 0 && processCallCount > 0 {
			break
		}
	}

	// Verify that GetOrCreateWeeklyIssue was called for current week
	if getOrCreateCallCount != 1 {
		t.Errorf("Expected GetOrCreateWeeklyIssue to be called once, got %d calls", getOrCreateCallCount)
	}

	// Verify AI processing was triggered
	if processCallCount != 1 {
		t.Errorf("Expected AI processing to be called once, got %d calls", processCallCount)
	}
}

// TDD Test 2: processSubmissionAsync should create newsletter issue if it doesn't exist
func TestProcessSubmissionAsync_CreatesWeeklyIssueIfNotExists(t *testing.T) {
	// This test should FAIL initially

	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}
	mockDB := NewMockDatabase()

	// Verify no issues exist initially
	if len(mockDB.WeeklyIssues) != 0 {
		t.Errorf("Expected no newsletter issues initially, got %d", len(mockDB.WeeklyIssues))
	}

	submission := database.Submission{
		ID:      1,
		UserID:  "U12345",
		Content: "Test submission content",
	}

	// Create bot - WILL FAIL as constructor doesn't exist
	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		nil,
		[]string{"U1234567"},
		mockSubmissionManager,
		mockAIService,
		mockDB,
	)

	// Process submission through public interface
	command := SlashCommand{
		Command:     "/pp",
		Text:        "submit " + submission.Content,
		UserID:      submission.UserID,
		ResponseURL: "",
	}

	_, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Give async processing time to complete and retry if needed
	var issueCount int
	for i := 0; i < 10; i++ { // Try up to 10 times with 200ms intervals
		time.Sleep(200 * time.Millisecond)
		issueCount = len(mockDB.WeeklyIssues)
		if issueCount > 0 {
			break
		}
	}

	// Verify a new newsletter issue was created
	if issueCount != 1 {
		t.Errorf("Expected 1 newsletter issue to be created, got %d", issueCount)
	}
}

// TDD Test 3: Integration test - Complete flow from submit command to newsletter display
func TestSlackBot_SubmitCommand_ArticleAppearsInNewsletter_Integration(t *testing.T) {
	// This test should FAIL initially - complete auto-assignment flow doesn't exist

	// Setup: Use real test database for integration testing
	tempDir := t.TempDir()
	dbPath := fmt.Sprintf("%s/test.db", tempDir)

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Create real dependencies
	questionSelector := database.NewQuestionSelector(db.DB)
	submissionManager := database.NewSubmissionManager(db.DB)
	mockAIService := &MockAIService{} // Use mock AI to avoid external API calls

	// Create bot with full dependencies including database - WILL FAIL as constructor doesn't exist
	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		questionSelector,
		[]string{"U1234567"},
		submissionManager,
		mockAIService,
		db, // Real database
	)

	// Simulate submission command
	command := SlashCommand{
		Command:     "/pp",
		Text:        "submit Our new analytics dashboard is live and helping teams make better decisions!",
		UserID:      "U987654321",
		ResponseURL: "",
	}

	// Handle the command - should trigger async processing
	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Verify immediate response
	if response == nil || response.Text == "" {
		t.Fatal("Expected immediate response from submission")
	}

	// TODO: Add synchronization mechanism for async processing in tests
	// For now, we'll check that the submission was created
	submissions, err := submissionManager.GetSubmissionsByUser(context.Background(), "U987654321")
	if err != nil {
		t.Fatalf("Failed to get submissions: %v", err)
	}

	if len(submissions) != 1 {
		t.Errorf("Expected 1 submission, got %d", len(submissions))
	}

	// After implementing async processing, this test should verify:
	// 1. ProcessedArticle exists with newsletter_issue_id set
	// 2. GetProcessedArticlesByNewsletterIssue returns the article
	// 3. Article appears in current week's newsletter
}

// TDD Phase 1: RED - Integration test for complete ProcessAndSaveSubmission flow
func TestSlackBot_ProcessAndSaveSubmission_Integration(t *testing.T) {
	// This test should FAIL initially - ProcessAndSaveSubmission architecture doesn't exist

	// Setup real test database
	tempDir := t.TempDir()
	dbPath := fmt.Sprintf("%s/test.db", tempDir)

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Create newsletter issue for testing
	issue, err := db.GetOrCreateWeeklyIssue(42, 2025)
	if err != nil {
		t.Fatalf("Failed to create newsletter issue: %v", err)
	}

	// Create mocks for testing
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIServiceWithSave{
		MockAIService: &MockAIService{},
		Database:      db,
	}

	// Create bot with database access
	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		nil,                  // question selector
		[]string{"U1234567"}, // admin users
		mockSubmissionManager,
		mockAIService,
		db,
	)

	// Submit article via Slack command
	command := SlashCommand{
		Command:     "/pp",
		Text:        "submit Our new analytics dashboard is transforming how teams make data-driven decisions!",
		UserID:      "U987654321",
		ResponseURL: "",
	}

	// Handle the submission command
	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response from command handler")
	}

	// Wait for async processing to complete
	time.Sleep(200 * time.Millisecond)

	// Verify that ProcessedArticle exists in database with newsletter_issue_id set
	articles, err := db.GetProcessedArticlesByNewsletterIssue(issue.ID)
	if err != nil {
		t.Fatalf("Failed to query processed articles: %v", err)
	}

	// This is the KEY TEST: Articles should now appear in newsletter query
	if len(articles) != 1 {
		t.Errorf("Expected 1 processed article in newsletter, got %d", len(articles))
		t.Log("This test will pass once ProcessAndSaveSubmission is implemented")
		return
	}

	// Verify article details
	article := articles[0]
	if article.NewsletterIssueID == nil {
		t.Error("Expected newsletter_issue_id to be set")
	} else if *article.NewsletterIssueID != issue.ID {
		t.Errorf("Expected newsletter_issue_id %d, got %d", issue.ID, *article.NewsletterIssueID)
	}

	if article.ProcessingStatus != database.ProcessingStatusSuccess {
		t.Errorf("Expected processing status 'success', got %s", article.ProcessingStatus)
	}

	t.Log("SUCCESS: Article automatically appears in newsletter after processing!")
}

// MockAIServiceWithSave extends MockAIService to support ProcessAndSaveSubmission
type MockAIServiceWithSave struct {
	*MockAIService
	Database *database.DB
}

// This method will FAIL to compile initially - that's the RED phase
func (m *MockAIServiceWithSave) ProcessAndSaveSubmission(
	ctx context.Context,
	db *database.DB,
	submission database.Submission,
	authorName, authorDepartment, journalistType string,
	newsletterIssueID *int,
) error {
	// Track the call
	call := ProcessAndSaveCall{
		Submission:        submission,
		AuthorName:        authorName,
		AuthorDepartment:  authorDepartment,
		JournalistType:    journalistType,
		NewsletterIssueID: newsletterIssueID,
	}
	m.MockAIService.ProcessAndSaveCalls = append(m.MockAIService.ProcessAndSaveCalls, call)

	// Simulate the complete flow: AI processing + database save
	processedArticle := database.ProcessedArticle{
		SubmissionID:      submission.ID,
		NewsletterIssueID: newsletterIssueID, // This is the key fix!
		JournalistType:    journalistType,
		ProcessedContent:  `{"headline": "Test Article", "content": "Test content", "byline": "Test Writer"}`,
		ProcessingStatus:  database.ProcessingStatusSuccess,
		WordCount:         25,
		TemplateFormat:    "hero",
	}

	// Save to database (this is what the real implementation should do)
	_, err := db.CreateProcessedArticle(processedArticle)
	return err
}
