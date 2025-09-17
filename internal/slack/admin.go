package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	case "assign-question":
		return ah.handleAssignQuestion(ctx, cmd.Args)
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
     • admin assign-question [feature|general|body_mind] [@user1 @user2] - Send questions to users
     • admin week-status - Current week dashboard with assignments and status
     • admin pool-status - Anonymous body/mind question pool levels and activity
     • admin broadcast-bodymind - Send wellness question request to all users

**Other:**
     • admin help - Show this help message

     Examples:
     > admin add-question "What did you accomplish this week?" work
     > admin list-questions fun
     > admin list-submissions
     > admin assign-question feature @john.doe
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

// handleAssignQuestion handles sending questions to users for current week assignments
func (ah *AdminHandler) handleAssignQuestion(ctx context.Context, args []string) (*SlashCommandResponse, error) {
	if ah.db == nil {
		return &SlashCommandResponse{
			Text:         "❌ Weekly automation is not available.",
			ResponseType: "ephemeral",
		}, nil
	}

	if len(args) < 2 {
		return &SlashCommandResponse{
			Text:         "Usage: admin assign-question [feature|general|body_mind] [@user1 @user2 ...]\nExample: admin assign-question feature @john.doe",
			ResponseType: "ephemeral",
		}, nil
	}

	contentType := args[0]
	users := args[1:]

	// Validate content type
	validContentTypes := map[string]database.ContentType{
		"feature":   database.ContentTypeFeature,
		"general":   database.ContentTypeGeneral,
		"body_mind": database.ContentTypeBodyMind,
	}

	dbContentType, valid := validContentTypes[contentType]
	if !valid {
		return &SlashCommandResponse{
			Text:         "❌ Content type must be 'feature', 'general', or 'body_mind'",
			ResponseType: "ephemeral",
		}, nil
	}

	// Get current week and create issue if needed
	now := time.Now()
	currentYear, currentWeek := now.ISOWeek()
	issue, err := ah.db.GetOrCreateWeeklyIssue(currentWeek, currentYear)
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to get weekly issue: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	var successfulAssignments []string
	var errors []string

	for _, userArg := range users {
		// Resolve user identifier (handles both user IDs and usernames)
		userID, err := ah.resolveUserIdentifier(ctx, userArg)
		if err != nil {
			errors = append(errors, fmt.Sprintf("User %s: %v", userArg, err))
			continue
		}

		// Select question based on content type
		var question *database.Question
		var questionText string

		if contentType == "body_mind" {
			// For body_mind, use anonymous question pool
			if ah.poolManager == nil {
				errors = append(errors, fmt.Sprintf("User %s: Body/mind pool not available", userID))
				continue
			}
			bodyMindQuestions, err := ah.db.GetActiveBodyMindQuestions()
			if err != nil || len(bodyMindQuestions) == 0 {
				errors = append(errors, fmt.Sprintf("User %s: No body/mind questions available", userID))
				continue
			}
			// Use first available question (could be improved with better selection)
			bodyMindQ := bodyMindQuestions[0]
			questionText = bodyMindQ.QuestionText
			// Mark as used
			if err := ah.db.MarkBodyMindQuestionUsed(bodyMindQ.ID); err != nil {
				errors = append(errors, fmt.Sprintf("User %s: Failed to mark question as used", userID))
				continue
			}
		} else {
			// For feature/general, use regular question rotation
			question, err = ah.questionSelector.SelectNextQuestion(ctx, contentType)
			if err != nil {
				errors = append(errors, fmt.Sprintf("User %s: Failed to select question: %v", userID, err))
				continue
			}
			questionText = question.Text

			// Mark question as used
			if err := ah.questionSelector.MarkQuestionUsed(ctx, question.ID); err != nil {
				errors = append(errors, fmt.Sprintf("User %s: Failed to mark question as used", userID))
				continue
			}
		}

		// Create assignment record
		assignment := database.PersonAssignment{
			IssueID:     issue.ID,
			PersonID:    userID,
			ContentType: dbContentType,
			AssignedAt:  now,
		}

		if question != nil {
			assignment.QuestionID = &question.ID
		}

		_, err = ah.db.CreatePersonAssignment(assignment)
		if err != nil {
			errors = append(errors, fmt.Sprintf("User %s: Failed to create assignment: %v", userID, err))
			continue
		}

		// Send direct message to user with question
		message := ah.createQuestionMessage(questionText, contentType, currentWeek, currentYear)
		var messageError error
		if ah.broadcastManager != nil {
			messageError = ah.sendDirectMessage(ctx, userID, message)
		}

		// Always mark as successful assignment if we got this far (database operations succeeded)
		successfulAssignments = append(successfulAssignments, userID)

		// But note message sending errors separately
		if messageError != nil {
			errors = append(errors, fmt.Sprintf("User %s: Assignment created but message failed: %v", userID, messageError))
		}
	}

	// Format response
	var responseText strings.Builder

	if len(successfulAssignments) > 0 {
		responseText.WriteString("✅ Successfully assigned questions:\n")
		for _, userID := range successfulAssignments {
			responseText.WriteString(fmt.Sprintf("• %s content → %s\n", contentType, userID))
		}
	}

	if len(errors) > 0 {
		if len(successfulAssignments) > 0 {
			responseText.WriteString("\n")
		}
		responseText.WriteString("❌ Errors:\n")
		for _, errMsg := range errors {
			responseText.WriteString(fmt.Sprintf("• %s\n", errMsg))
		}
	}

	if len(successfulAssignments) == 0 && len(errors) == 0 {
		responseText.WriteString("❌ No assignments were processed.")
	}

	return &SlashCommandResponse{
		Text:         responseText.String(),
		ResponseType: "ephemeral",
	}, nil
}

// createQuestionMessage creates the DM message with the question
func (ah *AdminHandler) createQuestionMessage(questionText, contentType string, week, year int) string {
	return fmt.Sprintf("📝 *Newsletter Assignment - Week %d, %d*\n\n"+
		"You've been assigned to write %s content for this week's newsletter.\n\n"+
		"*Your question:*\n> %s\n\n"+
		"Please submit your response using: `/pp submit \"your content here\"`\n\n"+
		"Need help? Contact an admin or check `/pp help` for more options.",
		week, year, contentType, questionText)
}

// sendDirectMessage sends a direct message to a user (wrapper for broadcast manager)
func (ah *AdminHandler) sendDirectMessage(ctx context.Context, userID, message string) error {
	if ah.broadcastManager == nil {
		return fmt.Errorf("broadcast manager not available")
	}
	// Use the broadcast manager's sendDirectMessage method (it's private but we can call it from same package)
	return ah.broadcastManager.sendDirectMessage(ctx, userID, message)
}

// resolveUserIdentifier converts a username or user identifier to a Slack user ID
func (ah *AdminHandler) resolveUserIdentifier(ctx context.Context, userArg string) (string, error) {
	// Strip @ prefix if present
	cleanInput := strings.TrimPrefix(userArg, "@")

	// If it's already a user ID (starts with "U" and has reasonable length), return it
	if strings.HasPrefix(cleanInput, "U") && len(cleanInput) > 5 {
		return cleanInput, nil
	}

	// Otherwise, try to look up the user by name
	if ah.broadcastManager == nil {
		return "", fmt.Errorf("cannot lookup user: broadcast manager not available")
	}

	userID, err := ah.broadcastManager.lookupUserByName(ctx, cleanInput)
	if err != nil {
		return "", fmt.Errorf("failed to find user '%s': %w", cleanInput, err)
	}

	return userID, nil
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
