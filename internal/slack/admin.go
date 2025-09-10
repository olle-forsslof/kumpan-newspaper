package slack

import (
	"context"
	"fmt"
	"strings"
)

type AdminHandler struct {
	questionSelector QuestionSelector
	authorizedUsers  []string // Slack user IDs who can run admin commands
}

type AdminCommand struct {
	Action string
	Args   []string
}

// NewAdminHandler creates a handler for admin commands
func NewAdminHandler(questionSelector QuestionSelector, authorizedUsers []string) *AdminHandler {
	return &AdminHandler{
		questionSelector: questionSelector,
		authorizedUsers:  authorizedUsers,
	}
}

// isAuthorized checks if a user can run admin commands
func (ah *AdminHandler) isAuthorized(userID string) bool {
	for _, id := range ah.authorizedUsers {
		if id == userID {
			return true
		}
	}
	return false
}

// Parse "admin add-question 'What did you do?' work" into structured command
func parseAdminCommand(text string) (*AdminCommand, error) {
	parts := strings.Fields(text)
	if len(parts) < 2 || parts[0] != "admin" {
		return nil, fmt.Errorf("invalid admin command format")
	}

	action := parts[1]

	// Special handling for add-question which needs to parse quoted text
	if action == "add-question" {
		return parseAddQuestionCommand(text)
	}

	// For other commands, use simple field splitting
	return &AdminCommand{
		Action: action,
		Args:   parts[2:],
	}, nil
}

func parseAddQuestionCommand(text string) (*AdminCommand, error) {
	// Expected format: admin add-question "quoted question text" category
	// Find the quoted text
	startQuote := strings.Index(text, "\"")
	if startQuote == -1 {
		return nil, fmt.Errorf("add-question requires quoted text: admin add-question \"Your question\" category")
	}

	endQuote := strings.Index(text[startQuote+1:], "\"")
	if endQuote == -1 {
		return nil, fmt.Errorf("unclosed quote in question text")
	}

	// Extract the question text (without quotes)
	questionText := text[startQuote+1 : startQuote+1+endQuote]

	// Get the category (everything after the closing quote, trimmed)
	afterQuote := strings.TrimSpace(text[startQuote+1+endQuote+1:])
	categoryParts := strings.Fields(afterQuote)

	if len(categoryParts) == 0 {
		return nil, fmt.Errorf("category required: admin add-question \"Your question\" category")
	}

	return &AdminCommand{
		Action: "add-question",
		Args:   []string{questionText, categoryParts[0]},
	}, nil
}

func (ah *AdminHandler) HandleAdminCommand(ctx context.Context, userID string, cmd *AdminCommand) (*SlashCommandResponse, error) {
	// Security check first
	if !ah.isAuthorized(userID) {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ You are not authorized to use admin commands. Your ID: %s", userID),
			ResponseType: "ephemeral",
		}, nil
	}

	switch cmd.Action {
	case "add-question":
		return ah.handleAddQuestion(ctx, cmd.Args)
	case "list-questions":
		return ah.handleListQuestions(ctx, cmd.Args)
	case "remove-question":
		return ah.handleRemoveQuestion(ctx, cmd.Args)
	case "test-rotation":
		return ah.handleTestRotation(ctx, cmd.Args)
	default:
		return ah.handleHelp()
	}
}

func (ah *AdminHandler) handleAddQuestion(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if len(args) < 2 {
		return &SlashCommandResponse{
			Text:         "Usage: admin add-question \"Your question text\" category",
			ResponseType: "ephemeral",
		}, nil
	}

	questionText := strings.Trim(args[0], "\"'")
	category := args[1]

	question, err := ah.questionSelector.AddQuestion(ctx, questionText, category)
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to add question: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	return &SlashCommandResponse{
		Text: fmt.Sprintf("✅ Added question #%d to category '%s':\n> %s",
			question.ID, question.Category, question.Text),
		ResponseType: "ephemeral",
	}, nil
}

func (ah *AdminHandler) handleRemoveQuestion(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if len(args) < 1 {
		return &SlashCommandResponse{
			Text:         "Usage: admin remove-question question_id",
			ResponseType: "ephemeral",
		}, nil
	}

	// TODO: implement actual question removal logic
	return &SlashCommandResponse{
		Text:         "Question removal not implemented yet",
		ResponseType: "ephemeral",
	}, nil
}

func (ah *AdminHandler) handleHelp() (*SlashCommandResponse, error) {
	help := `*Newsletter Admin Commands*:

     • admin add-question "Question text" category
     • admin list-questions category
     • admin test-rotation category
     • admin remove-question question_id
     • admin help - Show this help message

     Examples:
     > admin add-question "What did you accomplish this week?" work
     > admin list-questions fun
     > admin test-rotation personal`

	return &SlashCommandResponse{
		Text:         help,
		ResponseType: "ephemeral",
	}, nil
}

func (ah *AdminHandler) handleListQuestions(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if len(args) < 1 {
		return &SlashCommandResponse{
			Text:         "Usage: admin list-questions category",
			ResponseType: "ephemeral",
		}, nil
	}

	category := args[0]
	questions, err := ah.questionSelector.GetQuestionsByCategory(ctx, category)
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to get questions: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	if len(questions) == 0 {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("No questions found in category '%s'", category),
			ResponseType: "ephemeral",
		}, nil
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("📝 Questions in category '%s':\n\n", category))

	for _, q := range questions {
		usedStatus := "Never used"
		if q.LastUsedAt != nil {
			usedStatus = fmt.Sprintf("Last used: %s", q.LastUsedAt.Format("Jan 2, 2006"))
		}

		response.WriteString(fmt.Sprintf("#%d: %s\n   _%s_\n\n", q.ID, q.Text, usedStatus))
	}

	return &SlashCommandResponse{
		Text:         response.String(),
		ResponseType: "ephemeral",
	}, nil
}

func (ah *AdminHandler) handleTestRotation(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if len(args) < 1 {
		return &SlashCommandResponse{
			Text:         "Usage: admin test-rotation category",
			ResponseType: "ephemeral",
		}, nil
	}

	category := args[0]
	question, err := ah.questionSelector.SelectNextQuestion(ctx, category)
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to select question: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	usedStatus := "🆕 Never used"
	if question.LastUsedAt != nil {
		usedStatus = fmt.Sprintf("🔄 Last used: %s", question.LastUsedAt.Format("Jan 2, 2006"))
	}

	return &SlashCommandResponse{
		Text: fmt.Sprintf("🎯 Next question for '%s' category:\n\n> %s\n\n%s",
			category, question.Text, usedStatus),
		ResponseType: "ephemeral",
	}, nil
}
