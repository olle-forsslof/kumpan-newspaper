package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/olle-forsslof/kumpan-newspaper/internal/ai"
	"github.com/olle-forsslof/kumpan-newspaper/internal/config"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/olle-forsslof/kumpan-newspaper/internal/server"
	"github.com/olle-forsslof/kumpan-newspaper/internal/slack"
)

func main() {
	// load configuration
	cfg := config.Load()

	// validate critical configuration
	if err := cfg.Validate(); err != nil {
		log.Fatal("Configuration error: ", err)
	}

	// set up logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	db, err := database.NewSimple(cfg.DatabasePath)
	if err != nil {
		log.Fatal("Failed to initialize database: ", err)
	}

	if err := db.Migrate(); err != nil {
		log.Fatal("Failed to run database migrations: ", err)
	}

	questionSelector := database.NewQuestionSelector(db.DB)
	submissionManager := database.NewSubmissionManager(db.DB)

	// Create AI processor (AnthropicService implements the AIProcessor interface)
	aiProcessor := ai.NewAnthropicService(cfg.AnthropicAPIKey)

	// Create bot with full weekly automation capabilities
	slackBot := slack.NewBotWithWeeklyAutomation(slack.SlackConfig{
		Token:         cfg.SlackBotToken,
		SigningSecret: cfg.SlackSigningSecret,
	}, questionSelector, cfg.AdminUsers, submissionManager, aiProcessor, db)

	// create server with dependencies - pass the slackBot
	srv := server.NewWithBot(cfg, logger, slackBot) //  You'll need to create this method

	// set up routes
	srv.SetupRoutes()

	// start server
	logger.Info("Starting newsletter service")
	if err := srv.Start(); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
