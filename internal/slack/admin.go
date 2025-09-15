package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

type AdminHandler struct {
	questionSelector  QuestionSelector
	authorizedUsers   []string // Slack user IDs who can run admin commands
	submissionManager SubmissionManager
	db                *database.DB                  // Add database access for weekly automation
	poolManager       *database.BodyMindPoolManager // Body/mind question pool
	broadcastManager  *BroadcastManager             // Broadcast messaging system
}

type AdminCommand struct {
	Action string
	Args   []string
}

// NewAdminHandler creates a handler for admin commands
func NewAdminHandler(questionSelector QuestionSelector, authorizedUsers []string) *AdminHandler {
	return &AdminHandler{
		questionSelector:  questionSelector,
		authorizedUsers:   authorizedUsers,
		submissionManager: nil, // No submission management for basic handler
	}
}

// NewAdminHandlerWithSubmissions creates a handler with submission management capabilities
func NewAdminHandlerWithSubmissions(questionSelector QuestionSelector, authorizedUsers []string, submissionManager SubmissionManager) *AdminHandler {
	return &AdminHandler{
		questionSelector:  questionSelector,
		authorizedUsers:   authorizedUsers,
		submissionManager: submissionManager,
		db:                nil,
		poolManager:       nil,
		broadcastManager:  nil,
	}
}

// NewAdminHandlerWithWeeklyAutomation creates a handler with full weekly automation capabilities
func NewAdminHandlerWithWeeklyAutomation(questionSelector QuestionSelector, authorizedUsers []string, submissionManager SubmissionManager, db *database.DB, slackToken string) *AdminHandler {
	poolManager := database.NewBodyMindPoolManager(db)
	broadcastManager := NewBroadcastManager(slackToken)
	return &AdminHandler{
		questionSelector:  questionSelector,
		authorizedUsers:   authorizedUsers,
		submissionManager: submissionManager,
		db:                db,
		poolManager:       poolManager,
		broadcastManager:  broadcastManager,
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
	case "list-submissions":
		return ah.handleListSubmissions(ctx, cmd.Args)

	// Weekly automation commands
	case "assign-week":
		return ah.handleAssignWeek(ctx, cmd.Args)
	case "week-status":
		return ah.handleWeekStatus(ctx, cmd.Args)
	case "pool-status":
		return ah.handlePoolStatus(ctx, cmd.Args)
	case "broadcast-bodymind":
		return ah.handleBroadcastBodyMind(ctx, cmd.Args)

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

**Question Management:**
     • admin add-question "Question text" category
     • admin list-questions category
     • admin test-rotation category
     • admin remove-question question_id

**Submission Management:**
     • admin list-submissions - Show all news submissions
     • admin list-submissions [user_id] - Show submissions by specific user

**Weekly Automation:**
     • admin assign-week [feature|general] [@user1 @user2] - Manual assignment override
     • admin week-status - Current week dashboard with assignments and status
     • admin pool-status - Anonymous body/mind question pool levels and activity
     • admin broadcast-bodymind - Send wellness question request to all users

**Other:**
     • admin help - Show this help message

     Examples:
     > admin add-question "What did you accomplish this week?" work
     > admin list-questions fun
     > admin list-submissions
     > admin assign-week feature @john.doe
     > admin week-status
     > admin pool-status`

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

// handleListSubmissions handles listing news submissions
func (ah *AdminHandler) handleListSubmissions(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if ah.submissionManager == nil {
		return &SlashCommandResponse{
			Text:         "❌ Submission management is not available.",
			ResponseType: "ephemeral",
		}, nil
	}

	var submissions []database.Submission
	var err error

	// Check if filtering by specific user
	if len(args) > 0 {
		userID := args[0]
		submissions, err = ah.submissionManager.GetSubmissionsByUser(ctx, userID)
		if err != nil {
			return &SlashCommandResponse{
				Text:         fmt.Sprintf("❌ Failed to get submissions for user %s: %v", userID, err),
				ResponseType: "ephemeral",
			}, nil
		}
	} else {
		// Get all submissions
		submissions, err = ah.submissionManager.GetAllSubmissions(ctx)
		if err != nil {
			return &SlashCommandResponse{
				Text:         fmt.Sprintf("❌ Failed to get submissions: %v", err),
				ResponseType: "ephemeral",
			}, nil
		}
	}

	if len(submissions) == 0 {
		return &SlashCommandResponse{
			Text:         "📰 No news submissions found.",
			ResponseType: "ephemeral",
		}, nil
	}

	// Format the response
	var response strings.Builder
	if len(args) > 0 {
		response.WriteString(fmt.Sprintf("📰 News submissions for user %s:\n\n", args[0]))
	} else {
		response.WriteString(fmt.Sprintf("📰 All news submissions (%d total):\n\n", len(submissions)))
	}

	for i, submission := range submissions {
		response.WriteString(fmt.Sprintf("**#%d** (ID: %d)\n", i+1, submission.ID))
		response.WriteString(fmt.Sprintf("👤 User: %s\n", submission.UserID))
		response.WriteString(fmt.Sprintf("📅 Submitted: %s\n", submission.CreatedAt.Format("Jan 2, 2006 15:04")))
		response.WriteString(fmt.Sprintf("📝 Content: %s\n\n", submission.Content))

		// Add separator for readability (except for last item)
		if i < len(submissions)-1 {
			response.WriteString("---\n\n")
		}
	}

	return &SlashCommandResponse{
		Text:         response.String(),
		ResponseType: "ephemeral",
	}, nil
}

// Weekly automation command handlers

// handleAssignWeek handles manual assignment overrides for current week
func (ah *AdminHandler) handleAssignWeek(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if ah.db == nil {
		return &SlashCommandResponse{
			Text:         "❌ Weekly automation is not available.",
			ResponseType: "ephemeral",
		}, nil
	}

	if len(args) < 2 {
		return &SlashCommandResponse{
			Text:         "Usage: admin assign-week [feature|general] [@user1 @user2 ...]\nExample: admin assign-week feature @john.doe",
			ResponseType: "ephemeral",
		}, nil
	}

	contentType := args[0]
	users := args[1:]

	// Validate content type
	validContentTypes := map[string]bool{
		"feature": true,
		"general": true,
	}

	if !validContentTypes[contentType] {
		return &SlashCommandResponse{
			Text:         "❌ Content type must be 'feature' or 'general'",
			ResponseType: "ephemeral",
		}, nil
	}

	// TODO: Implement the actual assignment logic
	return &SlashCommandResponse{
		Text: fmt.Sprintf("🚧 Week assignment not fully implemented yet.\n"+
			"Would assign %s content to: %s", contentType, strings.Join(users, ", ")),
		ResponseType: "ephemeral",
	}, nil
}

// handleWeekStatus shows current week dashboard with assignments and submission status
func (ah *AdminHandler) handleWeekStatus(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if ah.db == nil {
		return &SlashCommandResponse{
			Text:         "❌ Weekly automation is not available.",
			ResponseType: "ephemeral",
		}, nil
	}

	// TODO: Implement the actual week status logic
	return &SlashCommandResponse{
		Text: "🚧 Week status not fully implemented yet.\n" +
			"Would show current week assignments and submission status.",
		ResponseType: "ephemeral",
	}, nil
}

// handlePoolStatus shows anonymous body/mind question pool levels and activity metrics
func (ah *AdminHandler) handlePoolStatus(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if ah.poolManager == nil {
		return &SlashCommandResponse{
			Text:         "❌ Body/mind pool management is not available.",
			ResponseType: "ephemeral",
		}, nil
	}

	status, err := ah.poolManager.GetPoolStatus()
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to get pool status: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	slackMessage := ah.poolManager.FormatPoolStatusForSlack(status)

	return &SlashCommandResponse{
		Text:         slackMessage,
		ResponseType: "ephemeral",
	}, nil
}

// handleBroadcastBodyMind sends anonymous wellness question request to all users
func (ah *AdminHandler) handleBroadcastBodyMind(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if ah.broadcastManager == nil {
		return &SlashCommandResponse{
			Text:         "❌ Broadcast messaging is not available.",
			ResponseType: "ephemeral",
		}, nil
	}

	// Send the broadcast
	result, err := ah.broadcastManager.BroadcastBodyMindRequest(ctx)
	if err != nil {
		// Even if some sends failed, we still want to report what happened
		if result != nil {
			return &SlashCommandResponse{
				Text: fmt.Sprintf("⚠️ *Body/Mind Question Broadcast - Partial Success*\n\n%s\n\nError: %v",
					result.GetDetailedReport(), err),
				ResponseType: "ephemeral",
			}, nil
		}

		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to send broadcast: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	// Success - return summary
	return &SlashCommandResponse{
		Text:         fmt.Sprintf("✅ *Body/Mind Question Broadcast Complete*\n\n%s", result.GetSummary()),
		ResponseType: "ephemeral",
	}, nil
}
