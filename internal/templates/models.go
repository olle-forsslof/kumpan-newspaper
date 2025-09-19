package templates

import (
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// NewsletterPage represents the complete newsletter template data
type NewsletterPage struct {
	Issue        *database.WeeklyNewsletterIssue `json:"issue"`
	Articles     []ArticleData                   `json:"articles"`
	GeneratedAt  time.Time                       `json:"generated_at"`
	PublishReady bool                            `json:"publish_ready"`
}

// ArticleData represents processed article data for template rendering
type ArticleData struct {
	*database.ProcessedArticle
	FormattedContent interface{} `json:"formatted_content"` // Parsed JSON content for template use
	AuthorInfo       *AuthorInfo `json:"author_info,omitempty"`
	CategoryName     string      `json:"category_name"`
	PublishDate      time.Time   `json:"publish_date"`
}

// AuthorInfo represents author information for articles
type AuthorInfo struct {
	Name       string `json:"name"`
	Department string `json:"department,omitempty"`
	Email      string `json:"email,omitempty"`
}

// FeatureArticleContent represents structured content for feature articles
type FeatureArticleContent struct {
	Headline string `json:"headline"`
	Byline   string `json:"byline"`
	Lead     string `json:"lead"`
	Body     string `json:"body"`
}

// InterviewContent represents structured content for interview articles
type InterviewContent struct {
	Headline  string              `json:"headline"`
	Byline    string              `json:"byline"`
	Intro     string              `json:"intro"`
	Questions []InterviewQuestion `json:"questions"`
}

// InterviewQuestion represents a Q&A pair in interviews
type InterviewQuestion struct {
	Question string `json:"q"`
	Answer   string `json:"a"`
}

// GeneralContent represents structured content for general articles
type GeneralContent struct {
	Headline string `json:"headline"`
	Byline   string `json:"byline"`
	Content  string `json:"content"`
}

// BodyMindContent represents structured content for wellness articles
type BodyMindContent struct {
	Headline string `json:"headline"`
	Response string `json:"response"`
	Question string `json:"question"`
}

// TemplateConfig holds configuration for template rendering
type TemplateConfig struct {
	BaseURL        string `json:"base_url"`
	StaticURL      string `json:"static_url"`
	CompanyName    string `json:"company_name"`
	NewsletterName string `json:"newsletter_name"`
	Theme          string `json:"theme"` // "classic", "modern", etc.
}
