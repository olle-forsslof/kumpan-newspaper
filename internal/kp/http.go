package kp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type AuthRoutes interface {
	Access
	Register(*http.ServeMux)
}

func NewHTTPHandler(store *Store, access AuthRoutes, cfg Config) http.Handler {
	mux := http.NewServeMux()
	access.Register(mux)
	web := NewWeb(store, access)
	web.imagesEnabled = cfg.UnsplashAccessKey != ""
	web.Register(mux)
	mux.Handle("POST /api/slack/commands", NewCommandHandler(store, cfg.SigningSecret, cfg.WorkspaceID))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if store == nil || store.DB == nil || store.DB.PingContext(ctx) != nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Status  string `json:"status"`
			Service string `json:"service"`
		}{"ok", "kp"})
	})
	mux.HandleFunc("GET /static/css/kp.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, r, "static/css/kp.css")
	})
	baseURL, err := url.Parse(cfg.BaseURL)
	https := err == nil && baseURL.Scheme == "https"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' https://images.unsplash.com; script-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(),microphone=(),geolocation=()")
		w.Header().Set("Cache-Control", "no-store")
		if https {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		defer func() {
			if recover() != nil {
				slog.Error("HTTP handler panic")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
