package slack

import (
	"context"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

type MockBot struct {
	messages      []string
	responses     map[string]*SlashCommandResponse
	lastChannelID string
	lastContext   context.Context

	SendMessageCalls       []SendMessageCall
	SendMessageReturnError error

	HandleSlashCommandCalls       []HandleSlashCommandCall
	HandleSlashCommandReturnError error
	HandleSlashCommandReturnValue *SlashCommandResponse

	HandleEventCallbackCalls       []HandleEventCallbackCall
	HandleEventCallbackReturnError error

	mockQuestionSelector *MockQuestionSelector
	mockAdminUsers       []string
}

type HandleEventCallbackCall struct {
	Context context.Context
	Event   SlackEvent
}

type SendMessageCall struct {
	Context   context.Context
	ChannelID string
	Text      string
}

type HandleSlashCommandCall struct {
	Context context.Context
	Command SlashCommand
}

// MockQuestionSelector implements database.QuestionSelector for testing
type MockQuestionSelector struct{}

func (m *MockQuestionSelector) SelectNextQuestion(ctx context.Context, category string) (*database.Question, error) {
	return &database.Question{
		ID:       1,
		Text:     "Test question",
		Category: category,
	}, nil
}

func (m *MockQuestionSelector) MarkQuestionUsed(ctx context.Context, questionID int) error {
	return nil
}

func (m *MockQuestionSelector) GetQuestionsByCategory(ctx context.Context, category string) ([]database.Question, error) {
	return []database.Question{{ID: 1, Text: "Test question", Category: category}}, nil
}

func (m *MockQuestionSelector) AddQuestion(ctx context.Context, text, category string) (*database.Question, error) {
	return &database.Question{ID: 1, Text: text, Category: category}, nil
}

func (m *MockQuestionSelector) GetQuestionByID(ctx context.Context, questionID int) (*database.Question, error) {
	return &database.Question{
		ID:       questionID,
		Text:     "Test question",
		Category: "general",
	}, nil
}

func (m *MockQuestionSelector) DeleteQuestion(ctx context.Context, questionID int) error {
	return nil
}

func NewMockBot() *MockBot {
	return &MockBot{
		responses:            make(map[string]*SlashCommandResponse),
		mockQuestionSelector: &MockQuestionSelector{},
		mockAdminUsers:       []string{"U1234567"},
	}
}

func (m *MockBot) SendMessage(ctx context.Context, channelID, text string) error {
	m.messages = append(m.messages, text)
	m.lastChannelID = channelID
	m.lastContext = ctx
	return nil
}

func (m *MockBot) HandleSlashCommand(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	if response, exists := m.responses[cmd.Command]; exists {
		return response, nil
	}
	return &SlashCommandResponse{Text: "Command handled"}, nil
}

func (m *MockBot) HandleEventCallback(ctx context.Context, event SlackEvent) error {
	if m.HandleEventCallbackCalls == nil {
		m.HandleEventCallbackCalls = []HandleEventCallbackCall{}
	}

	m.HandleEventCallbackCalls = append(m.HandleEventCallbackCalls, HandleEventCallbackCall{
		Context: ctx,
		Event:   event,
	})

	return m.HandleEventCallbackReturnError
}

func (m *MockBot) GetMessages() []string {
	return m.messages
}

func (m *MockBot) GetLastChannelID() string {
	return m.lastChannelID
}

func (m *MockBot) GetUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	// Return mock user info for testing
	return &UserInfo{
		ID:       userID,
		Name:     "testuser",
		RealName: "Test User",
		Profile: UserProfile{
			Email:     "testuser@company.com",
			Title:     "Software Developer",
			RealName:  "Test User",
			FirstName: "Test",
			LastName:  "User",
		},
	}, nil
}

func (m *MockBot) EnrichSubmissionWithUserInfo(ctx context.Context, userID, content string) (*EnrichedSubmission, error) {
	userInfo, err := m.GetUserInfo(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &EnrichedSubmission{
		UserID:           userID,
		Content:          content,
		AuthorName:       userInfo.RealName,
		AuthorEmail:      userInfo.Profile.Email,
		AuthorDepartment: userInfo.Profile.Title,
	}, nil
}
