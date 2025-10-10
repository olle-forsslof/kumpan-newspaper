package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/olle-forsslof/kumpan-newspaper/internal/ai"
	"github.com/olle-forsslof/kumpan-newspaper/internal/config"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/olle-forsslof/kumpan-newspaper/internal/scheduler"
	"github.com/olle-forsslof/kumpan-newspaper/internal/server"
	"github.com/olle-forsslof/kumpan-newspaper/internal/slack"
	"github.com/olle-forsslof/kumpan-newspaper/internal/templates"
)

func main() {
	cfg := config.Load()

	if err := cfg.Validate(); err != nil {
		log.Fatal("Configuration error: ", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	db, err := database.NewSimple(cfg.DatabasePath)
	if err != nil {
		log.Fatal("Failed to initialize database: ", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatal("Failed to run database migrations: ", err)
	}

	questionSelector := database.NewQuestionSelector(db.DB)
	submissionManager := database.NewSubmissionManager(db.DB)

	aiProcessor := ai.NewAnthropicService(cfg.AnthropicAPIKey)

	slackBot := slack.NewBotWithWeeklyAutomation(slack.SlackConfig{
		Token:         cfg.SlackBotToken,
		SigningSecret: cfg.SlackSigningSecret,
	}, questionSelector, cfg.AdminUsers, submissionManager, aiProcessor, db)

	templateService, err := templates.NewTemplateService(nil)
	if err != nil {
		log.Fatal("Failed to create template service: ", err)
	}

	publisher, err := scheduler.NewNewsletterPublisher(db, logger)
	if err != nil {
		log.Fatal("Failed to create newsletter publisher: ", err)
	}

	go publisher.Start()

	srv := server.NewWithBotAndTemplates(cfg, logger, slackBot, db, templateService)
	srv.SetupRoutes()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("Starting newsletter service")
		if err := srv.Start(); err != nil {
			log.Fatal("Server failed to start: ", err)
		}
	}()

	<-ctx.Done()

	logger.Info("Shutdown signal received, stopping services...")
	publisher.Stop()
	logger.Info("Newsletter service stopped gracefully")
}
