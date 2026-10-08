package kp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/auth"
)

type webTestAccess struct {
	reader, editor bool
}

func (a webTestAccess) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.reader {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a webTestAccess) RequireEditor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.editor {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func webTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func webTestMux(s *Store, access webTestAccess) *http.ServeMux {
	mux := http.NewServeMux()
	NewWeb(s, access).Register(mux)
	return mux
}

func webTestRequest(mux *http.ServeMux, method, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func webTestReady(t *testing.T, s *Store) *Article {
	t.Helper()
	a, err := s.AddSubmission("report", "author", "Skribent", "Original <script>alert(1)</script>", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != a.ID {
		t.Fatal("unexpected queue article")
	}
	err = s.CompleteArticle(a.ID, claimed.Revision, Generated{Headline: "Rubrik <script>alert(1)</script>", Body: "Första stycket.\n\nAndra <b>stycket</b>.", Signature: "En skribent"})
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestWebAccessGates(t *testing.T) {
	s := webTestStore(t)
	for _, path := range []string{"/", "/archive", "/issues/1"} {
		w := webTestRequest(webTestMux(s, webTestAccess{}), "GET", path, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s: missing private cache header", path)
		}
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/draft"}, {"GET", "/editor/article/1"},
		{"POST", "/editor/article/1/save"}, {"POST", "/editor/article/1/remove"},
		{"POST", "/editor/article/1/retry"}, {"POST", "/editor/issues/1/publish"},
	} {
		w := webTestRequest(webTestMux(s, webTestAccess{reader: true}), route.method, route.path, nil)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d", route.method, route.path, w.Code)
		}
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM kp_issues").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("denied requests created a draft")
	}
}

func TestWebReadersNeverSeeDraft(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	for _, path := range []string{"/", "/archive", "/issues/" + strconv.Itoa(a.IssueID)} {
		w := webTestRequest(mux, "GET", path, nil)
		want := http.StatusOK
		if strings.HasPrefix(path, "/issues/") {
			want = http.StatusNotFound
		}
		if w.Code != want {
			t.Errorf("%s: status %d, body %s", path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "Rubrik") || strings.Contains(w.Body.String(), "Original") {
			t.Errorf("draft leaked at %s", path)
		}
	}
	w := webTestRequest(mux, "GET", "/issues/9999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing issue status %d", w.Code)
	}
}

func TestWebPublicationAndEscaping(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/issues/" + strconv.Itoa(a.IssueID) + "/publish"
	for _, value := range []string{"", "no", "true"} {
		w := webTestRequest(mux, "POST", path, url.Values{"confirm": {value}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("confirmation %q: status %d", value, w.Code)
		}
		i, err := s.Issue(a.IssueID)
		if err != nil {
			t.Fatal(err)
		}
		if i.PublishedAt != nil {
			t.Fatal("published without confirmation")
		}
	}
	w := webTestRequest(mux, "POST", path, url.Values{"confirm": {"yes"}, "review": {ReviewKey(a.IssueID, []Article{*a})}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/issues/"+strconv.Itoa(a.IssueID) {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/", "/issues/" + strconv.Itoa(a.IssueID)} {
		w = webTestRequest(mux, "GET", path, nil)
		body := w.Body.String()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, body)
		}
		if strings.Contains(body, "<script>") || strings.Contains(body, "<b>") || strings.Contains(body, "Original") {
			t.Fatal("unsafe HTML or original in published page")
		}
		if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "Första stycket.\n\nAndra &lt;b&gt;") {
			t.Fatal("escaped paragraph text missing")
		}
		if strings.Contains(body, "/editor/article/") || strings.Contains(body, "/publish") {
			t.Fatal("published issue exposes mutation controls")
		}
	}
	w = webTestRequest(mux, "GET", "/archive", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/issues/"+strconv.Itoa(a.IssueID)) {
		t.Fatalf("archive: %d %s", w.Code, w.Body.String())
	}
}

func TestWebNewsAndQuestionSections(t *testing.T) {
	s := webTestStore(t)
	var issueID int
	for _, fixture := range []struct {
		kind string
		g    Generated
	}{
		{KindQuestion, Generated{Headline: "Luktfrågan", Question: "Hur tar jag upp lukten?", Signature: "En fattig och känslig näsa", Body: "Doftsvaret"}},
		{KindReport, Generated{Headline: "Veckans nyhet", Body: "Olle berättar om arbetet."}},
		{KindQuestion, Generated{Headline: "Fikafrågan", Question: "Vem tog sista kakan?", Signature: "En smulig detektiv", Body: "Fikasvaret"}},
	} {
		a, err := s.AddSubmission(fixture.kind, "source-id", "Avsändarnamn", "Privat original", "")
		if err != nil {
			t.Fatal(err)
		}
		claim, err := s.ClaimNext()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.CompleteArticle(a.ID, claim.Revision, fixture.g); err != nil {
			t.Fatal(err)
		}
		issueID = a.IssueID
	}
	// Deployed issues may still carry the old title; branding is presentation data.
	if _, err := s.DB.Exec("UPDATE kp_issues SET title = ? WHERE id = ?", "Kumpan-Posten", issueID); err != nil {
		t.Fatal(err)
	}
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	for _, path := range []string{"/draft", "/issues/" + strconv.Itoa(issueID)} {
		if strings.HasPrefix(path, "/issues/") {
			if _, err := s.Publish(issueID); err != nil {
				t.Fatal(err)
			}
		}
		w := webTestRequest(mux, "GET", path, nil)
		body := w.Body.String()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, body)
		}
		if strings.Contains(body, "Avsändarnamn") || strings.Contains(body, "Rapporterat av") || strings.Contains(body, "Bidrag från") {
			t.Fatal("source shown as sender or reporter")
		}
		if !strings.Contains(body, "Olle berättar om arbetet.") {
			t.Fatal("source mention in the story was removed")
		}
		previous := -1
		for _, text := range []string{"Veckans nyhet", "Inuti Kumpanernas kroppar och knoppar", "Luktfrågan", "Hur tar jag upp lukten?", "En fattig och känslig näsa", "Doftsvaret", "Fikafrågan", "En smulig detektiv", "Fikasvaret"} {
			position := strings.Index(body, text)
			if position <= previous {
				t.Fatalf("missing or incorrectly ordered %q", text)
			}
			previous = position
		}
		if strings.Count(body, "Inuti Kumpanernas kroppar och knoppar") != 1 {
			t.Fatal("question section must have one shared heading")
		}
		if strings.Contains(body, "Kumpan-Posten") || !strings.Contains(body, ">Kumpanposten</a>") {
			t.Fatal("incorrect newspaper name")
		}
	}
	archive := webTestRequest(mux, "GET", "/archive", nil)
	if archive.Code != http.StatusOK || strings.Contains(archive.Body.String(), "Kumpan-Posten") {
		t.Fatal("archive uses the old newspaper name")
	}
}

func TestWebQuestionSectionVisibility(t *testing.T) {
	for _, kind := range []string{KindReport, KindQuestion} {
		t.Run(kind, func(t *testing.T) {
			s := webTestStore(t)
			if _, err := s.AddSubmission(kind, "source-id", "Avsändarnamn", "Privat original", ""); err != nil {
				t.Fatal(err)
			}
			mux := webTestMux(s, webTestAccess{reader: true, editor: true})
			w := webTestRequest(mux, "GET", "/draft", nil)
			body := w.Body.String()
			if w.Code != http.StatusOK {
				t.Fatal(w.Code)
			}
			if strings.Contains(body, "Inuti Kumpanernas kroppar och knoppar") != (kind == KindQuestion) {
				t.Fatal("empty question section shown or pending question hidden")
			}
			if strings.Contains(body, "Avsändarnamn") || !strings.Contains(body, "Visa original") || !strings.Contains(body, "/remove") {
				t.Fatal("pending article leaks source or loses editorial controls")
			}
		})
	}
}

func TestWebRequiresCurrentReview(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/issues/" + strconv.Itoa(a.IssueID) + "/publish"
	form := url.Values{"confirm": {"yes"}}
	if w := webTestRequest(mux, "POST", path, form); w.Code != http.StatusConflict {
		t.Fatalf("missing review: %d", w.Code)
	}
	form.Set("review", ReviewKey(a.IssueID, []Article{*a}))
	if err := s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Ändrad rubrik", Body: "Ändrad text"}); err != nil {
		t.Fatal(err)
	}
	if w := webTestRequest(mux, "POST", path, form); w.Code != http.StatusConflict {
		t.Fatalf("stale review: %d", w.Code)
	}
	articles, err := s.Articles(a.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("review", ReviewKey(a.IssueID, articles))
	if w := webTestRequest(mux, "POST", path, form); w.Code != http.StatusSeeOther {
		t.Fatalf("current review: %d %s", w.Code, w.Body.String())
	}
}

func TestWebSaveRevisionAndPublishedGuard(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/article/" + strconv.Itoa(a.ID)
	w := webTestRequest(mux, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Original &lt;script&gt;") || !strings.Contains(w.Body.String(), `name="revision" value="`+strconv.Itoa(a.Revision)+`"`) {
		t.Fatalf("editor: %d %s", w.Code, w.Body.String())
	}
	form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "heading": {"Ny rubrik"}, "body": {"Ny text"}}
	w = webTestRequest(mux, "POST", path+"/save", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	w = webTestRequest(mux, "POST", path+"/save", form)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale save: %d", w.Code)
	}
	updated, err := s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Headline != "Ny rubrik" || updated.Revision != a.Revision+1 {
		t.Fatal("save did not update article exactly once")
	}
	if _, err = s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	form.Set("revision", strconv.Itoa(updated.Revision))
	for _, route := range []struct{ method, suffix string }{{"GET", ""}, {"POST", "/save"}, {"POST", "/remove"}, {"POST", "/retry"}} {
		w = webTestRequest(mux, route.method, path+route.suffix, form)
		if w.Code != http.StatusConflict {
			t.Errorf("published %s %s: %d", route.method, route.suffix, w.Code)
		}
	}
}

func TestWebValidationAndPendingRemoval(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/article/" + strconv.Itoa(a.ID) + "/save"
	for _, form := range []url.Values{
		{"heading": {"Rubrik"}, "body": {"Text"}},
		{"revision": {"invalid"}, "heading": {"Rubrik"}, "body": {"Text"}},
		{"revision": {strconv.Itoa(a.Revision)}, "heading": {" "}, "body": {"Behåll min text"}},
		{"revision": {strconv.Itoa(a.Revision)}, "heading": {strings.Repeat("x", 501)}, "body": {"Text"}},
	} {
		w := webTestRequest(mux, "POST", path, form)
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid save: %d %s", w.Code, w.Body.String())
		}
	}
	pending, err := s.AddSubmission("report", "u", "Namn", "Privat original", "")
	if err != nil {
		t.Fatal(err)
	}
	w := webTestRequest(mux, "GET", "/draft", nil)
	removePath := "/editor/article/" + strconv.Itoa(pending.ID) + "/remove"
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), removePath) || !strings.Contains(w.Body.String(), `name="csrf_token"`) {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	w = webTestRequest(mux, "POST", "/editor/issues/"+strconv.Itoa(pending.IssueID)+"/publish", url.Values{"confirm": {"yes"}})
	if w.Code != http.StatusConflict {
		t.Fatalf("pending publish: %d", w.Code)
	}
	w = webTestRequest(mux, "POST", removePath, url.Values{"revision": {strconv.Itoa(pending.Revision)}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	w = webTestRequest(mux, "GET", "/editor/article/"+strconv.Itoa(pending.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("removed article: %d", w.Code)
	}
}

func TestWebFailedRetryAndSafeErrors(t *testing.T) {
	s := webTestStore(t)
	a, err := s.AddSubmission("report", "u", "Namn", "Original", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE kp_articles SET status = 'failed', error = ? WHERE id = ?", "secret SQL failure", a.ID); err != nil {
		t.Fatal(err)
	}
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	w := webTestRequest(mux, "GET", "/draft", nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "secret SQL failure") || !strings.Contains(w.Body.String(), "/retry") {
		t.Fatalf("failed draft: %d %s", w.Code, w.Body.String())
	}
	path := "/editor/article/" + strconv.Itoa(a.ID) + "/retry"
	w = webTestRequest(mux, "POST", path, url.Values{"revision": {strconv.Itoa(claimed.Revision)}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	retried, err := s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != "pending" || retried.Revision != claimed.Revision+1 {
		t.Fatal("retry did not queue with a new revision")
	}
	w = webTestRequest(mux, "POST", path, url.Values{"revision": {strconv.Itoa(retried.Revision)}})
	if w.Code != http.StatusConflict {
		t.Fatalf("nonfailed retry: %d", w.Code)
	}
	s.DB.Close()
	w = webTestRequest(mux, "GET", "/archive", nil)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "database") || strings.Contains(w.Body.String(), "sql:") {
		t.Fatalf("internal error leaked: %d %s", w.Code, w.Body.String())
	}
}

func TestWebTemplateNavigationAndCSRF(t *testing.T) {
	web := NewWeb(nil, webTestAccess{})
	for _, editor := range []bool{false, true} {
		var buf bytes.Buffer
		page := webPage{Title: "Arkiv", User: auth.User{Name: "<script>name</script>", Editor: editor, CSRF: "token<&"}}
		if err := web.views.ExecuteTemplate(&buf, "archive", page); err != nil {
			t.Fatal(err)
		}
		body := buf.String()
		if strings.Contains(body, `href="/draft"`) != editor {
			t.Errorf("editor %v: wrong navigation", editor)
		}
		if !strings.Contains(body, `name="csrf_token" value="token&lt;&amp;"`) || !strings.Contains(body, `action="/auth/logout" method="post"`) {
			t.Fatal("logout form missing escaped CSRF token")
		}
		if strings.Contains(body, "<script>name") || !strings.Contains(body, "&lt;script&gt;name") {
			t.Fatal("username not escaped")
		}
	}
}
