// Package auth implements workspace-restricted Slack OpenID Connect sign-in.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	slackIssuer     = "https://slack.com"
	flowLifetime    = 10 * time.Minute
	sessionLifetime = 8 * time.Hour
	requestTimeout  = 10 * time.Second
	maxFormBytes    = 1 << 20
	maxPendingFlows = 4096
	maxLoginPeers   = 4096
	loginLimit      = 60
)

type Config struct {
	ClientID, ClientSecret, BaseURL, WorkspaceID, SessionSecret string
	EditorIDs                                                   []string
}

type User struct {
	ID, Name string
	Editor   bool
	CSRF     string
}

type userKey struct{}

func UserFrom(ctx context.Context) User {
	u, _ := ctx.Value(userKey{}).(User)
	return u
}

type Auth struct {
	cfg        Config
	oauth      oauth2.Config
	verifier   *oidc.IDTokenVerifier
	client     *http.Client
	secure     bool
	editors    map[string]bool
	now        func() time.Time
	mu         sync.Mutex
	pending    map[string]int64
	loginPeers map[string]loginAttempts
}

type loginAttempts struct {
	Count   int
	Expires int64
}

type flow struct {
	State   string `json:"state"`
	Nonce   string `json:"nonce"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

type session struct {
	ID        string `json:"uid"`
	Name      string `json:"name"`
	Workspace string `json:"team"`
	CSRF      string `json:"csrf"`
	Issued    int64  `json:"iat"`
	Expires   int64  `json:"exp"`
}

func New(ctx context.Context, cfg Config) (*Auth, error) {
	return newAuth(ctx, cfg, slackIssuer, &http.Client{Timeout: requestTimeout})
}

// Only package tests may replace the issuer; runtime always uses Slack discovery.
func newAuth(ctx context.Context, cfg Config, issuer string, client *http.Client) (*Auth, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.BaseURL, "#") || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return nil, errors.New("auth: invalid base URL")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("auth: HTTPS base URL required")
	}
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.WorkspaceID) == "" || len(cfg.SessionSecret) < 32 || strings.TrimSpace(cfg.SessionSecret) == "" {
		return nil, errors.New("auth: missing configuration or session secret shorter than 32 bytes")
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	cfg.EditorIDs = append([]string(nil), cfg.EditorIDs...)
	a := &Auth{cfg: cfg, client: client, secure: u.Scheme == "https", editors: make(map[string]bool), now: time.Now, pending: make(map[string]int64), loginPeers: make(map[string]loginAttempts)}
	for _, id := range cfg.EditorIDs {
		if id == "" || strings.TrimSpace(id) != id {
			return nil, errors.New("auth: invalid editor ID")
		}
		a.editors[id] = true
	}
	discoveryCtx, cancel := context.WithTimeout(oidc.ClientContext(ctx, client), requestTimeout)
	defer cancel()
	provider, err := oidc.NewProvider(discoveryCtx, issuer)
	if err != nil {
		return nil, errors.New("auth: provider discovery failed")
	}
	a.verifier = provider.VerifierContext(oidc.ClientContext(ctx, client), &oidc.Config{ClientID: cfg.ClientID, Now: func() time.Time { return a.now() }})
	a.oauth = oauth2.Config{
		ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		RedirectURL: cfg.BaseURL + "/auth/callback",
		Endpoint:    provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile"},
	}
	// Slack supports client_secret_post. Avoid retrying one-use authorization codes.
	a.oauth.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	return a, nil
}

func (a *Auth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.Handle("POST /auth/logout", a.Require(http.HandlerFunc(a.logout)))
}

func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var s session
		c, err := r.Cookie(a.cookieName("session"))
		if err != nil || !a.decode("session", c.Value, &s) || !a.validTimes(s.Issued, s.Expires, sessionLifetime) || s.Workspace != a.cfg.WorkspaceID || s.ID == "" || !validRandom(s.CSRF) {
			a.clearCookie(w, "session")
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		u := User{ID: s.ID, Name: s.Name, Editor: a.editors[s.ID], CSRF: s.CSRF}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}

func (a *Auth) RequireEditor(next http.Handler) http.Handler {
	return a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !UserFrom(r.Context()).Editor || !a.checkCSRF(w, r) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *Auth) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(maxFormBytes)
	} else {
		// ParseForm otherwise ignores URL-encoded bodies on DELETE and OPTIONS.
		formRequest := *r
		formRequest.Method = http.MethodPost
		err = formRequest.ParseForm()
		r.Form, r.PostForm = formRequest.Form, formRequest.PostForm
	}
	if err != nil {
		return false
	}
	// Query parameters are not accepted as CSRF credentials.
	values := r.PostForm["csrf_token"]
	return len(values) == 1 && equal(values[0], UserFrom(r.Context()).CSRF)
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	now := a.now().Unix()
	// Use the network peer only, never forwarded headers. A single reverse proxy
	// shares this allowance across all users behind it.
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	limited := err != nil || peer == ""
	if !limited {
		a.mu.Lock()
		for host, attempts := range a.loginPeers {
			if attempts.Expires <= now {
				delete(a.loginPeers, host)
			}
		}
		attempts, exists := a.loginPeers[peer]
		limited = attempts.Count >= loginLimit || (!exists && len(a.loginPeers) >= maxLoginPeers)
		if !limited {
			if !exists {
				attempts.Expires = now + int64(time.Minute/time.Second)
			}
			attempts.Count++
			a.loginPeers[peer] = attempts
		}
		a.mu.Unlock()
	}
	if limited {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	state, err := randomToken()
	if err != nil {
		http.Error(w, "Sign-in unavailable", http.StatusServiceUnavailable)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "Sign-in unavailable", http.StatusServiceUnavailable)
		return
	}
	f := flow{State: state, Nonce: nonce, Issued: now, Expires: now + int64(flowLifetime/time.Second)}
	a.mu.Lock()
	for s, exp := range a.pending {
		if exp <= now {
			delete(a.pending, s)
		}
	}
	full := len(a.pending) >= maxPendingFlows
	if !full {
		a.pending[state] = f.Expires
	}
	a.mu.Unlock()
	if full || !a.setCookie(w, "flow", f, f.Expires) {
		http.Error(w, "Sign-in unavailable", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("team", a.cfg.WorkspaceID)), http.StatusSeeOther)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	a.clearCookie(w, "flow")
	fail := func() { http.Error(w, "Sign-in failed", http.StatusUnauthorized) }
	var f flow
	c, err := r.Cookie(a.cookieName("flow"))
	if err != nil || !a.decode("flow", c.Value, &f) || !a.validTimes(f.Issued, f.Expires, flowLifetime) || !validRandom(f.State) || !validRandom(f.Nonce) {
		fail()
		return
	}
	// Consume before exchanging the code, including on failed callbacks.
	a.mu.Lock()
	exp, exists := a.pending[f.State]
	delete(a.pending, f.State)
	a.mu.Unlock()
	q := r.URL.Query()
	if !exists || exp != f.Expires || len(q["state"]) != 1 || !equal(q.Get("state"), f.State) || len(q["code"]) != 1 || q.Get("code") == "" || q.Has("error") {
		fail()
		return
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(r.Context(), a.client), requestTimeout)
	defer cancel()
	tok, err := a.oauth.Exchange(ctx, q.Get("code"))
	if err != nil {
		fail()
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		fail()
		return
	}
	idToken, err := a.verifier.Verify(ctx, raw)
	if err != nil || !equal(idToken.Nonce, f.Nonce) {
		fail()
		return
	}
	var claims struct {
		Workspace string `json:"https://slack.com/team_id"`
		ID        string `json:"https://slack.com/user_id"`
		Name      string `json:"name"`
	}
	if idToken.Claims(&claims) != nil || claims.Workspace != a.cfg.WorkspaceID || claims.ID == "" || claims.ID != idToken.Subject {
		fail()
		return
	}
	csrf, err := randomToken()
	if err != nil {
		fail()
		return
	}
	if claims.Name == "" {
		claims.Name = claims.ID
	}
	now := a.now().Unix()
	s := session{ID: claims.ID, Name: claims.Name, Workspace: claims.Workspace, CSRF: csrf, Issued: now, Expires: now + int64(sessionLifetime/time.Second)}
	if !a.setCookie(w, "session", s, s.Expires) {
		fail()
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(w, r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	a.clearCookie(w, "session")
	a.clearCookie(w, "flow")
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

func (a *Auth) validTimes(issued, expires int64, lifetime time.Duration) bool {
	now := a.now().Unix()
	return issued > 0 && issued <= now && expires > now && expires-issued == int64(lifetime/time.Second)
}

func (a *Auth) cookieName(kind string) string {
	if a.secure {
		return "__Host-newspaper-" + kind
	}
	return "newspaper-" + kind
}

func (a *Auth) setCookie(w http.ResponseWriter, kind string, value any, expires int64) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := a.mac(kind, payload)
	encoded := payload + "." + base64.RawURLEncoding.EncodeToString(mac)
	if len(encoded) > 3800 {
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(kind), Value: encoded, Path: "/", Secure: a.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Unix(expires, 0), MaxAge: int(expires - a.now().Unix())})
	return true
}

func (a *Auth) clearCookie(w http.ResponseWriter, kind string) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(kind), Path: "/", Secure: a.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (a *Auth) mac(kind, payload string) []byte {
	m := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	m.Write([]byte(kind + ":" + payload))
	return m.Sum(nil)
}

func (a *Auth) decode(kind, value string, dest any) bool {
	if len(value) > 3800 {
		return false
	}
	payload, signature, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, a.mac(kind, payload)) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	return err == nil && json.Unmarshal(data, dest) == nil
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func validRandom(s string) bool {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
