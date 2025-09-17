package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// parseCategorizedSubmission parses a submission command with optional category
// Returns: category, content, valid
func parseCategorizedSubmission(input string) (string, string, bool) {
	// Remove "submit " prefix
	content := strings.TrimSpace(strings.TrimPrefix(input, "submit "))
	if content == "" {
		return "", "", false
	}

	// Valid categories for unified submission system
	validCategories := []string{"feature", "general", "interview", "body_mind"}

	// Check if first word is a valid category
	parts := strings.SplitN(content, " ", 2)

	if len(parts) >= 1 {
		category := parts[0]

		// Check if it's a valid category
		isValidCategory := false
		for _, validCat := range validCategories {
			if category == validCat {
				isValidCategory = true
				break
			}
		}

		if isValidCategory {
			if len(parts) < 2 {
				return "", "", false // Category specified but no content
			}
			actualContent := strings.TrimSpace(parts[1])
			if actualContent == "" {
				return "", "", false // Category specified but no content
			}
			return category, actualContent, true
		}

		// If first word looks like a category attempt but isn't valid, reject it
		// We detect this by checking if it ends with common category suffixes or contains underscores
		if strings.Contains(category, "_") || strings.HasSuffix(category, "category") {
			// Looks like a category attempt but not valid
			return "", "", false
		}
	}

	// No category specified or first word doesn't look like a category
	// Default to general for backward compatibility
	return "general", content, true
}

// handleCategorizedSubmission processes unified submissions with category routing
func (b *slackBot) handleCategorizedSubmission(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	category, content, valid := parseCategorizedSubmission(cmd.Text)
	if !valid {
		return &SlashCommandResponse{
			Text:         "Please provide content for your submission.\n\nExamples:\n• `submit feature My team built a new dashboard`\n• `submit general Found this great Go performance article`\n• `submit body_mind How do you manage stress during deployments?`",
			ResponseType: "ephemeral",
		}, nil
	}

	// Route based on category
	switch category {
	case "body_mind":
		return b.handleAnonymousBodyMindSubmission(ctx, content)
	default:
		return b.handleAssignmentLinkedSubmission(ctx, cmd.UserID, category, content, cmd.ResponseURL)
	}
}

// handleAnonymousBodyMindSubmission creates anonymous submissions for wellness content
func (b *slackBot) handleAnonymousBodyMindSubmission(ctx context.Context, content string) (*SlashCommandResponse, error) {
	if b.db == nil {
		return &SlashCommandResponse{
			Text:         "❌ Anonymous submissions not available (database not configured)",
			ResponseType: "ephemeral",
		}, nil
	}

	// Create anonymous submission (no UserID stored)
	submission, err := b.db.CreateAnonymousSubmission(content, "body_mind")
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to store anonymous submission: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	responseText := fmt.Sprintf("🧘 *Anonymous wellness submission received!*\n\n> %s\n\n✅ Your submission has been added to the body/mind pool anonymously.", content)

	// Process with AI if available
	if b.aiProcessor != nil {
		responseText += "\n🤖 Processing with our wellness journalist in the background..."

		// Launch async processing for anonymous submission
		go b.processAnonymousSubmissionAsync(context.Background(), *submission)
	}

	return &SlashCommandResponse{
		Text:         responseText,
		ResponseType: "ephemeral",
	}, nil
}

// handleAssignmentLinkedSubmission processes submissions that should link to user assignments
func (b *slackBot) handleAssignmentLinkedSubmission(ctx context.Context, userID, category, content, responseURL string) (*SlashCommandResponse, error) {
	if b.submissionManager == nil {
		return &SlashCommandResponse{
			Text:         "❌ Submission storage not available",
			ResponseType: "ephemeral",
		}, nil
	}

	// Create submission with user attribution
	submission, err := b.submissionManager.CreateNewsSubmission(ctx, userID, content)
	if err != nil {
		return &SlashCommandResponse{
			Text:         fmt.Sprintf("❌ Failed to store submission: %v", err),
			ResponseType: "ephemeral",
		}, nil
	}

	responseText := fmt.Sprintf("📰 *%s submission received!*\n\n> %s\n\n", strings.Title(category), content)

	// Try to link to active assignment if available
	if b.db != nil {
		// Convert category to ContentType
		contentType := categoryToContentType(category)
		if contentType != "" {
			assignment, err := b.db.GetActiveAssignmentByUser(userID, database.ContentType(contentType))
			if err == nil && assignment != nil {
				// Link submission to assignment
				linkErr := b.db.LinkSubmissionToAssignment(assignment.ID, submission.ID)
				if linkErr == nil {
					responseText += fmt.Sprintf("🎯 Linked to your %s assignment for this week!\n", category)
				}
			}
		}
	}

	// Launch async AI processing if available
	if b.aiProcessor != nil && submission != nil {
		responseText += "🤖 Processing with AI in the background...\n"
		go b.processSubmissionAsync(context.Background(), *submission, userID, responseURL)
	}

	responseText += "✅ Thanks for contributing!"

	return &SlashCommandResponse{
		Text:         responseText,
		ResponseType: "ephemeral",
	}, nil
}

// categoryToContentType converts submission category to database ContentType
func categoryToContentType(category string) string {
	switch category {
	case "feature":
		return "feature"
	case "general":
		return "general"
	case "interview":
		return "general" // Interview content uses general journalist for now
	case "body_mind":
		return "body_mind"
	default:
		return "general"
	}
}

// processAnonymousSubmissionAsync handles AI processing for anonymous body/mind submissions
func (b *slackBot) processAnonymousSubmissionAsync(ctx context.Context, submission database.Submission) {
	// Similar to regular async processing but for anonymous submissions
	// Get current newsletter issue
	if b.db == nil {
		return
	}

	// Get newsletter issue for auto-assignment
	var newsletterIssueID *int
	now := time.Now()
	year, week := now.ISOWeek()

	issue, err := b.db.GetOrCreateWeeklyIssue(week, year)
	if err == nil {
		newsletterIssueID = &issue.ID
	}

	// Process anonymously (no author info)
	dbPtr := b.db.GetUnderlyingDB()
	if dbPtr == nil {
		return
	}

	err = b.aiProcessor.ProcessAndSaveSubmission(
		ctx,
		dbPtr,
		submission,
		"Community Member", // Anonymous author name
		"Wellness",         // Anonymous department
		"body_mind",        // Journalist type
		newsletterIssueID,
	)

	// Log results but don't send user notifications (anonymous)
	if err != nil {
		slog.Error("Anonymous submission processing failed", "error", err, "submission_id", submission.ID)
	} else {
		slog.Info("Anonymous submission processed successfully", "submission_id", submission.ID)
	}
}
