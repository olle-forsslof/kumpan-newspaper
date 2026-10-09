package kp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type httpTestAccess struct{ webTestAccess }

func (a httpTestAccess) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /auth/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("private-payload")
	})
}

func TestHTTPRoutesAndGates(t *testing.T) {
	s, _ := storeTestOpen(t)
	h := NewHTTPHandler(s, httpTestAccess{}, Config{SigningSecret: commandTestSecret, WorkspaceID: "T-CONFIGURED"})
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 401}, {"GET", "/archive", 401}, {"GET", "/issues/1", 401},
		{"GET", "/draft", 403}, {"GET", "/editor/article/1", 403},
		{"POST", "/editor/article/1/save", 403}, {"POST", "/editor/article/1/remove", 403},
		{"POST", "/editor/article/1/retry", 403}, {"POST", "/editor/issues/1/publish", 403},
		{"POST", "/editor/article/1/image", 403}, {"POST", "/editor/article/1/image/remove", 403},
		{"GET", "/auth/test", 202}, {"POST", "/api/slack/commands", 400},
		{"GET", "/api/slack/commands", 405}, {"GET", "/pp", 404},
		{"POST", "/api/slack/events", 404}, {"POST", "/slack/events", 404},
		{"GET", "/static/", 404}, {"GET", "/static/css/", 404},
		{"GET", "/static/css/newspaper.css", 404}, {"GET", "/internal/kp/views/issue.html", 404},
		{"GET", "/static/css/kp.css/extra", 404}, {"POST", "/static/css/kp.css", 405},
	} {
		w := commandTestServe(h, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != route.status {
			t.Errorf("%s %s: %d, want %d", route.method, route.path, w.Code, route.status)
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("%s: cacheable response", route.path)
		}
	}
	r := commandTestRequest(commandTestForm("/askkp", "Private question").Encode(), time.Now().Unix())
	r.URL.Path = "/api/slack/commands"
	commandTestReceipt(t, commandTestServe(h, r))
	reader := NewHTTPHandler(s, httpTestAccess{webTestAccess{reader: true}}, Config{})
	if w := commandTestServe(reader, httptest.NewRequest("GET", "/archive", nil)); w.Code != 200 {
		t.Fatalf("reader denied: %d", w.Code)
	}
	if w := commandTestServe(reader, httptest.NewRequest("GET", "/draft", nil)); w.Code != 403 {
		t.Fatalf("reader granted editor access: %d", w.Code)
	}
}

func TestHTTPHealth(t *testing.T) {
	s, _ := storeTestOpen(t)
	h := NewHTTPHandler(s, httpTestAccess{}, Config{})
	w := commandTestServe(h, httptest.NewRequest("GET", "/health", nil))
	var health map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || len(health) != 2 || health["status"] != "ok" || health["service"] != "kp" {
		t.Fatalf("invalid health response: %d %s", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = commandTestServe(h, httptest.NewRequest("GET", "/health", nil).WithContext(ctx))
	if w.Code != 503 {
		t.Fatalf("canceled health request: %d", w.Code)
	}
	ctxRequest := httptest.NewRequest("GET", "/health", nil)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	w = commandTestServe(h, ctxRequest)
	if w.Code != 503 || w.Body.String() != "Service unavailable\n" {
		t.Fatalf("closed DB: %d %s", w.Code, w.Body.String())
	}
}

func TestHTTPImageConfiguration(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "Article")
	for _, key := range []string{"", "test-unsplash-key"} {
		h := NewHTTPHandler(s, httpTestAccess{webTestAccess{reader: true, editor: true}}, Config{UnsplashAccessKey: key})
		r := httptest.NewRequest("GET", "/editor/article/"+strconv.Itoa(a.ID), nil)
		w := commandTestServe(h, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `name="image_query"`) != (key != "") {
			t.Fatalf("image configuration mismatch: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestHTTPHealthDeadline(t *testing.T) {
	s, _ := storeTestOpen(t)
	s.DB.SetMaxOpenConns(1)
	conn, err := s.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	h := NewHTTPHandler(s, httpTestAccess{}, Config{})
	start := time.Now()
	w := commandTestServe(h, httptest.NewRequest("GET", "/health", nil))
	if elapsed := time.Since(start); w.Code != 503 || elapsed < 1900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("health deadline: status %d, elapsed %s", w.Code, elapsed)
	}
}

func TestHTTPSecurityAndPrivacy(t *testing.T) {
	s, _ := storeTestOpen(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	for _, baseURL := range []string{"https://kp.example", "http://localhost:8080"} {
		h := NewHTTPHandler(s, httpTestAccess{}, Config{BaseURL: baseURL})
		for _, path := range []string{"/health", "/missing?secret=private-query", "/auth/test", "/auth/panic"} {
			w := commandTestServe(h, httptest.NewRequest("GET", path, strings.NewReader("private-body")))
			for name, want := range map[string]string{
				"Content-Security-Policy": "default-src 'self'; style-src 'self'; img-src 'self' https://images.unsplash.com; script-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'",
				"X-Content-Type-Options":  "nosniff", "Referrer-Policy": "no-referrer",
				"Permissions-Policy": "camera=(),microphone=(),geolocation=()", "Cache-Control": "no-store",
			} {
				if w.Header().Get(name) != want {
					t.Errorf("%s: %s = %q", path, name, w.Header().Get(name))
				}
			}
			wantHSTS := ""
			if strings.HasPrefix(baseURL, "https:") {
				wantHSTS = "max-age=31536000"
			}
			if w.Header().Get("Strict-Transport-Security") != wantHSTS {
				t.Error("incorrect HSTS")
			}
			if path == "/auth/panic" && (w.Code != 500 || w.Body.String() != "Internal server error\n") {
				t.Errorf("panic response: %d %s", w.Code, w.Body.String())
			}
		}
	}
	if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "/auth") {
		t.Fatalf("request/panic leaked: %s", logs.String())
	}
}

func TestHTTPStylesheet(t *testing.T) {
	// ServeFile intentionally resolves its path from the application working directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Error(err)
		}
	}()
	css, err := os.ReadFile("static/css/kp.css")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPHandler(nil, httpTestAccess{}, Config{})
	w := commandTestServe(h, httptest.NewRequest("GET", "/static/css/kp.css", nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), css) || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") || w.Header().Get("Cache-Control") != "public, max-age=3600" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stylesheet response: %d %v", w.Code, w.Header())
	}
}
