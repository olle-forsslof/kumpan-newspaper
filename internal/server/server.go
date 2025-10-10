package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/config"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/olle-forsslof/kumpan-newspaper/internal/slack"
	"github.com/olle-forsslof/kumpan-newspaper/internal/templates"
)

type Server struct {
	config          *config.Config
	logger          *slog.Logger
	slack           slack.Bot
	mux             *http.ServeMux
	db              *database.DB
	templateService *templates.TemplateService
}

func New(cfg *config.Config, logger *slog.Logger) *Server {
	var slackBot slack.Bot
	if cfg.SlackBotToken != "" {
		// For the basic New function, we pass nil for the question selector
		// The caller should use NewWithBot if they want full functionality
		slackBot = slack.NewBot(slack.SlackConfig{
			Token:         cfg.SlackBotToken,
			SigningSecret: cfg.SlackSigningSecret,
		}, nil, cfg.AdminUsers)
		logger.Info("Slack bot initialized without question selector")
	} else {
		logger.Warn("No Slack bot token provided - Slack integration disabled")
	}

	return &Server{
		config: cfg,
		logger: logger,
		slack:  slackBot,
		mux:    http.NewServeMux(), // Initialize a custom mux
	}
}

func (s *Server) SetupRoutes() {
	s.mux.HandleFunc("/health", s.healthHandler)

	// Static file serving for CSS and assets
	staticDir := http.Dir("./static/")
	s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(staticDir)))

	// Newsletter template routes
	if s.templateService != nil {
		s.mux.HandleFunc("/draft", s.draftHandler)
		s.mux.HandleFunc("/archive", s.archiveHandler)
		s.mux.HandleFunc("/", s.specificNewsletterHandler)
	}

	if s.slack != nil {
		slackHandler := slack.NewSlashCommandHandlerWithSecurity(
			s.slack,
			s.config.SlackSigningSecret,
		)

		eventHandler := slack.NewEventCallbackHandler(s.slack, s.config.SlackSigningSecret)

		// Register the handlers with our custom mux
		s.mux.Handle("/api/slack/commands", slackHandler)
		s.mux.Handle("/api/slack/events", eventHandler)
		s.logger.Info("Registered Slack command handler at /api/slack/commands")
	}
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("Health check requested",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "ok", "service": "newsletter"}`)
}

func (s *Server) Start() error {
	s.logger.Info("Starting server", slog.String("port", s.config.Port))
	return http.ListenAndServe(":"+s.config.Port, s.mux)
}

// Handler returns the server's HTTP handler for testing
func (s *Server) Handler() http.Handler {
	return s.mux
}

func NewWithBot(cfg *config.Config, logger *slog.Logger, bot slack.Bot) *Server {
	return &Server{
		config: cfg,
		logger: logger,
		slack:  bot,
		mux:    http.NewServeMux(),
	}
}

// NewWithBotAndTemplates creates a server with bot and template rendering capabilities
func NewWithBotAndTemplates(cfg *config.Config, logger *slog.Logger, bot slack.Bot, db *database.DB, templateService *templates.TemplateService) *Server {
	return &Server{
		config:          cfg,
		logger:          logger,
		slack:           bot,
		mux:             http.NewServeMux(),
		db:              db,
		templateService: templateService,
	}
}

// specificNewsletterHandler routes requests to either most recent published or specific issue
func (s *Server) specificNewsletterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	segments := strings.Split(strings.Trim(path, "/"), "/")

	if len(segments) == 1 && segments[0] == "" {
		issue, err := s.db.GetMostRecentPublishedIssue()
		if err != nil {
			s.logger.Error("Failed to get most recent published issue", "error", err)
			http.Error(w, "No published newsletters available", http.StatusNotFound)
			return
		}
		s.renderNewsletter(w, r, issue)
		return
	}

	if len(segments) == 2 {
		week, err1 := strconv.Atoi(segments[0])
		year, err2 := strconv.Atoi(segments[1])

		if err1 == nil && err2 == nil {
			issue, err := s.db.GetOrCreateWeeklyIssue(week, year)
			if err != nil {
				s.logger.Error("Failed to get newsletter issue", "week", week, "year", year, "error", err)
				http.Error(w, "Newsletter not found", http.StatusNotFound)
				return
			}
			s.renderNewsletter(w, r, issue)
			return
		}
	}

	http.Error(w, "Invalid URL format. Use / for current or /{week}/{year} for specific issue", http.StatusBadRequest)
}

// draftHandler serves the current week's draft newsletter
func (s *Server) draftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	now := time.Now()
	year, week := now.ISOWeek()

	issue, err := s.db.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		s.logger.Error("Failed to get current week draft", "error", err)
		http.Error(w, "Failed to load draft", http.StatusInternalServerError)
		return
	}

	s.renderNewsletter(w, r, issue)
}

// archiveHandler serves the archive page listing published newsletter issues
func (s *Server) archiveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	issues, err := s.db.GetPublishedNewsletterIssues()
	if err != nil {
		s.logger.Error("Failed to get published newsletter issues for archive", "error", err)
		http.Error(w, "Failed to load archive", http.StatusInternalServerError)
		return
	}

	html, err := s.templateService.RenderArchive(r.Context(), issues)
	if err != nil {
		s.logger.Error("Failed to render archive template", "error", err)
		http.Error(w, "Failed to render archive", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))

	s.logger.Info("Archive page rendered successfully", "issues_count", len(issues))
}

// renderNewsletter renders a newsletter issue with its articles
func (s *Server) renderNewsletter(w http.ResponseWriter, r *http.Request, issue *database.WeeklyNewsletterIssue) {
	// Get processed articles for this issue
	articles, err := s.db.GetProcessedArticlesByNewsletterIssue(issue.ID)
	if err != nil {
		s.logger.Error("Failed to get articles for newsletter", "issue_id", issue.ID, "error", err)
		// Continue with empty articles rather than error - show empty newsletter
		articles = []database.ProcessedArticle{}
	}

	// Render the newsletter
	html, err := s.templateService.RenderNewsletter(r.Context(), issue, articles)
	if err != nil {
		s.logger.Error("Failed to render newsletter template", "issue_id", issue.ID, "error", err)
		http.Error(w, "Failed to render newsletter", http.StatusInternalServerError)
		return
	}

	// Set content type and write response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))

	s.logger.Info("Newsletter rendered successfully",
		"issue_id", issue.ID,
		"week", issue.WeekNumber,
		"year", issue.Year,
		"articles_count", len(articles))
}
