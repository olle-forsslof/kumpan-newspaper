package kp

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const commandTestSecret = "test-signing-secret"

func commandTestForm(command, text string) url.Values {
	return url.Values{
		"team_id": {"T-CONFIGURED"}, "user_id": {"U-PRIVATE-SENDER"},
		"user_name": {"private-sender-name"}, "command": {command}, "text": {text},
		"token": {"private-token"}, "response_url": {"https://example.invalid/private-response"},
	}
}

func commandTestRequest(body string, timestamp int64) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	ts := strconv.FormatInt(timestamp, 10)
	r.Header.Set("X-Slack-Request-Timestamp", ts)
	mac := hmac.New(sha256.New, []byte(commandTestSecret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	r.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return r
}

func commandTestServe(handler http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func commandTestReceipt(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("receipt status/type: %d, %s", w.Code, w.Header().Get("Content-Type"))
	}
	var receipt struct {
		ResponseType string `json:"response_type"`
		Text         string `json:"text"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ResponseType != "ephemeral" || receipt.Text == "" {
		t.Fatalf("invalid receipt: %+v", receipt)
	}
	return receipt.Text
}

func TestCommandReportAndReplay(t *testing.T) {
	for _, name := range []string{"private-sender-name", ""} {
		t.Run("name="+name, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			h := NewCommandHandler(s, commandTestSecret, "T-CONFIGURED")
			form := commandTestForm("/tellkp", "  submit admin publish arbitrary report \n")
			form.Set("user_name", name)
			body, timestamp := form.Encode(), time.Now().Unix()
			r := commandTestRequest(body, timestamp)
			signature := r.Header.Get("X-Slack-Signature")
			commandTestReceipt(t, commandTestServe(h, r))
			commandTestReceipt(t, commandTestServe(h, commandTestRequest(body, timestamp)))
			var count int
			if err := s.DB.QueryRow("SELECT count(*) FROM kp_articles").Scan(&count); err != nil || count != 1 {
				t.Fatalf("replay row count: %d, %v", count, err)
			}
			a, err := s.GetArticle(1)
			if err != nil {
				t.Fatal(err)
			}
			wantName := name
			if wantName == "" {
				wantName = "U-PRIVATE-SENDER"
			}
			if a.Kind != KindReport || a.AuthorID != "U-PRIVATE-SENDER" || a.AuthorName != wantName || a.Original != "submit admin publish arbitrary report" || a.Status != StatusPending {
				t.Fatalf("report: %+v", a)
			}
			var key string
			if err := s.DB.QueryRow("SELECT request_key FROM kp_articles").Scan(&key); err != nil {
				t.Fatal(err)
			}
			mac := hmac.New(sha256.New, []byte(commandTestSecret))
			mac.Write([]byte("kp/slack/request-key/v1\x00" + signature))
			if key != hex.EncodeToString(mac.Sum(nil)) || key == signature[3:] {
				t.Fatal("request key is not a domain-separated keyed digest")
			}
		})
	}
}

func TestCommandQuestionAnonymousStorageAndLogs(t *testing.T) {
	s, path := storeTestOpen(t)
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	h := NewCommandHandler(s, commandTestSecret, "T-CONFIGURED")
	form := commandTestForm("/askkp", "  admin submit remove Why is this happening?  ")
	r := commandTestRequest(form.Encode(), time.Now().Unix())
	signature := r.Header.Get("X-Slack-Signature")
	receipt := commandTestReceipt(t, commandTestServe(h, r))
	if strings.Contains(receipt, strings.TrimSpace(form.Get("text"))) || !strings.Contains(receipt, "anonymt") || !strings.Contains(receipt, "Slack") {
		t.Fatal("question receipt echoes the question or omits privacy information")
	}
	a, err := s.GetArticle(1)
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindQuestion || a.AuthorID != "" || a.AuthorName != "" || a.Original != strings.TrimSpace(form.Get("text")) {
		t.Fatalf("question storage: %+v", a)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{form.Get("user_id"), form.Get("user_name"), form.Get("token"), form.Get("response_url"), signature, signature[3:]} {
		if bytes.Contains(database, []byte(private)) || strings.Contains(logs.String(), private) || strings.Contains(receipt, private) {
			t.Fatal("private request metadata retained or disclosed")
		}
	}
	if logs.Len() != 0 {
		t.Fatal("handler logged question request")
	}
}

func TestCommandRejectedRequests(t *testing.T) {
	for _, tc := range []struct {
		name              string
		secret, workspace string
		mutate            func(*http.Request)
		body              string
		offset            int64
		status            int
	}{
		{name: "tampered", mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(commandTestForm("/tellkp", "tampered").Encode()))
		}, status: 401},
		{name: "old", offset: -301, status: 401},
		{name: "future", offset: 120, status: 401},
		{name: "empty secret", secret: "empty", status: 503},
		{name: "empty workspace", workspace: "empty", status: 503},
		{name: "wrong workspace", workspace: "T-OTHER", status: 403},
		{name: "get", mutate: func(r *http.Request) { r.Method = http.MethodGet }, status: 405},
		{name: "content type", mutate: func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }, status: 400},
		{name: "missing content type", mutate: func(r *http.Request) { r.Header.Del("Content-Type") }, status: 400},
		{name: "signature hex", mutate: func(r *http.Request) { r.Header.Set("X-Slack-Signature", "v0="+strings.Repeat("z", 64)) }, status: 401},
		{name: "signature version", mutate: func(r *http.Request) { r.Header.Set("X-Slack-Signature", "v1="+strings.Repeat("0", 64)) }, status: 401},
		{name: "missing signature", mutate: func(r *http.Request) { r.Header.Del("X-Slack-Signature") }, status: 401},
		{name: "invalid timestamp", mutate: func(r *http.Request) { r.Header.Set("X-Slack-Request-Timestamp", "invalid") }, status: 401},
		{name: "overflow timestamp", mutate: func(r *http.Request) { r.Header.Set("X-Slack-Request-Timestamp", "9223372036854775807") }, status: 401},
		{name: "invalid encoding", body: "text=%zz", status: 400},
		{name: "invalid encoding unsigned", body: "text=%zz", mutate: func(r *http.Request) { r.Header.Del("X-Slack-Signature") }, status: 401},
		{name: "duplicate command", body: commandTestForm("/tellkp", "report").Encode() + "&command=%2Faskkp", status: 400},
		{name: "missing user", body: "team_id=T-CONFIGURED&command=%2Faskkp&text=question", status: 400},
		{name: "body limit", body: strings.Repeat("x", 64*1024+1), status: 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			secret, workspace := commandTestSecret, "T-CONFIGURED"
			if tc.secret == "empty" {
				secret = ""
			}
			if tc.workspace != "" {
				workspace = tc.workspace
			}
			if workspace == "empty" {
				workspace = ""
			}
			body := tc.body
			if body == "" {
				body = commandTestForm("/tellkp", "report").Encode()
			}
			r := commandTestRequest(body, time.Now().Unix()+tc.offset)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := commandTestServe(NewCommandHandler(s, secret, workspace), r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			var count int
			if err := s.DB.QueryRow("SELECT count(*) FROM kp_articles").Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected request stored: %d, %v", count, err)
			}
		})
	}
}

func TestCommandUsageDoesNotStore(t *testing.T) {
	for _, tc := range []struct{ command, text, want string }{
		{"/tellkp", " \n ", "/tellkp"}, {"/askkp", "", "/askkp"},
		{"/unknown", "report", "/tellkp"}, {"/tellkp extra", "report", "/tellkp"},
		{"/tellkp", strings.Repeat("x", 10001), "10000"},
		{"/askkp", strings.Repeat("\u00e5", 5001), "10000"},
	} {
		t.Run(tc.command+tc.want, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			w := commandTestServe(NewCommandHandler(s, commandTestSecret, "T-CONFIGURED"), commandTestRequest(commandTestForm(tc.command, tc.text).Encode(), time.Now().Unix()))
			if text := commandTestReceipt(t, w); !strings.Contains(text, tc.want) {
				t.Fatalf("unhelpful usage: %s", text)
			}
			var count int
			if err := s.DB.QueryRow("SELECT count(*) FROM kp_issues").Scan(&count); err != nil || count != 0 {
				t.Fatalf("usage created draft: %d, %v", count, err)
			}
		})
	}
}

func TestCommandLimitsAndFutureTolerance(t *testing.T) {
	s, _ := storeTestOpen(t)
	h := NewCommandHandler(s, commandTestSecret, "T-CONFIGURED")
	form := commandTestForm("/tellkp", strings.Repeat("x", 10000))
	body := form.Encode()
	body += "&ignored=" + strings.Repeat("x", 64*1024-len(body)-len("&ignored="))
	commandTestReceipt(t, commandTestServe(h, commandTestRequest(body, time.Now().Unix()+60)))
	a, err := s.GetArticle(1)
	if err != nil || len(a.Original) != 10000 {
		t.Fatalf("boundary report: %v", err)
	}
}

func TestCommandStoreErrorIsGeneric(t *testing.T) {
	s, _ := storeTestOpen(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	w := commandTestServe(NewCommandHandler(s, commandTestSecret, "T-CONFIGURED"), commandTestRequest(commandTestForm("/askkp", "private question").Encode(), time.Now().Unix()))
	if w.Code != 503 || strings.TrimSpace(w.Body.String()) != "Service unavailable" {
		t.Fatalf("database error disclosed: %d, %s", w.Code, w.Body.String())
	}
}
