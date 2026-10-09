package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/auth"
	"github.com/olle-forsslof/kumpan-newspaper/internal/kp"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("KP stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := kp.LoadConfig()
	if err != nil {
		return err
	}
	access, err := auth.New(ctx, auth.Config{
		ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, BaseURL: cfg.BaseURL,
		WorkspaceID: cfg.WorkspaceID, SessionSecret: cfg.SessionSecret, EditorIDs: cfg.EditorIDs,
	})
	if err != nil {
		return err
	}
	store, err := kp.Open(cfg.DatabasePath)
	if err != nil {
		return errors.New("could not open KP database; use a separate, writable DATABASE_PATH")
	}
	defer store.Close()

	workerConfig := kp.WorkerConfig{BaseURL: cfg.BaseURL, PublishChannel: cfg.PublishChannel, EditorIDs: cfg.EditorIDs}
	if cfg.UnsplashAccessKey != "" {
		workerConfig.Photos = kp.NewUnsplash(cfg.UnsplashAccessKey)
	}
	worker := kp.NewWorker(store, kp.NewReporter(cfg.APIKey, cfg.Model), kp.NewMessenger(cfg.BotToken), workerConfig, logger)
	// Requests drain independently of the signal that stops the worker.
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	server := &http.Server{
		Addr: ":" + cfg.Port, Handler: kp.NewHTTPHandler(store, access, cfg),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
		BaseContext: func(net.Listener) context.Context { return requestCtx },
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return errors.New("could not listen on configured PORT")
	}
	return serve(ctx, server, listener, worker.Run, logger)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, worker func(context.Context) error, logger *slog.Logger) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() { workerDone <- worker(workerCtx) }()
	go func() { serverDone <- server.Serve(listener) }()
	logger.Info("KP started", "address", listener.Addr().String())

	var result error
	workerStopped, serverStopped := false, false
	select {
	case <-ctx.Done():
	case err := <-workerDone:
		workerStopped = true
		if !errors.Is(err, context.Canceled) {
			result = errors.New("background worker stopped unexpectedly")
		}
	case err := <-serverDone:
		serverStopped = true
		if !errors.Is(err, http.ErrServerClosed) {
			result = errors.New("HTTP server stopped unexpectedly")
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		server.Close()
		result = errors.New("HTTP shutdown timed out")
	}
	if !serverStopped {
		<-serverDone
	}
	if !workerStopped {
		select {
		case <-workerDone:
		case <-shutdownCtx.Done():
			return errors.New("worker shutdown timed out")
		}
	}
	logger.Info("KP stopped")
	return result
}
