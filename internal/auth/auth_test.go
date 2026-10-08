package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	a         *Auth
	mux       *http.ServeMux
	provider  *httptest.Server
	key       *rsa.PrivateKey
	mu        sync.Mutex
	tokens    map[string]string
	exchanges int
}

func testConfig() Config {
	return Config{ClientID: "client", ClientSecret: "secret", BaseURL: "https://news.example", WorkspaceID: "T123", SessionSecret: strings.Repeat("s", 32), EditorIDs: []string{"U123"}}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: key, tokens: make(map[string]string)}
	f.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{
				"issuer": f.provider.URL, "authorization_endpoint": f.provider.URL + "/authorize",
				"token_endpoint": f.provider.URL + "/token", "jwks_uri": f.provider.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
				"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
			}}})
		case "/token":
			if r.FormValue("code") == "hang" {
				<-r.Context().Done()
				return
			}
			if r.Method != "POST" || r.ParseForm() != nil || r.PostForm.Get("client_id") != "client" || r.PostForm.Get("client_secret") != "secret" || r.PostForm.Get("redirect_uri") != "https://news.example/auth/callback" || r.PostForm.Get("grant_type") != "authorization_code" {
				http.Error(w, "bad exchange", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.exchanges++
			token, ok := f.tokens[r.PostForm.Get("code")]
			f.mu.Unlock()
			if !ok {
				http.Error(w, "sensitive provider error", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "never-store-access", "token_type": "Bearer", "id_token": token})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.provider.Close)
	f.a, err = newAuth(context.Background(), testConfig(), f.provider.URL, f.provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	f.a.now = func() time.Time { return time.Unix(1700000000, 0) }
	f.mux = http.NewServeMux()
	f.a.Register(f.mux)
	return f
}

func (f *fixture) begin(t *testing.T) (*http.Cookie, string, string) {
	t.Helper()
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, httptest.NewRequest("GET", "/auth/login?next=https://evil.example", nil))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d", w.Code)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("scope") != "openid profile" || q.Get("team") != "T123" || q.Get("redirect_uri") != "https://news.example/auth/callback" || !validRandom(q.Get("state")) || !validRandom(q.Get("nonce")) {
		t.Fatal("invalid authorization request")
	}
	c := responseCookie(t, w, f.a.cookieName("flow"))
	assertCookie(t, c, true, int(flowLifetime/time.Second))
	return c, q.Get("state"), q.Get("nonce")
}

func (f *fixture) token(t *testing.T, code, nonce string, modify func(map[string]any)) {
	t.Helper()
	claims := map[string]any{
		"iss": f.provider.URL, "aud": "client", "sub": "U123", "exp": f.a.now().Add(time.Hour).Unix(),
		"iat": f.a.now().Unix(), "nonce": nonce, "https://slack.com/team_id": "T123", "https://slack.com/user_id": "U123", "name": "Test Editor",
	}
	if modify != nil {
		modify(claims)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.tokens[code] = unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)
	f.mu.Unlock()
}

func (f *fixture) callback(c *http.Cookie, state, code string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/auth/callback?"+url.Values{"state": {state}, "code": {code}}.Encode(), nil)
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func (f *fixture) signIn(t *testing.T, id string) *http.Cookie {
	t.Helper()
	c, state, nonce := f.begin(t)
	f.token(t, state, nonce, func(claims map[string]any) { claims["sub"], claims["https://slack.com/user_id"] = id, id })
	w := f.callback(c, state, state)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("callback status = %d", w.Code)
	}
	return responseCookie(t, w, f.a.cookieName("session"))
}

func responseCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing %s cookie", name)
	return nil
}

func assertCookie(t *testing.T, c *http.Cookie, secure bool, age int) {
	t.Helper()
	if c.Secure != secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || c.Path != "/" || c.MaxAge != age || c.Expires.IsZero() {
		t.Fatalf("incorrect cookie attributes for %s", c.Name)
	}
	if secure && !strings.HasPrefix(c.Name, "__Host-") {
		t.Fatal("secure cookie missing host prefix")
	}
}

func TestSignInAndUserContext(t *testing.T) {
	f := newFixture(t)
	c := f.signIn(t, "U123")
	assertCookie(t, c, true, int(sessionLifetime/time.Second))
	var got User
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	f.a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = UserFrom(r.Context()) })).ServeHTTP(w, r)
	if got.ID != "U123" || got.Name != "Test Editor" || !got.Editor || !validRandom(got.CSRF) {
		t.Fatalf("unexpected authenticated user: ID=%q editor=%v", got.ID, got.Editor)
	}
	if UserFrom(context.Background()) != (User{}) {
		t.Fatal("unauthenticated context must return zero user")
	}
	var s session
	if !f.a.decode("session", c.Value, &s) || s.Workspace != "T123" || s.Expires-s.Issued != 28800 {
		t.Fatal("invalid session payload")
	}
	payload, _, _ := strings.Cut(c.Value, ".")
	data, _ := base64.RawURLEncoding.DecodeString(payload)
	if strings.Contains(string(data), "never-store-access") || strings.Contains(string(data), "id_token") || strings.Contains(string(data), "editor") {
		t.Fatal("session contains tokens or editor permission")
	}
}

func TestCallbackRejectsInvalidIDTokens(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name   string
		modify func(map[string]any)
	}{
		{"workspace", func(c map[string]any) { c["https://slack.com/team_id"] = "Tother" }},
		{"missing workspace", func(c map[string]any) { delete(c, "https://slack.com/team_id") }},
		{"nonce", func(c map[string]any) { c["nonce"] = "wrong" }},
		{"missing nonce", func(c map[string]any) { delete(c, "nonce") }},
		{"issuer", func(c map[string]any) { c["iss"] = "https://evil.example" }},
		{"audience", func(c map[string]any) { c["aud"] = "other-client" }},
		{"expiry", func(c map[string]any) { c["exp"] = 1 }},
		{"subject", func(c map[string]any) { c["sub"] = "Uother" }},
		{"missing user ID", func(c map[string]any) { delete(c, "https://slack.com/user_id") }},
		{"empty user ID", func(c map[string]any) { c["https://slack.com/user_id"], c["sub"] = "", "" }},
		{"wrong claim type", func(c map[string]any) { c["https://slack.com/team_id"] = 123 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, state, nonce := f.begin(t)
			f.token(t, state, nonce, tt.modify)
			assertFailedCallback(t, f, f.callback(c, state, state))
		})
	}
	t.Run("signature", func(t *testing.T) {
		c, state, nonce := f.begin(t)
		f.token(t, state, nonce, nil)
		f.mu.Lock()
		parts := strings.Split(f.tokens[state], ".")
		parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, 256))
		f.tokens[state] = strings.Join(parts, ".")
		f.mu.Unlock()
		assertFailedCallback(t, f, f.callback(c, state, state))
	})
	t.Run("missing ID token", func(t *testing.T) {
		c, state, _ := f.begin(t)
		f.mu.Lock()
		f.tokens[state] = ""
		f.mu.Unlock()
		assertFailedCallback(t, f, f.callback(c, state, state))
	})
}

func assertFailedCallback(t *testing.T, f *fixture, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusUnauthorized || w.Body.String() != "Sign-in failed\n" {
		t.Fatalf("unexpected failure status or unsafe error: %d", w.Code)
	}
	if responseCookie(t, w, f.a.cookieName("flow")).MaxAge != -1 {
		t.Fatal("callback must clear flow cookie")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == f.a.cookieName("session") && c.MaxAge > 0 {
			t.Fatal("failed callback set a session")
		}
	}
}

func TestStateFlowAndReplay(t *testing.T) {
	f := newFixture(t)
	c, state, nonce := f.begin(t)
	f.token(t, "good", nonce, nil)
	assertFailedCallback(t, f, f.callback(c, "wrong", "good"))
	assertFailedCallback(t, f, f.callback(c, state, "good"))
	if f.exchanges != 0 {
		t.Fatal("invalid or consumed state exchanged a token")
	}
	c, state, nonce = f.begin(t)
	f.token(t, "good", nonce, nil)
	if w := f.callback(c, state, "good"); w.Code != http.StatusSeeOther {
		t.Fatal("valid callback rejected")
	}
	assertFailedCallback(t, f, f.callback(c, state, "good"))
	if f.exchanges != 1 {
		t.Fatal("replay exchanged another token")
	}
	assertFailedCallback(t, f, f.callback(nil, state, "good"))
	c, state, _ = f.begin(t)
	c.Value += "tampered"
	assertFailedCallback(t, f, f.callback(c, state, "good"))
	c, state, _ = f.begin(t)
	originalNow := f.a.now
	now := originalNow()
	f.a.now = func() time.Time { return now.Add(flowLifetime) }
	assertFailedCallback(t, f, f.callback(c, state, "good"))
	f.a.now = originalNow
	c, state, _ = f.begin(t)
	assertFailedCallback(t, f, f.callback(c, state, "provider-error"))
}

func TestInvalidSessions(t *testing.T) {
	f := newFixture(t)
	good := f.signIn(t, "U123")
	var original session
	f.a.decode("session", good.Value, &original)
	tests := []struct {
		name   string
		cookie func() *http.Cookie
	}{
		{"missing", func() *http.Cookie { return nil }},
		{"forged", func() *http.Cookie { c := *good; c.Value += "x"; return &c }},
		{"malformed", func() *http.Cookie { c := *good; c.Value = "garbage"; return &c }},
		{"wrong workspace", func() *http.Cookie { s := original; s.Workspace = "Tother"; return signedSession(t, f.a, s) }},
		{"empty ID", func() *http.Cookie { s := original; s.ID = ""; return signedSession(t, f.a, s) }},
		{"empty CSRF", func() *http.Cookie { s := original; s.CSRF = ""; return signedSession(t, f.a, s) }},
		{"expired", func() *http.Cookie {
			s := original
			s.Issued -= 28800
			s.Expires -= 28800
			return signedSession(t, f.a, s)
		}},
		{"future", func() *http.Cookie {
			s := original
			s.Issued += 3600
			s.Expires += 3600
			return signedSession(t, f.a, s)
		}},
		{"overlong lifetime", func() *http.Cookie { s := original; s.Expires++; return signedSession(t, f.a, s) }},
		{"flow as session", func() *http.Cookie { c, _, _ := f.begin(t); c.Name = good.Name; return c }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?next=https://evil.example", nil)
			if c := tt.cookie(); c != nil {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			f.a.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid session reached downstream") })).ServeHTTP(w, r)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/auth/login" {
				t.Fatalf("invalid session status = %d", w.Code)
			}
		})
	}
}

func signedSession(t *testing.T, a *Auth, s session) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	if !a.setCookie(w, "session", s, s.Expires) {
		t.Fatal("signing test session failed")
	}
	return responseCookie(t, w, a.cookieName("session"))
}

func TestEditorAndCSRF(t *testing.T) {
	f := newFixture(t)
	editor := f.signIn(t, "U123")
	reader := f.signIn(t, "U456")
	var s session
	f.a.decode("session", editor.Value, &s)
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			for _, valid := range []bool{false, true} {
				body := url.Values{"title": {"article"}, "csrf_token": {"wrong"}}
				if valid {
					body.Set("csrf_token", s.CSRF)
				}
				r := httptest.NewRequest(method, "/edit", strings.NewReader(body.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(editor)
				w := httptest.NewRecorder()
				called := false
				f.a.RequireEditor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					if method != "GET" && method != "HEAD" {
						if r.ParseForm() != nil || r.PostForm.Get("title") != "article" {
							t.Error("downstream form was lost")
						}
					}
				})).ServeHTTP(w, r)
				allowed := valid || method == "GET" || method == "HEAD"
				if called != allowed || (!allowed && w.Code != http.StatusForbidden) {
					t.Fatalf("valid=%v status=%d downstream=%v", valid, w.Code, called)
				}
			}
		})
	}
	for _, cookie := range []*http.Cookie{reader, editor} {
		if cookie == editor {
			delete(f.a.editors, "U123")
		}
		r := httptest.NewRequest("GET", "/edit", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		f.a.RequireEditor(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("non-editor reached downstream") })).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("non-editor status = %d", w.Code)
		}
	}
}

func TestCSRFFailsClosedAndMultipart(t *testing.T) {
	f := newFixture(t)
	c := f.signIn(t, "U123")
	var s session
	f.a.decode("session", c.Value, &s)
	for _, body := range []string{"", "csrf_token=%zz", "csrf_token=" + s.CSRF + "&csrf_token=" + s.CSRF, "csrf_token=" + s.CSRF + "&data=" + strings.Repeat("x", maxFormBytes)} {
		r := httptest.NewRequest("POST", "/edit?csrf_token="+s.CSRF, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(c)
		w := httptest.NewRecorder()
		f.a.RequireEditor(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid CSRF reached downstream") })).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("invalid CSRF status = %d", w.Code)
		}
	}
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	m.WriteField("csrf_token", s.CSRF)
	m.WriteField("title", "article")
	m.Close()
	r := httptest.NewRequest("POST", "/edit", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.AddCookie(c)
	w := httptest.NewRecorder()
	f.a.RequireEditor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ParseForm() != nil || r.FormValue("title") != "article" {
			t.Error("multipart form not preserved")
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("multipart status = %d", w.Code)
	}
}

func TestLogoutRequiresPOSTAndCSRF(t *testing.T) {
	f := newFixture(t)
	c := f.signIn(t, "U456")
	var s session
	f.a.decode("session", c.Value, &s)
	for _, tt := range []struct {
		method, csrf string
		want         int
	}{
		{"GET", s.CSRF, http.StatusMethodNotAllowed},
		{"POST", "", http.StatusForbidden},
		{"POST", "wrong", http.StatusForbidden},
		{"POST", s.CSRF, http.StatusSeeOther},
	} {
		r := httptest.NewRequest(tt.method, "/auth/logout", strings.NewReader(url.Values{"csrf_token": {tt.csrf}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(c)
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("logout %s status = %d, want %d", tt.method, w.Code, tt.want)
		}
		if tt.want == http.StatusSeeOther {
			if responseCookie(t, w, c.Name).MaxAge != -1 || w.Header().Get("Location") != "/auth/login" {
				t.Fatal("logout did not clear session")
			}
		}
	}
}

func TestConfigValidation(t *testing.T) {
	for _, base := range []string{"", "/relative", "http://news.example", "https://:443", "https://user:pass@news.example", "https://news.example/path", "https://news.example?query=1", "https://news.example#fragment", "https://news.example#", "ftp://localhost"} {
		cfg := testConfig()
		cfg.BaseURL = base
		if _, err := New(context.Background(), cfg); err == nil {
			t.Errorf("accepted base URL %q", base)
		}
	}
	for _, field := range []string{"client", "secret", "workspace", "session", "editors"} {
		cfg := testConfig()
		switch field {
		case "client":
			cfg.ClientID = ""
		case "secret":
			cfg.ClientSecret = ""
		case "workspace":
			cfg.WorkspaceID = ""
		case "session":
			cfg.SessionSecret = strings.Repeat("x", 31)
		case "editors":
			cfg.EditorIDs = []string{""}
		}
		if _, err := New(context.Background(), cfg); err == nil {
			t.Errorf("accepted invalid %s", field)
		}
	}
}

func TestLoopbackCookiesAndDiscoveryFailures(t *testing.T) {
	f := newFixture(t)
	for _, base := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "https://localhost:8080"} {
		cfg := testConfig()
		cfg.BaseURL = base
		a, err := newAuth(context.Background(), cfg, f.provider.URL, f.provider.Client())
		if err != nil {
			t.Fatal(err)
		}
		a.now = f.a.now
		w := httptest.NewRecorder()
		a.login(w, httptest.NewRequest("GET", "/auth/login", nil))
		assertCookie(t, responseCookie(t, w, a.cookieName("flow")), strings.HasPrefix(base, "https:"), 600)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newAuth(ctx, testConfig(), f.provider.URL, f.provider.Client()); err == nil || err.Error() != "auth: provider discovery failed" {
		t.Fatal("canceled discovery did not fail safely")
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	start := time.Now()
	if _, err := newAuth(context.Background(), testConfig(), slow.URL, &http.Client{Timeout: 25 * time.Millisecond}); err == nil {
		t.Fatal("hanging discovery succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("discovery did not respect timeout")
	}
}

func TestFlowRegistryBoundedAndPruned(t *testing.T) {
	f := newFixture(t)
	now := f.a.now()
	f.a.now = func() time.Time { return now }
	for i := 0; i < maxPendingFlows; i++ {
		f.a.pending[fmt.Sprint(i)] = now.Add(flowLifetime).Unix()
	}
	w := httptest.NewRecorder()
	f.a.login(w, httptest.NewRequest("GET", "/auth/login", nil))
	if w.Code != http.StatusServiceUnavailable || len(f.a.pending) != maxPendingFlows {
		t.Fatal("unbounded flow registry")
	}
	now = now.Add(flowLifetime)
	f.begin(t)
	if len(f.a.pending) != 1 {
		t.Fatal("expired states not pruned")
	}
}

func TestConcurrentCallbackConsumesStateOnce(t *testing.T) {
	f := newFixture(t)
	c, state, nonce := f.begin(t)
	f.token(t, state, nonce, nil)
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.callback(c, state, state).Code
		}()
	}
	wg.Wait()
	close(results)
	successes, failures := 0, 0
	for code := range results {
		switch code {
		case http.StatusSeeOther:
			successes++
		case http.StatusUnauthorized:
			failures++
		default:
			t.Fatalf("unexpected callback status: %d", code)
		}
	}
	if successes != 1 || failures != 1 || f.exchanges != 1 {
		t.Fatal("concurrent callbacks did not consume state exactly once")
	}
}

func TestTokenExchangeTimeout(t *testing.T) {
	f := newFixture(t)
	c, state, _ := f.begin(t)
	client := *f.a.client
	client.Timeout = 25 * time.Millisecond
	f.a.client = &client
	start := time.Now()
	assertFailedCallback(t, f, f.callback(c, state, "hang"))
	if time.Since(start) > time.Second {
		t.Fatal("token exchange did not respect HTTP timeout")
	}
}

func TestLoginRateLimit(t *testing.T) {
	f := newFixture(t)
	now := f.a.now()
	f.a.now = func() time.Time { return now }
	request := func(peer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/auth/login", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", len(f.a.pending)))
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 60; i++ {
		if w := request(fmt.Sprintf("192.0.2.1:%d", 1000+i)); w.Code != http.StatusSeeOther {
			t.Fatalf("attempt %d status = %d", i+1, w.Code)
		}
	}
	assertRateLimited(t, request("192.0.2.1:9000"))
	if len(f.a.pending) != 60 || len(f.a.loginPeers) != 1 {
		t.Fatal("limited request allocated a flow or used forwarded headers or ports as peers")
	}
	if w := request("[2001:db8::1]:9000"); w.Code != http.StatusSeeOther {
		t.Fatalf("independent IPv6 peer status = %d", w.Code)
	}
	now = now.Add(time.Minute - time.Second)
	assertRateLimited(t, request("192.0.2.1:9000"))
	now = now.Add(time.Second)
	if w := request("192.0.2.1:9000"); w.Code != http.StatusSeeOther {
		t.Fatalf("new window status = %d", w.Code)
	}
	if len(f.a.loginPeers) != 1 || f.a.loginPeers["192.0.2.1"].Count != 1 {
		t.Fatal("expired peer counters were not pruned and reset")
	}
	for _, peer := range []string{"", "192.0.2.1", ":9000", "malformed"} {
		assertRateLimited(t, request(peer))
	}
	if len(f.a.pending) != 62 || len(f.a.loginPeers) != 1 {
		t.Fatal("malformed peers allocated flows or counters")
	}
}

func TestLoginRateLimitMapCap(t *testing.T) {
	f := newFixture(t)
	now := f.a.now()
	f.a.now = func() time.Time { return now }
	for i := 0; i < 4096; i++ {
		f.a.loginPeers[fmt.Sprintf("peer-%d", i)] = loginAttempts{Count: 60, Expires: now.Add(time.Minute).Unix()}
	}
	r := httptest.NewRequest("GET", "/auth/login", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		assertRateLimited(t, w)
	}
	r.RemoteAddr = "peer-0:1234"
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	assertRateLimited(t, w)
	if len(f.a.loginPeers) != 4096 || len(f.a.pending) != 0 {
		t.Fatal("full limiter map allowed bypass or allocated a flow")
	}
	now = now.Add(time.Minute)
	r.RemoteAddr = "192.0.2.1:1234"
	w = httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || len(f.a.loginPeers) != 1 || len(f.a.pending) != 1 {
		t.Fatal("full limiter map did not recover after expiry")
	}
}

func TestConcurrentLoginRateLimit(t *testing.T) {
	f := newFixture(t)
	results := make(chan int, 61)
	var wg sync.WaitGroup
	for i := 0; i < 61; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/auth/login", nil)
			w := httptest.NewRecorder()
			f.mux.ServeHTTP(w, r)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	allowed, limited := 0, 0
	for code := range results {
		switch code {
		case http.StatusSeeOther:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected login status = %d", code)
		}
	}
	if allowed != 60 || limited != 1 || len(f.a.pending) != 60 {
		t.Fatal("concurrent requests bypassed login rate limit")
	}
}

func assertRateLimited(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" || w.Body.String() != "Too many requests\n" || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" {
		t.Fatalf("invalid rate-limit response: status = %d", w.Code)
	}
}
