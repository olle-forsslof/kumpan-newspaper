package kp_test

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/auth"
	"github.com/olle-forsslof/kumpan-newspaper/internal/kp"
)

type integrationRoundTripper func(*http.Request) (*http.Response, error)

func (f integrationRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestIntegrationAuthSubmissionAndPublication(t *testing.T) {
	// This test changes the default transport and must not run in parallel.
	cfg := kp.Config{
		BaseURL: "https://kp.example.test", WorkspaceID: "Ttest",
		SigningSecret: "test-signing-secret", ClientID: "client", ClientSecret: "test-client-secret",
		SessionSecret: strings.Repeat("s", 32), EditorIDs: []string{"Ueditor"},
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	tokens := make(map[string]string)
	requests := make(map[string]int)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{
				"issuer": "https://slack.com", "jwks_uri": "https://slack.com/keys",
				"token_endpoint": "https://slack.com/token", "authorization_endpoint": "https://slack.com/authorize",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
				"kty": "RSA", "kid": "integration", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
			}}})
		case "/token":
			if r.Method != http.MethodPost || r.ParseForm() != nil ||
				r.PostForm.Get("client_id") != cfg.ClientID || r.PostForm.Get("client_secret") != cfg.ClientSecret ||
				r.PostForm.Get("redirect_uri") != cfg.BaseURL+"/auth/callback" || r.PostForm.Get("grant_type") != "authorization_code" {
				t.Error("invalid token exchange")
				http.Error(w, "invalid exchange", http.StatusBadRequest)
				return
			}
			mu.Lock()
			token, ok := tokens[r.PostForm.Get("code")]
			delete(tokens, r.PostForm.Get("code"))
			mu.Unlock()
			if !ok {
				http.Error(w, "invalid code", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "id_token": token})
		default:
			t.Errorf("unexpected provider request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	providerURL, err := url.Parse(provider.URL)
	if err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = integrationRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "slack.com" {
			t.Errorf("unexpected outbound request: %s", r.URL)
			return nil, errors.New("integration test blocks non-Slack requests")
		}
		// Preserve the discovery paths and query, without mutating the caller's request.
		local := r.Clone(r.Context())
		local.URL.Scheme, local.URL.Host = providerURL.Scheme, providerURL.Host
		local.Host = providerURL.Host
		return originalTransport.RoundTrip(local)
	})
	access, err := auth.New(context.Background(), auth.Config{
		ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, BaseURL: cfg.BaseURL,
		WorkspaceID: cfg.WorkspaceID, SessionSecret: cfg.SessionSecret, EditorIDs: cfg.EditorIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := kp.Open(filepath.Join(t.TempDir(), "integration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	handler := kp.NewHTTPHandler(store, access, cfg)
	request := func(method, path string, form url.Values, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, strings.NewReader(form.Encode()))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	cookieFrom := func(w *httptest.ResponseRecorder, name string) *http.Cookie {
		t.Helper()
		for _, c := range w.Result().Cookies() {
			if c.Name == name && c.MaxAge > 0 {
				if !c.Secure || !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
					t.Fatalf("unsafe auth cookie: %+v", c)
				}
				return c
			}
		}
		t.Fatalf("missing %s cookie", name)
		return nil
	}
	hidden := func(w *httptest.ResponseRecorder, name string) string {
		t.Helper()
		match := regexp.MustCompile(`<input type="hidden" name="` + regexp.QuoteMeta(name) + `" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
		if len(match) != 2 {
			t.Fatalf("missing hidden %s field", name)
		}
		return html.UnescapeString(match[1])
	}
	login := func(userID, workspace string, want int) *http.Cookie {
		t.Helper()
		begin := request(http.MethodGet, "/auth/login", nil, nil, http.StatusSeeOther)
		flow := cookieFrom(begin, "__Host-newspaper-flow")
		authorize, err := url.Parse(begin.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		q := authorize.Query()
		if authorize.Scheme != "https" || authorize.Host != "slack.com" || authorize.Path != "/authorize" ||
			q.Get("client_id") != cfg.ClientID || q.Get("redirect_uri") != cfg.BaseURL+"/auth/callback" ||
			q.Get("team") != cfg.WorkspaceID || q.Get("scope") != "openid profile" || q.Get("response_type") != "code" {
			t.Fatalf("invalid authorization redirect: %s", authorize)
		}
		for _, name := range []string{"state", "nonce"} {
			decoded, err := base64.RawURLEncoding.DecodeString(q.Get(name))
			if err != nil || len(decoded) != 32 {
				t.Fatalf("invalid %s", name)
			}
		}
		claims, err := json.Marshal(map[string]any{
			"iss": "https://slack.com", "aud": cfg.ClientID, "sub": userID,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": q.Get("nonce"),
			"https://slack.com/user_id": userID, "https://slack.com/team_id": workspace, "name": userID,
		})
		if err != nil {
			t.Fatal(err)
		}
		unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"integration"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
		digest := sha256.Sum256([]byte(unsigned))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		code := "code-" + q.Get("state")
		mu.Lock()
		tokens[code] = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
		mu.Unlock()
		callback := "/auth/callback?" + url.Values{"state": {q.Get("state")}, "code": {code}}.Encode()
		w := request(http.MethodGet, callback, nil, flow, want)
		request(http.MethodGet, callback, nil, flow, http.StatusUnauthorized)
		if want != http.StatusSeeOther {
			for _, c := range w.Result().Cookies() {
				if c.Name == "__Host-newspaper-session" && c.MaxAge > 0 {
					t.Fatal("denied workspace received a session")
				}
			}
			return nil
		}
		if w.Header().Get("Location") != "/" {
			t.Fatal("callback did not redirect home")
		}
		return cookieFrom(w, "__Host-newspaper-session")
	}
	for _, path := range []string{"/", "/archive", "/draft"} {
		w := request(http.MethodGet, path, nil, nil, http.StatusSeeOther)
		if w.Header().Get("Location") != "/auth/login" {
			t.Fatalf("unauthenticated %s did not redirect to login", path)
		}
	}
	login("Uoutsider", "Tother", http.StatusUnauthorized)
	editor := login("Ueditor", cfg.WorkspaceID, http.StatusSeeOther)
	reader := login("Ureader", cfg.WorkspaceID, http.StatusSeeOther)

	reportOriginal := "Vi planterade ett nytt trad vid kontoret."
	questionOriginal := "Jag heter Karin Hemligsson. Hur ordnar vi en gemensam fikapaus?"
	redactedQuestion := "Hur ordnar vi en gemensam fikapaus?"
	for _, submission := range []struct{ command, text, userID, name string }{
		{"/tellkp", reportOriginal, "Ureporter", "Sven Rapport"},
		{"/askkp", questionOriginal, "Uanonymous", "Karin Hemligsson"},
	} {
		form := url.Values{"team_id": {cfg.WorkspaceID}, "user_id": {submission.userID}, "user_name": {submission.name}, "command": {submission.command}, "text": {submission.text}}
		body := form.Encode()
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(cfg.SigningSecret))
		mac.Write([]byte("v0:" + timestamp + ":" + body))
		signature := "v0=" + hex.EncodeToString(mac.Sum(nil))
		// Replaying the signed receipt must not create a second article.
		for attempt := 0; attempt < 2; attempt++ {
			r := httptest.NewRequest(http.MethodPost, cfg.BaseURL+"/api/slack/commands", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("X-Slack-Request-Timestamp", timestamp)
			r.Header.Set("X-Slack-Signature", signature)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			var receipt struct {
				ResponseType string `json:"response_type"`
				Text         string `json:"text"`
			}
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.ResponseType != "ephemeral" || !strings.HasPrefix(receipt.Text, "Tack!") {
				t.Fatalf("invalid receipt: %d %s", w.Code, w.Body.String())
			}
			for _, private := range []string{submission.text, submission.userID, submission.name, signature, cfg.SigningSecret} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatalf("receipt leaked %q", private)
				}
			}
		}
	}
	draft, err := store.CurrentDraft()
	if err != nil {
		t.Fatal(err)
	}
	articles, err := store.Articles(draft.ID)
	if err != nil || len(articles) != 2 {
		t.Fatalf("submissions: count %d, error %v", len(articles), err)
	}
	report, question := articles[0], articles[1]
	if report.Kind != kp.KindReport || report.AuthorID != "Ureporter" || report.AuthorName != "Sven Rapport" || report.Original != reportOriginal ||
		question.Kind != kp.KindQuestion || question.AuthorID != "" || question.AuthorName != "" || question.Original != questionOriginal {
		t.Fatalf("incorrect stored submissions: %+v", articles)
	}
	var authorID, authorName, requestKey string
	if err := store.DB.QueryRow("SELECT author_id, author_name, request_key FROM kp_articles WHERE id = ?", question.ID).Scan(&authorID, &authorName, &requestKey); err != nil {
		t.Fatal(err)
	}
	if authorID != "" || authorName != "" || len(requestKey) != 64 || strings.Contains(requestKey, "Uanonymous") {
		t.Fatal("anonymous submission retained sender information")
	}
	// Generation is covered separately; these fixtures test the publication pipeline.
	for _, expected := range articles {
		claimed, err := store.ClaimNext()
		if err != nil || claimed == nil || claimed.ID != expected.ID {
			t.Fatalf("claim: %+v, error %v", claimed, err)
		}
		generated := kp.Generated{Headline: "Ett nytt trad", Body: "Ett trad ger gronska vid kontoret."}
		if claimed.Kind == kp.KindQuestion {
			generated = kp.Generated{Headline: "En paus tillsammans", Body: "Valj en tid som passar gruppen.", Question: redactedQuestion, Signature: "En fikasugen kollega", Signoff: "Ta hand om varandra."}
		}
		if err := store.CompleteArticle(claimed.ID, claimed.Revision, generated); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ClaimNext(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unexpected queued submission: %v", err)
	}
	issuePath := fmt.Sprintf("/issues/%d", draft.ID)
	editorPath := fmt.Sprintf("/editor/article/%d", question.ID)
	publishPath := fmt.Sprintf("/editor/issues/%d/publish", draft.ID)
	readerHome := request(http.MethodGet, "/", nil, reader, http.StatusOK)
	readerCSRF := hidden(readerHome, "csrf_token")
	for _, w := range []*httptest.ResponseRecorder{readerHome, request(http.MethodGet, "/archive", nil, reader, http.StatusOK)} {
		for _, private := range []string{reportOriginal, questionOriginal, "Karin Hemligsson", "Ett nytt trad", redactedQuestion} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("reader page exposed draft content %q", private)
			}
		}
	}
	request(http.MethodGet, issuePath, nil, reader, http.StatusNotFound)
	for _, path := range []string{"/draft", editorPath} {
		w := request(http.MethodGet, path, nil, reader, http.StatusForbidden)
		if strings.Contains(w.Body.String(), questionOriginal) {
			t.Fatal("reader saw an original submission")
		}
	}
	initialDraft := request(http.MethodGet, "/draft", nil, editor, http.StatusOK)
	oldReview := hidden(initialDraft, "review")
	edit := request(http.MethodGet, editorPath, nil, editor, http.StatusOK)
	if !strings.Contains(edit.Body.String(), questionOriginal) {
		t.Fatal("editor cannot see original question")
	}
	form := url.Values{
		"csrf_token": {hidden(edit, "csrf_token")}, "revision": {hidden(edit, "revision")},
		"heading": {"En gemensam fikapaus"}, "body": {"Bestam en tid tillsammans och bjud in alla."},
		"question": {redactedQuestion}, "signature": {"En fikasugen kollega"}, "signoff": {"Ta hand om varandra."},
	}
	form.Set("csrf_token", readerCSRF)
	request(http.MethodPost, editorPath+"/save", form, reader, http.StatusForbidden)
	form.Set("csrf_token", "invalid")
	request(http.MethodPost, editorPath+"/save", form, editor, http.StatusForbidden)
	form.Set("csrf_token", hidden(edit, "csrf_token"))
	request(http.MethodPost, editorPath+"/save", form, editor, http.StatusSeeOther)
	request(http.MethodPost, editorPath+"/save", form, editor, http.StatusConflict)
	updated, err := store.GetArticle(question.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision, err := strconv.Atoi(form.Get("revision"))
	if err != nil || updated.Revision != oldRevision+1 || updated.Headline != form.Get("heading") || updated.Body != form.Get("body") {
		t.Fatalf("save did not update exactly once: %+v, error %v", updated, err)
	}
	publishForm := url.Values{"csrf_token": {form.Get("csrf_token")}, "confirm": {"yes"}, "review": {oldReview}}
	request(http.MethodPost, publishPath, publishForm, editor, http.StatusConflict)
	stillDraft, err := store.Issue(draft.ID)
	if err != nil || stillDraft.PublishedAt != nil {
		t.Fatalf("stale review published issue: %+v, error %v", stillDraft, err)
	}
	currentDraft := request(http.MethodGet, "/draft", nil, editor, http.StatusOK)
	publishForm.Set("review", hidden(currentDraft, "review"))
	if publishForm.Get("review") == oldReview {
		t.Fatal("review did not change after saving")
	}
	publication := request(http.MethodPost, publishPath, publishForm, editor, http.StatusSeeOther)
	if publication.Header().Get("Location") != issuePath {
		t.Fatal("publication did not redirect to the published issue")
	}
	for _, path := range []string{"/", issuePath, "/archive"} {
		w := request(http.MethodGet, path, nil, reader, http.StatusOK)
		body := w.Body.String()
		for _, private := range []string{reportOriginal, questionOriginal, "Karin Hemligsson", "Uanonymous", "Originalbidrag", "/editor/article/"} {
			if strings.Contains(body, private) {
				t.Fatalf("published %s leaked %q", path, private)
			}
		}
		if path == "/archive" {
			if !strings.Contains(body, `href="`+issuePath+`"`) {
				t.Fatal("archive missing published issue")
			}
		} else if !strings.Contains(body, redactedQuestion) || !strings.Contains(body, form.Get("heading")) || !strings.Contains(body, form.Get("body")) || !strings.Contains(body, "Ett nytt trad") {
			t.Fatalf("published %s missing reviewed content", path)
		}
		request(http.MethodGet, path, nil, nil, http.StatusSeeOther)
	}
	form.Set("revision", strconv.Itoa(updated.Revision))
	request(http.MethodPost, editorPath+"/save", form, editor, http.StatusConflict)
	if err := store.SaveArticle(updated.ID, updated.Revision, kp.Generated{Headline: "Ny rubrik", Body: "Ny text", Question: redactedQuestion, Signature: "En kollega"}); !errors.Is(err, kp.ErrConflict) {
		t.Fatalf("published store save: %v", err)
	}
	if err := store.RemoveArticle(updated.ID, updated.Revision); !errors.Is(err, kp.ErrConflict) {
		t.Fatalf("published store removal: %v", err)
	}
	if _, err := store.DB.Exec("UPDATE kp_articles SET body = ? WHERE id = ?", "Otillaten andring", updated.ID); err == nil {
		t.Fatal("published database allowed direct editing")
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["/.well-known/openid-configuration"] != 1 || requests["/keys"] == 0 || requests["/token"] != 3 || len(tokens) != 0 {
		t.Fatalf("unexpected provider activity: %v, pending codes %d", requests, len(tokens))
	}
}
