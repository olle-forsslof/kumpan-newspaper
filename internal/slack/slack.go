// Package slack provides functionality for interacting with the Slack API.
// It offers tools for creating and managing Slack bots, handling slash commands,
// and other Slack-related operations.
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/slack-go/slack"
)

type slackBot struct {
	client       *slack.Client
	config       SlackConfig
	adminHandler *AdminHandler
}

type QuestionSelector interface {
	SelectNextQuestion(ctx context.Context, category string) (*database.Question, error)
	MarkQuestionUsed(ctx context.Context, questionID int) error
	GetQuestionsByCategory(ctx context.Context, category string) ([]database.Question, error)
	AddQuestion(ctx context.Context, text, category string) (*database.Question, error)
}

func NewBot(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string) Bot {
	// Don't initialize the client immediately - only when needed
	return &slackBot{
		client:       nil, // Initialize as nil
		config:       cfg,
		adminHandler: NewAdminHandler(questionSelector, adminUsers),
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
	if strings.HasPrefix(cmd.Text, "report ") {
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

func (b *slackBot) handleNewsSubmission(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	// Extract the news content (everything after "submit ")
	newsContent := strings.TrimSpace(strings.TrimPrefix(cmd.Text, "report "))

	if newsContent == "" {
		return &SlashCommandResponse{
			Text:         "Please provide some content for your news submission.\n\nExample: `submit Our team launched a new feature this week!`",
			ResponseType: "ephemeral",
		}, nil
	}

	// TODO: Store the news submission in database
	// For now, just acknowledge the submission

	return &SlashCommandResponse{
		Text:         fmt.Sprintf("📰 *News submission received!*\n\n> %s\n\n✅ Your story has been submitted for the newsletter. Thanks for contributing!", newsContent),
		ResponseType: "ephemeral",
	}, nil
}
