package slack

import (
	"context"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

type SlackConfig struct {
	Token         string
	SigningSecret string
}

type SlashCommand struct {
	Token       string
	Command     string
	Text        string
	UserID      string
	ChannelID   string
	ResponseURL string
}

type SlashCommandResponse struct {
	Text         string `json:"text"`
	ResponseType string `json:"response_type,omitempty"`
}

type Bot interface {
	SendMessage(ctx context.Context, channelID, text string) error
	HandleSlashCommand(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error)
	HandleEventCallback(ctx context.Context, event SlackEvent) error
	GetUserInfo(ctx context.Context, userID string) (*UserInfo, error)
	EnrichSubmissionWithUserInfo(ctx context.Context, userID, content string) (*EnrichedSubmission, error)
}

type SlackEvent struct {
	Type    string `json:"type"`
	User    string `json:"user"`
	Text    string `json:"text"`
	Channel string `json:"channel"`
	BotID   string `json:"bot_id,omitempty"`
}

// UserInfo represents Slack user information
type UserInfo struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	RealName string      `json:"real_name"`
	Profile  UserProfile `json:"profile"`
}

// UserProfile represents Slack user profile information
type UserProfile struct {
	Email     string `json:"email"`
	Title     string `json:"title"`
	RealName  string `json:"real_name"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// EnrichedSubmission represents a submission with user information attached
type EnrichedSubmission struct {
	UserID           string `json:"user_id"`
	Content          string `json:"content"`
	AuthorName       string `json:"author_name"`
	AuthorEmail      string `json:"author_email"`
	AuthorDepartment string `json:"author_department"`
}

// AIProcessor defines interface for AI content processing
type AIProcessor interface {
	ProcessSubmissionWithUserInfo(ctx context.Context, submission database.Submission, authorName, authorDepartment, journalistType string) (*database.ProcessedArticle, error)
	ProcessAndSaveSubmission(ctx context.Context, db *database.DB, submission database.Submission, authorName, authorDepartment, journalistType string, newsletterIssueID *int) error
	GetAvailableJournalists() []string
	ValidateJournalistType(journalistType string) bool
}

// DatabaseInterface defines interface for database operations needed by SlackBot
type DatabaseInterface interface {
	GetOrCreateWeeklyIssue(weekNumber, year int) (*database.WeeklyNewsletterIssue, error)
	CreateProcessedArticle(article database.ProcessedArticle) (int, error)
}
