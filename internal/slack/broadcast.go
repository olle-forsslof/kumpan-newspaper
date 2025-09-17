package slack

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

// BroadcastManager handles broadcasting messages to all workspace members
type BroadcastManager struct {
	client *slack.Client
}

// NewBroadcastManager creates a new broadcast manager
func NewBroadcastManager(token string) *BroadcastManager {
	return &BroadcastManager{
		client: slack.New(token),
	}
}

// BroadcastBodyMindRequest sends a wellness question request to all workspace members
func (bm *BroadcastManager) BroadcastBodyMindRequest(ctx context.Context) (*BroadcastResult, error) {
	// Get list of all users in the workspace
	users, err := bm.getAllWorkspaceUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace users: %w", err)
	}

	// Filter out bots and deleted users
	activeUsers := bm.filterActiveUsers(users)

	// Create the wellness question broadcast message
	message := bm.createWellnessBroadcastMessage()

	// Send direct message to each user
	var successCount int
	var failureCount int
	var errors []string

	for _, user := range activeUsers {
		err := bm.sendDirectMessage(ctx, user.ID, message)
		if err != nil {
			failureCount++
			errors = append(errors, fmt.Sprintf("Failed to send to %s (%s): %v", user.Name, user.ID, err))

			// Log error but continue with other users
			fmt.Printf("Warning: Failed to send wellness broadcast to user %s: %v\n", user.ID, err)
		} else {
			successCount++
		}
	}

	result := &BroadcastResult{
		TotalUsers:      len(activeUsers),
		SuccessfulSends: successCount,
		FailedSends:     failureCount,
		Errors:          errors,
	}

	if len(errors) > 0 {
		return result, fmt.Errorf("broadcast completed with %d failures", failureCount)
	}

	return result, nil
}

// getAllWorkspaceUsers retrieves all users from the workspace
func (bm *BroadcastManager) getAllWorkspaceUsers(ctx context.Context) ([]slack.User, error) {
	users, err := bm.client.GetUsersContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get users: %w", err)
	}

	return users, nil
}

// filterActiveUsers filters out bots, deleted users, and the current bot user
func (bm *BroadcastManager) filterActiveUsers(users []slack.User) []slack.User {
	var activeUsers []slack.User

	for _, user := range users {
		// Skip bots, deleted users, and Slack's default users
		if user.IsBot ||
			user.Deleted ||
			user.ID == "USLACKBOT" ||
			user.IsUltraRestricted ||
			user.IsRestricted {
			continue
		}

		// Only include regular users who can receive DMs
		activeUsers = append(activeUsers, user)
	}

	return activeUsers
}

// sendDirectMessage sends a direct message to a specific user
func (bm *BroadcastManager) sendDirectMessage(ctx context.Context, userID, message string) error {
	// Open IM channel with the user first
	params := &slack.OpenConversationParameters{
		Users: []string{userID},
	}
	channel, _, _, err := bm.client.OpenConversationContext(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to open IM channel with user %s: %w", userID, err)
	}

	// Send message to the IM channel
	_, _, err = bm.client.PostMessageContext(ctx, channel.ID,
		slack.MsgOptionText(message, false),
		slack.MsgOptionAsUser(true),
	)
	if err != nil {
		return fmt.Errorf("failed to send message to user %s: %w", userID, err)
	}

	return nil
}

// createWellnessBroadcastMessage creates the message content for wellness question requests
func (bm *BroadcastManager) createWellnessBroadcastMessage() string {
	return "💡 *Help us expand our wellness content pool!*\n\n" +
		"Hi! We're looking for anonymous wellness questions for our company newsletter. Your contributions help create valuable content for the whole team.\n\n" +
		"*What we're looking for:*\n" +
		"• General wellness and self-care questions\n" +
		"• Mental health and mindfulness topics\n" +
		"• Work-life balance tips and strategies\n\n" +
		"*How to contribute:*\n" +
		"Use the command: `/pp submit-wellness \"Your question here\" category`\n\n" +
		"*Categories to choose from:*\n" +
		"• `wellness` - General health, fitness, nutrition\n" +
		"• `mental_health` - Stress management, mindfulness, mental wellbeing\n" +
		"• `work_life_balance` - Time management, boundaries, remote work tips\n\n" +
		"*Examples:*\n" +
		"• `/pp submit-wellness \"How do you manage stress during busy periods?\" wellness`\n" +
		"• `/pp submit-wellness \"What's your favorite mindfulness practice?\" mental_health`\n" +
		"• `/pp submit-wellness \"How do you disconnect from work after hours?\" work_life_balance`\n\n" +
		"*Important:* All questions are stored anonymously - no attribution to contributors. This helps create a safe space for sharing wellness topics.\n\n" +
		"Thanks for helping make our newsletter more valuable for everyone! 🙏"
}

// BroadcastResult contains the results of a broadcast operation
type BroadcastResult struct {
	TotalUsers      int      `json:"total_users"`
	SuccessfulSends int      `json:"successful_sends"`
	FailedSends     int      `json:"failed_sends"`
	Errors          []string `json:"errors,omitempty"`
}

// GetSummary returns a human-readable summary of the broadcast results
func (br *BroadcastResult) GetSummary() string {
	if br.FailedSends == 0 {
		return fmt.Sprintf("✅ Successfully sent wellness question request to all %d workspace members", br.SuccessfulSends)
	}

	return fmt.Sprintf("⚠️ Sent to %d of %d users (%d failed)",
		br.SuccessfulSends, br.TotalUsers, br.FailedSends)
}

// GetDetailedReport returns a detailed report including any errors
func (br *BroadcastResult) GetDetailedReport() string {
	summary := br.GetSummary()

	if len(br.Errors) == 0 {
		return summary
	}

	report := fmt.Sprintf("%s\n\nErrors encountered:\n", summary)
	for i, err := range br.Errors {
		if i < 10 { // Limit to first 10 errors to avoid very long messages
			report += fmt.Sprintf("• %s\n", err)
		} else if i == 10 {
			report += fmt.Sprintf("• ... and %d more errors\n", len(br.Errors)-10)
			break
		}
	}

	return report
}
