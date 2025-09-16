package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TemplateService provides newsletter template rendering functionality
type TemplateService struct {
	templates *template.Template
	config    *TemplateConfig
}

// NewTemplateService creates a new template service with the given configuration
func NewTemplateService(config *TemplateConfig) (*TemplateService, error) {
	if config == nil {
		config = &TemplateConfig{
			CompanyName:    "Company Newsletter",
			NewsletterName: "Weekly Newsletter",
			Theme:          "classic",
			BaseURL:        "",
			StaticURL:      "/static",
		}
	}

	// Initialize template with helper functions first
	tmpl := template.New("newsletter").Funcs(getTemplateFunctions())

	// Parse all HTML templates from templates directory
	templatesPath := filepath.Join("templates", "*.html")
	var err error
	tmpl, err = tmpl.ParseGlob(templatesPath)
	if err != nil {
		// Try relative path for tests
		templatesPath = filepath.Join("../../templates", "*.html")
		tmpl, err = tmpl.ParseGlob(templatesPath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse templates: %w", err)
		}
	}

	return &TemplateService{
		templates: tmpl,
		config:    config,
	}, nil
}

// RenderNewsletter renders a complete newsletter page from weekly issue and articles
func (ts *TemplateService) RenderNewsletter(ctx context.Context, issue *database.WeeklyNewsletterIssue, articles []database.ProcessedArticle) (string, error) {
	// Transform articles to template data
	articleData, err := ts.prepareArticleData(articles)
	if err != nil {
		return "", fmt.Errorf("failed to prepare article data: %w", err)
	}

	// Create newsletter page data
	page := &NewsletterPage{
		Issue:        issue,
		Articles:     articleData,
		GeneratedAt:  time.Now(),
		PublishReady: ts.isPublishReady(issue),
	}

	// Render template
	var buf bytes.Buffer
	if err := ts.templates.ExecuteTemplate(&buf, "newsletter.html", page); err != nil {
		return "", fmt.Errorf("failed to execute newsletter template: %w", err)
	}

	return buf.String(), nil
}

// RenderArticle renders a single article with appropriate template
func (ts *TemplateService) RenderArticle(ctx context.Context, article database.ProcessedArticle) (string, error) {
	// Prepare article data
	articleData, err := ts.prepareArticleData([]database.ProcessedArticle{article})
	if err != nil {
		return "", fmt.Errorf("failed to prepare article data: %w", err)
	}

	if len(articleData) == 0 {
		return "", fmt.Errorf("no article data prepared")
	}

	data := articleData[0]

	// Choose template based on journalist type
	templateName := ts.getArticleTemplateName(article.JournalistType)

	var buf bytes.Buffer
	if err := ts.templates.ExecuteTemplate(&buf, templateName, data); err != nil {
		return "", fmt.Errorf("failed to execute article template %s: %w", templateName, err)
	}

	return buf.String(), nil
}

// prepareArticleData transforms processed articles to template-ready data
func (ts *TemplateService) prepareArticleData(articles []database.ProcessedArticle) ([]ArticleData, error) {
	var result []ArticleData

	for _, article := range articles {
		// Parse JSON content for template use
		var formattedContent interface{}
		if article.ProcessedContent != "" {
			if err := json.Unmarshal([]byte(article.ProcessedContent), &formattedContent); err != nil {
				return nil, fmt.Errorf("failed to parse article content JSON for article %d: %w", article.ID, err)
			}
		}

		// Create article data
		data := ArticleData{
			ProcessedArticle: &article,
			FormattedContent: formattedContent,
			CategoryName:     ts.formatCategoryName(article.JournalistType),
			PublishDate:      time.Now(), // TODO: Get actual publish date from issue
		}

		result = append(result, data)
	}

	return result, nil
}

// getArticleTemplateName maps journalist type to template name
func (ts *TemplateService) getArticleTemplateName(journalistType string) string {
	switch journalistType {
	case "feature":
		return "article-feature.html"
	case "interview":
		return "article-interview.html"
	case "sports":
		return "article-sports.html"
	case "general":
		return "article-general.html"
	case "body_mind":
		return "article-bodymind.html"
	default:
		return "article-general.html"
	}
}

// formatCategoryName converts journalist type to display name
func (ts *TemplateService) formatCategoryName(journalistType string) string {
	switch journalistType {
	case "feature":
		return "Feature Story"
	case "interview":
		return "Interview"
	case "sports":
		return "Sports"
	case "general":
		return "News"
	case "body_mind":
		return "Wellness"
	default:
		return "Article"
	}
}

// isPublishReady determines if a newsletter issue is ready for publication
func (ts *TemplateService) isPublishReady(issue *database.WeeklyNewsletterIssue) bool {
	if issue == nil {
		return false
	}

	// Check if it's past the publication time (Thursday 9:30 AM)
	now := time.Now()
	return now.After(issue.PublicationDate)
}

// getTemplateFunctions returns template helper functions
func getTemplateFunctions() template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("January 2, 2006")
		},
		"formatTime": func(t time.Time) string {
			return t.Format("3:04 PM")
		},
		"formatWeek": func(weekNum, year int) string {
			return fmt.Sprintf("Week %d, %d", weekNum, year)
		},
		"truncate": func(s string, length int) string {
			if len(s) <= length {
				return s
			}
			return s[:length] + "..."
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"title": strings.Title,
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("invalid dict call")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"wordCount": func(s string) int {
			if s == "" {
				return 0
			}
			return len(strings.Fields(s))
		},
	}
}
