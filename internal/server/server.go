package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/olle-forsslof/kumpan-newspaper/internal/config"
	"github.com/olle-forsslof/kumpan-newspaper/internal/slack"
)

type Server struct {
	config *config.Config
	logger *slog.Logger
	slack  slack.Bot
	mux    *http.ServeMux // Add a custom mux field
}

func New(cfg *config.Config, logger *slog.Logger) *Server {
	var slackBot slack.Bot
	if cfg.SlackBotToken != "" {
		slackBot = slack.NewBot(slack.SlackConfig{
			Token:         cfg.SlackBotToken,
			SigningSecret: cfg.SlackSigningSecret,
		}, nil, cfg.AdminUsers)
		logger.Info("Slack bot initialized")
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
	s.mux.HandleFunc("/", s.rootHandler)

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

func (s *Server) rootHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("Root endpoint accessed",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)

	fmt.Fprintf(w, "Newsletter service is running")
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
