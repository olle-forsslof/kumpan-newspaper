// Package slack provides functionality for interacting with the Slack API.
// It offers tools for creating and managing Slack bots, handling slash commands,
// and other Slack-related operations.
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/olle-forsslof/kumpan-newspaper/internal/ai"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/slack-go/slack"
)

type slackBot struct {
	client            *slack.Client
	config            SlackConfig
	adminHandler      *AdminHandler
	submissionManager SubmissionManager
	aiProcessor       AIProcessor
	questionSelector  QuestionSelector
}

type QuestionSelector interface {
	SelectNextQuestion(ctx context.Context, category string) (*database.Question, error)
	MarkQuestionUsed(ctx context.Context, questionID int) error
	GetQuestionsByCategory(ctx context.Context, category string) ([]database.Question, error)
	AddQuestion(ctx context.Context, text, category string) (*database.Question, error)
	GetQuestionByID(ctx context.Context, questionID int) (*database.Question, error)
}

type SubmissionManager interface {
	CreateNewsSubmission(ctx context.Context, userID, content string) (*database.Submission, error)
	GetSubmissionsByUser(ctx context.Context, userID string) ([]database.Submission, error)
	GetAllSubmissions(ctx context.Context) ([]database.Submission, error)
}

func NewBot(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string) Bot {
	// Don't initialize the client immediately - only when needed
	return &slackBot{
		client:            nil, // Initialize as nil
		config:            cfg,
		adminHandler:      NewAdminHandler(questionSelector, adminUsers),
		submissionManager: nil, // No submission manager for basic bot
		aiProcessor:       nil, // No AI processor for basic bot
		questionSelector:  questionSelector,
	}
}

// NewBotWithSubmissions creates a bot with news submission storage capabilities
func NewBotWithSubmissions(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      NewAdminHandler(questionSelector, adminUsers),
		submissionManager: submissionManager,
		aiProcessor:       nil,
		questionSelector:  questionSelector,
	}
}

// NewBotWithAIProcessing creates a bot with automatic AI processing capabilities
func NewBotWithAIProcessing(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager, aiProcessor AIProcessor) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      NewAdminHandler(questionSelector, adminUsers),
		submissionManager: submissionManager,
		aiProcessor:       aiProcessor,
		questionSelector:  questionSelector,
	}
}

// NewBotWithWeeklyAutomation creates a bot with full weekly automation capabilities
func NewBotWithWeeklyAutomation(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager, aiProcessor AIProcessor, db *database.DB) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      NewAdminHandlerWithWeeklyAutomation(questionSelector, adminUsers, submissionManager, db, cfg.Token),
		submissionManager: submissionManager,
		aiProcessor:       aiProcessor,
		questionSelector:  questionSelector,
	}
}

func (b *slackBot) SendMessage(ctx context.Context, channelID, text string) error {
	// Initialize client only when actually needed
	if b.client == nil {
		b.client = slack.New(b.config.Token)
	}

	_, _, err := b.client.PostMessageContext(ctx, channelID,
		slack.MsgOptionText(text, false))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

func (b *slackBot) HandleSlashCommand(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	// Handle empty commands or help requests
	if cmd.Text == "" || cmd.Text == "help" {
		return b.handleRegularHelp(), nil
	}

	// Handle admin commands
	if strings.HasPrefix(cmd.Text, "admin ") {
		adminCmd, err := parseAdminCommand(cmd.Text)
		if err != nil {
			return &SlashCommandResponse{
				Text:         "Invalid admin command format. Type 'admin help' for admin usage or just 'help' for regular commands.",
				ResponseType: "ephemeral",
			}, nil
		}

		return b.adminHandler.HandleAdminCommand(ctx, cmd.UserID, adminCmd)
	}

	// Handle news story submissions for regular users
	if strings.HasPrefix(cmd.Text, "submit ") {
		return b.handleNewsSubmission(ctx, cmd)
	}

	// Handle regular newsletter functionality
	return &SlashCommandResponse{
		Text:         fmt.Sprintf("I received: '%s'\n\nFor help with commands, type `help`\nFor admin commands, type `admin help`", cmd.Text),
		ResponseType: "ephemeral",
	}, nil
}

func (b *slackBot) HandleEventCallback(ctx context.Context, event SlackEvent) error {
	// skip messages from bots to avoid infinite loops
	if event.BotID != "" {
		return nil
	}

	// for now echo the message
	if event.Type == "message" && event.Text != "" {
		return b.SendMessage(ctx, event.Channel, fmt.Sprintf("You said %s", event.Text))
	}

	return nil
}

func (b *slackBot) handleRegularHelp() *SlashCommandResponse {
	help := "*Newsletter Bot Help*\n\n" +
		"This bot helps manage daily/weekly newsletter questions and collect news stories.\n\n" +
		"*Available Commands:*\n" +
		"• `help` - Show this help message\n" +
		"• `submit [your news story]` - Submit a news story for the newsletter\n" +
		"• `admin help` - Show admin commands (authorized users only)\n\n" +
		"*Examples:*\n" +
		"• `submit Check out this cool new Go library: https://github.com/example/repo`\n" +
		"• `submit Our team shipped the new user dashboard this week!`\n\n" +
		"*For Admins:*\n" +
		"Admin users can manage newsletter questions, view submissions, and configure the bot."

	return &SlashCommandResponse{
		Text:         help,
		ResponseType: "ephemeral",
	}
}

func (b *slackBot) GetUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	// Initialize client only when actually needed
	if b.client == nil {
		b.client = slack.New(b.config.Token)
	}

	user, err := b.client.GetUserInfoContext(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	return &UserInfo{
		ID:       user.ID,
		Name:     user.Name,
		RealName: user.RealName,
		Profile: UserProfile{
			Email:     user.Profile.Email,
			Title:     user.Profile.Title,
			RealName:  user.Profile.RealName,
			FirstName: user.Profile.FirstName,
			LastName:  user.Profile.LastName,
		},
	}, nil
}

func (b *slackBot) EnrichSubmissionWithUserInfo(ctx context.Context, userID, content string) (*EnrichedSubmission, error) {
	userInfo, err := b.GetUserInfo(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Use title as department, fallback to email domain if title is empty
	department := userInfo.Profile.Title
	if department == "" && userInfo.Profile.Email != "" {
		// Extract domain from email as fallback department info
		if atIndex := strings.Index(userInfo.Profile.Email, "@"); atIndex > 0 {
			department = strings.Split(userInfo.Profile.Email[atIndex+1:], ".")[0]
		}
	}

	return &EnrichedSubmission{
		UserID:           userID,
		Content:          content,
		AuthorName:       userInfo.RealName,
		AuthorEmail:      userInfo.Profile.Email,
		AuthorDepartment: department,
	}, nil
}

func (b *slackBot) handleNewsSubmission(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	// Extract the news content (everything after "submit ")
	newsContent := strings.TrimSpace(strings.TrimPrefix(cmd.Text, "submit "))

	if newsContent == "" {
		return &SlashCommandResponse{
			Text:         "Please provide some content for your news submission.\n\nExample: `submit Our team launched a new feature this week!`",
			ResponseType: "ephemeral",
		}, nil
	}

	var responseText string
	var submission *database.Submission

	// Store the news submission in database if SubmissionManager is available
	if b.submissionManager != nil {
		var err error
		submission, err = b.submissionManager.CreateNewsSubmission(ctx, cmd.UserID, newsContent)
		if err != nil {
			return &SlashCommandResponse{
				Text:         fmt.Sprintf("❌ Failed to store your submission: %v", err),
				ResponseType: "ephemeral",
			}, nil
		}
		responseText = fmt.Sprintf("📰 *News submission received!*\n\n> %s\n\n", newsContent)
	} else {
		responseText = fmt.Sprintf("📰 *News submission received!*\n\n> %s\n\n", newsContent)
	}

	// Automatically process with AI if AIProcessor is available
	if b.aiProcessor != nil && submission != nil {
		// Get user information for enriched processing
		enrichedSubmission, err := b.EnrichSubmissionWithUserInfo(ctx, cmd.UserID, newsContent)

		// Use fallback user info if enrichment fails
		authorName := "Team Member"
		authorDepartment := "Unknown"

		if err != nil {
			// Log error but continue with fallback values
			responseText += fmt.Sprintf("⚠️ Note: Using fallback user info (couldn't fetch from Slack): %v\n", err)
		} else {
			authorName = enrichedSubmission.AuthorName
			authorDepartment = enrichedSubmission.AuthorDepartment
		}

		// Determine journalist type from question category
		journalistType := b.determineJournalistTypeFromSubmission(ctx, submission)

		// Process with AI
		processedArticle, err := b.aiProcessor.ProcessSubmissionWithUserInfo(
			ctx,
			*submission,
			authorName,
			authorDepartment,
			journalistType,
		)

		if err != nil {
			// Log error but don't fail the submission - processing can be retried later
			responseText += fmt.Sprintf("⚠️ Note: Auto-processing failed (AI): %v\n", err)
		} else {
			responseText += fmt.Sprintf("🤖 Auto-processed by %s journalist (%d words)\n", journalistType, processedArticle.WordCount)
		}
	}

	responseText += "✅ Thanks for contributing!"

	return &SlashCommandResponse{
		Text:         responseText,
		ResponseType: "ephemeral",
	}, nil
}

// determineJournalistTypeFromSubmission determines journalist type based on question category
func (b *slackBot) determineJournalistTypeFromSubmission(ctx context.Context, submission *database.Submission) string {
	// If no question ID, this is a general news submission
	if submission.QuestionID == nil {
		return "general"
	}

	// Get the question to find its category
	if b.questionSelector != nil {
		question, err := b.questionSelector.GetQuestionByID(ctx, *submission.QuestionID)
		if err != nil {
			// If we can't get the question, default to general
			return "general"
		}

		// Map question category to journalist type using existing mapping
		return ai.GetJournalistTypeForCategory(question.Category)
	}

	// Fallback to general if no question selector available
	return "general"
}

// determineJournalistType is deprecated - use determineJournalistTypeFromSubmission instead
