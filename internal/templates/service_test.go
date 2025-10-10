package templates

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

func TestTemplateService_RenderNewsletter(t *testing.T) {
	service, err := NewTemplateService(nil)
	if err != nil {
		t.Fatalf("Failed to create template service: %v", err)
	}

	// Create test newsletter issue
	issue := &database.WeeklyNewsletterIssue{
		ID:              1,
		WeekNumber:      37,
		Year:            2025,
		Title:           "Weekly Newsletter - Week 37",
		Status:          database.IssueStatusReady,
		PublicationDate: time.Now(),
		CreatedAt:       time.Now(),
	}

	// Create test articles with different journalist types
	articles := []database.ProcessedArticle{
		{
			ID:               1,
			SubmissionID:     1,
			JournalistType:   "feature",
			ProcessedContent: `{"headline":"Major Project Launch","byline":"By Engineering Team","lead":"We are excited to announce the launch of our new platform.","body":"<p>This revolutionary platform will change how we work...</p>"}`,
			TemplateFormat:   "hero",
			ProcessingStatus: database.ProcessingStatusSuccess,
			WordCount:        150,
			CreatedAt:        time.Now(),
		},
		{
			ID:               2,
			SubmissionID:     2,
			JournalistType:   "interview",
			ProcessedContent: `{"headline":"Meet Our New CTO","byline":"By HR Team","intro":"We sat down with our new CTO to learn about their vision.","questions":[{"q":"What's your background?","a":"I've been in tech for 15 years..."},{"q":"What are your plans?","a":"We'll focus on innovation..."}]}`,
			TemplateFormat:   "interview",
			ProcessingStatus: database.ProcessingStatusSuccess,
			WordCount:        200,
			CreatedAt:        time.Now(),
		},
		{
			ID:               3,
			SubmissionID:     3,
			JournalistType:   "body_mind",
			ProcessedContent: `{"headline":"Workplace Wellness Tips","byline":"By Wellness Team","question":"How do you stay focused during busy periods?","tips":["Take regular breaks","Practice deep breathing","Stay hydrated"],"takeaway":"Small habits make a big difference in maintaining focus and energy."}`,
			TemplateFormat:   "advice",
			ProcessingStatus: database.ProcessingStatusSuccess,
			WordCount:        120,
			CreatedAt:        time.Now(),
		},
	}

	// Render the newsletter
	html, err := service.RenderNewsletter(context.Background(), issue, articles)
	if err != nil {
		t.Fatalf("Failed to render newsletter: %v", err)
	}

	// Verify the rendered HTML contains expected elements
	if !strings.Contains(html, "Weekly Newsletter - Week 37") {
		t.Error("Newsletter title not found in rendered HTML")
	}

	if !strings.Contains(html, "Week 37, 2025") {
		t.Error("Week/year not found in rendered HTML")
	}

	if !strings.Contains(html, "Major Project Launch") {
		t.Error("Feature article headline not found")
	}

	if !strings.Contains(html, "Meet Our New CTO") {
		t.Error("Interview article headline not found")
	}

	if !strings.Contains(html, "Workplace Wellness Tips") {
		t.Error("Body/mind article headline not found")
	}

	if !strings.Contains(html, "feature-article") {
		t.Error("Feature article template not used")
	}

	if !strings.Contains(html, "interview-article") {
		t.Error("Interview article template not used")
	}

	if !strings.Contains(html, "bodymind-article") {
		t.Error("Body/mind article template not used")
	}

	// Verify CSS link is present
	if !strings.Contains(html, "/static/css/newsletter.css") {
		t.Error("CSS stylesheet link not found")
	}

	// Verify responsive meta tag
	if !strings.Contains(html, `viewport`) {
		t.Error("Responsive viewport meta tag not found")
	}
}

func TestTemplateService_RenderArticle(t *testing.T) {
	service, err := NewTemplateService(nil)
	if err != nil {
		t.Fatalf("Failed to create template service: %v", err)
	}

	tests := []struct {
		name         string
		article      database.ProcessedArticle
		expectedText string
		templateName string
	}{
		{
			name: "Feature Article",
			article: database.ProcessedArticle{
				ID:               1,
				JournalistType:   "feature",
				ProcessedContent: `{"headline":"Breaking News","byline":"By News Team","lead":"Important announcement","body":"<p>Details here...</p>"}`,
				TemplateFormat:   "hero",
				WordCount:        100,
			},
			expectedText: "Breaking News",
			templateName: "feature-article",
		},
		{
			name: "Interview Article",
			article: database.ProcessedArticle{
				ID:               2,
				JournalistType:   "interview",
				ProcessedContent: `{"headline":"Q&A Session","byline":"By Interview Team","intro":"Great conversation","questions":[{"q":"Question 1?","a":"Answer 1"}]}`,
				TemplateFormat:   "interview",
				WordCount:        80,
			},
			expectedText: "Q&amp;A Session", // HTML-encoded version
			templateName: "interview-article",
		},
		{
			name: "Body/Mind Article",
			article: database.ProcessedArticle{
				ID:               3,
				JournalistType:   "body_mind",
				ProcessedContent: `{"headline":"Wellness Tips","byline":"By Wellness Team","question":"How to relax?","tips":["Breathe","Meditate"],"takeaway":"Stay calm"}`,
				TemplateFormat:   "advice",
				WordCount:        60,
			},
			expectedText: "Wellness Tips",
			templateName: "bodymind-article",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := service.RenderArticle(context.Background(), tt.article)
			if err != nil {
				t.Fatalf("Failed to render article: %v", err)
			}

			if !strings.Contains(html, tt.expectedText) {
				t.Errorf("Expected text '%s' not found in rendered HTML", tt.expectedText)
			}

			if !strings.Contains(html, tt.templateName) {
				t.Errorf("Expected template class '%s' not found", tt.templateName)
			}
		})
	}
}

func TestTemplateService_EmptyNewsletter(t *testing.T) {
	service, err := NewTemplateService(nil)
	if err != nil {
		t.Fatalf("Failed to create template service: %v", err)
	}

	issue := &database.WeeklyNewsletterIssue{
		ID:              1,
		WeekNumber:      38,
		Year:            2025,
		Title:           "Empty Newsletter",
		Status:          database.IssueStatusDraft,
		PublicationDate: time.Now().Add(24 * time.Hour), // Future date
		CreatedAt:       time.Now(),
	}

	// Render newsletter with no articles
	html, err := service.RenderNewsletter(context.Background(), issue, []database.ProcessedArticle{})
	if err != nil {
		t.Fatalf("Failed to render empty newsletter: %v", err)
	}

	// Should show no-content message
	if !strings.Contains(html, "No articles available") {
		t.Error("Empty newsletter should show 'No articles available' message")
	}

	// Should show draft notice since publication date is in the future
	if !strings.Contains(html, "Draft Version") {
		t.Error("Draft newsletter should show draft notice")
	}
}

func TestTemplateService_InvalidJSON(t *testing.T) {
	service, err := NewTemplateService(nil)
	if err != nil {
		t.Fatalf("Failed to create template service: %v", err)
	}

	issue := &database.WeeklyNewsletterIssue{
		ID:         1,
		WeekNumber: 39,
		Year:       2025,
		Title:      "Test Newsletter",
	}

	// Article with invalid JSON content
	articles := []database.ProcessedArticle{
		{
			ID:               1,
			JournalistType:   "general",
			ProcessedContent: `{"invalid": json}`, // Invalid JSON
			TemplateFormat:   "column",
		},
	}

	// Should return error for invalid JSON
	_, err = service.RenderNewsletter(context.Background(), issue, articles)
	if err == nil {
		t.Error("Expected error for invalid JSON content")
	}

	if !strings.Contains(err.Error(), "failed to parse article content JSON") {
		t.Errorf("Expected JSON parsing error, got: %v", err)
	}
}

func TestTemplateService_HelperFunctions(t *testing.T) {
	service, err := NewTemplateService(nil)
	if err != nil {
		t.Fatalf("Failed to create template service: %v", err)
	}

	// Test category name formatting
	tests := []struct {
		journalistType string
		expected       string
	}{
		{"feature", "Feature Story"},
		{"interview", "Interview"},
		{"sports", "Sports"},
		{"general", "News"},
		{"body_mind", "Wellness"},
		{"unknown", "Article"},
	}

	for _, tt := range tests {
		t.Run(tt.journalistType, func(t *testing.T) {
			result := service.formatCategoryName(tt.journalistType)
			if result != tt.expected {
				t.Errorf("formatCategoryName(%s) = %s, want %s", tt.journalistType, result, tt.expected)
			}
		})
	}
}

func TestTemplateService_Configuration(t *testing.T) {
	// Test with custom configuration
	config := &TemplateConfig{
		CompanyName:    "Test Company",
		NewsletterName: "Test Newsletter",
		Theme:          "modern",
		BaseURL:        "https://test.com",
		StaticURL:      "/assets",
	}

	service, err := NewTemplateService(config)
	if err != nil {
		t.Fatalf("Failed to create template service with config: %v", err)
	}

	if service.config.CompanyName != "Test Company" {
		t.Error("Custom company name not set")
	}

	if service.config.StaticURL != "/assets" {
		t.Error("Custom static URL not set")
	}
}

func TestTemplateService_RenderArchive(t *testing.T) {
	service, err := NewTemplateService(nil)
	if err != nil {
		t.Fatalf("Failed to create template service: %v", err)
	}

	t.Run("Archive with multiple issues", func(t *testing.T) {
		issues := []database.WeeklyNewsletterIssue{
			{
				ID:              1,
				WeekNumber:      40,
				Year:            2025,
				Title:           "Week 40 Newsletter - 2025",
				Status:          database.IssueStatusPublished,
				PublicationDate: time.Date(2025, 10, 2, 9, 30, 0, 0, time.UTC),
				PublishedAt:     &[]time.Time{time.Date(2025, 10, 2, 10, 0, 0, 0, time.UTC)}[0],
				CreatedAt:       time.Now(),
			},
			{
				ID:              2,
				WeekNumber:      39,
				Year:            2025,
				Title:           "Week 39 Newsletter - 2025",
				Status:          database.IssueStatusPublished,
				PublicationDate: time.Date(2025, 9, 25, 9, 30, 0, 0, time.UTC),
				PublishedAt:     &[]time.Time{time.Date(2025, 9, 25, 10, 0, 0, 0, time.UTC)}[0],
				CreatedAt:       time.Now(),
			},
		}

		html, err := service.RenderArchive(context.Background(), issues)
		if err != nil {
			t.Fatalf("Failed to render archive: %v", err)
		}

		if !strings.Contains(html, "Newsletter Archive") {
			t.Error("Archive page title not found")
		}

		if !strings.Contains(html, "Week 40 Newsletter - 2025") {
			t.Error("First issue title not found")
		}

		if !strings.Contains(html, "Week 39 Newsletter - 2025") {
			t.Error("Second issue title not found")
		}

		if !strings.Contains(html, "/40/2025") {
			t.Error("Link to week 40 newsletter not found")
		}

		if !strings.Contains(html, "/39/2025") {
			t.Error("Link to week 39 newsletter not found")
		}

		if !strings.Contains(html, "Week 40, 2025") {
			t.Error("Week/year formatting not found for issue 1")
		}

		if !strings.Contains(html, "/static/css/newsletter.css") {
			t.Error("CSS stylesheet link not found")
		}
	})

	t.Run("Archive with empty issues list", func(t *testing.T) {
		html, err := service.RenderArchive(context.Background(), []database.WeeklyNewsletterIssue{})
		if err != nil {
			t.Fatalf("Failed to render empty archive: %v", err)
		}

		if !strings.Contains(html, "No newsletters yet") {
			t.Error("Empty state message not found")
		}

		if !strings.Contains(html, "Newsletter Archive") {
			t.Error("Archive page title not found in empty state")
		}
	})

	t.Run("Archive HTML structure", func(t *testing.T) {
		issues := []database.WeeklyNewsletterIssue{
			{
				ID:              3,
				WeekNumber:      38,
				Year:            2025,
				Title:           "Week 38 Newsletter - 2025",
				Status:          database.IssueStatusPublished,
				PublicationDate: time.Date(2025, 9, 18, 9, 30, 0, 0, time.UTC),
				CreatedAt:       time.Now(),
			},
		}

		html, err := service.RenderArchive(context.Background(), issues)
		if err != nil {
			t.Fatalf("Failed to render archive: %v", err)
		}

		if !strings.Contains(html, "<!DOCTYPE html>") {
			t.Error("HTML doctype not found")
		}

		if !strings.Contains(html, `<html lang="en">`) {
			t.Error("HTML lang attribute not found")
		}

		if !strings.Contains(html, "viewport") {
			t.Error("Viewport meta tag not found")
		}

		if !strings.Contains(html, "archive-grid") || !strings.Contains(html, "archive-item") {
			t.Error("Archive-specific CSS classes not found")
		}
	})
}
