package kp

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

func TestMessengerSend(t *testing.T) {
	for _, recipient := range []string{"U-private", "W-private", "C-channel", "D-channel"} {
		t.Run(recipient, func(t *testing.T) {
			var paths []string
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				paths = append(paths, r.URL.Path)
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("token") != "private-token" {
					t.Error("missing token")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/conversations.open":
					if r.Form.Get("users") != recipient {
						t.Errorf("users = %q", r.Form.Get("users"))
					}
					fmt.Fprint(w, `{"ok":true,"channel":{"id":"D-opened"}}`)
				case "/chat.postMessage":
					channel := recipient
					if strings.HasPrefix(recipient, "U") || strings.HasPrefix(recipient, "W") {
						channel = "D-opened"
					}
					if r.Form.Get("channel") != channel || r.Form.Get("text") != "private <text> & link" || r.Form.Get("unfurl_links") != "false" || r.Form.Get("unfurl_media") != "false" {
						t.Error("incorrect message parameters")
					}
					fmt.Fprint(w, `{"ok":true,"channel":"D-opened","ts":"1.0"}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			m := NewMessenger("private-token")
			m.client = slack.New("private-token", slack.OptionAPIURL(server.URL+"/"), slack.OptionHTTPClient(server.Client()))
			if err := m.Send(context.Background(), recipient, "private <text> & link"); err != nil {
				t.Fatal(err)
			}
			want := []string{"/chat.postMessage"}
			if strings.HasPrefix(recipient, "U") || strings.HasPrefix(recipient, "W") {
				want = []string{"/conversations.open", "/chat.postMessage"}
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(paths, want) {
				t.Fatalf("calls = %v, want %v", paths, want)
			}
		})
	}
}

func TestMessengerFailuresArePrivate(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	for _, stage := range []string{"open", "post", "empty", "transport", "canceled", "deadline", "timeout"} {
		t.Run(stage, func(t *testing.T) {
			var calls atomic.Int32
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if stage == "timeout" || stage == "deadline" {
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				if stage == "transport" {
					http.Error(w, "private-token private-text U-private", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if stage == "empty" {
					fmt.Fprint(w, `{"ok":true,"channel":{"id":""}}`)
				} else if stage == "post" && r.URL.Path == "/conversations.open" {
					fmt.Fprint(w, `{"ok":true,"channel":{"id":"D-private"}}`)
				} else {
					fmt.Fprint(w, `{"ok":false,"error":"private-token private-text U-private"}`)
				}
			}))
			defer server.Close()
			defer close(release)
			client := server.Client()
			client.Timeout = 10 * time.Second
			if stage == "timeout" {
				client.Timeout = 20 * time.Millisecond
			}
			m := &SlackMessenger{client: slack.New("private-token", slack.OptionAPIURL(server.URL+"/"), slack.OptionHTTPClient(client))}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "canceled" {
				cancel()
			}
			if stage == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 20*time.Millisecond)
				defer stop()
			}
			start := time.Now()
			err := m.Send(ctx, "U-private", "private-text")
			if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), server.URL) || strings.Contains(err.Error(), "context") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			if time.Since(start) > time.Second {
				t.Error("cancellation/timeout was not prompt")
			}
			if stage == "canceled" && calls.Load() != 0 {
				t.Error("canceled request reached server")
			}
			if stage != "post" && stage != "canceled" && calls.Load() != 1 {
				t.Errorf("calls = %d, want 1", calls.Load())
			}
		})
	}
	if logs.Len() != 0 {
		t.Fatalf("messenger logged data: %s", logs.String())
	}
}
