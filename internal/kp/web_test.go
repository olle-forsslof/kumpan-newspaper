package kp

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	for _, path := range []string{"/", "/archive", "/issues/1", "/images/1/0123456789abcdef0123456789abcdef.png"} {
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
		{"POST", "/editor/article/1/image"}, {"POST", "/editor/article/1/image/remove"},
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
		for _, text := range []string{"Veckans nyhet", "Kropp & Knopp", "Luktfrågan", "Hur tar jag upp lukten?", "En fattig och känslig näsa", "Doftsvaret", "Fikafrågan", "En smulig detektiv", "Fikasvaret"} {
			position := strings.Index(body, text)
			if position <= previous {
				t.Fatalf("missing or incorrectly ordered %q", text)
			}
			previous = position
		}
		if strings.Count(body, "Kropp & Knopp") != 1 {
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
			if strings.Contains(body, "Kropp & Knopp") != (kind == KindQuestion) {
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
	if err := s.QueueImage(a.ID, updated.Revision, "An office scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	updated = storeTestGet(t, s, a.ID)
	if _, err = s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	for _, callback := range []func() error{
		func() error { return s.CompleteImage(a.ID, job.ImageRevision, storeTestImage()) },
		func() error { return s.FailImage(a.ID, job.ImageRevision) },
	} {
		if err := callback(); !errors.Is(err, ErrConflict) {
			t.Fatalf("published image callback with valid claim: %v", err)
		}
		current := storeTestGet(t, s, a.ID)
		if current.Revision != updated.Revision || current.ImageRevision != job.ImageRevision || current.ImageStatus != StatusProcessing || current.Image != nil {
			t.Fatal("published image callback changed the frozen article")
		}
	}
	form.Set("revision", strconv.Itoa(updated.Revision))
	for _, route := range []struct{ method, suffix string }{{"GET", ""}, {"POST", "/save"}, {"POST", "/remove"}, {"POST", "/retry"}, {"POST", "/image"}, {"POST", "/image/remove"}} {
		w = webTestRequest(mux, route.method, path+route.suffix, form)
		if w.Code != http.StatusConflict {
			t.Errorf("published %s %s: %d", route.method, route.suffix, w.Code)
		}
	}
}

func TestWebEditConflictPreservesSubmittedText(t *testing.T) {
	for _, change := range []string{"photo", "text"} {
		t.Run(change, func(t *testing.T) {
			s := webTestStore(t)
			a := webTestReady(t, s)
			if change == "photo" {
				if err := s.QueueImage(a.ID, a.Revision, "office desk"); err != nil {
					t.Fatal(err)
				}
				job, err := s.ClaimNextImage()
				if err != nil {
					t.Fatal(err)
				}
				if err := s.CompleteImage(a.ID, job.ImageRevision, storeTestImage()); err != nil {
					t.Fatal(err)
				}
			} else if err := s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Another editor", Body: "Current text"}); err != nil {
				t.Fatal(err)
			}
			mux := webTestMux(s, webTestAccess{reader: true, editor: true})
			form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "heading": {"My unsaved <script>headline</script>"}, "body": {"My unsaved text"}}
			path := "/editor/article/" + strconv.Itoa(a.ID) + "/save"
			w := webTestRequest(mux, "POST", path, form)
			current := storeTestGet(t, s, a.ID)
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "My unsaved &lt;script&gt;headline&lt;/script&gt;") || !strings.Contains(w.Body.String(), "My unsaved text") || !strings.Contains(w.Body.String(), "Nuvarande artikeltext") {
				t.Fatalf("edit lost on conflict: %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "<script>headline") || current.Body == "My unsaved text" {
				t.Fatal("conflict was unsafe or wrote stale content")
			}
			if !strings.Contains(w.Body.String(), `name="revision" value="`+strconv.Itoa(current.Revision)+`"`) {
				t.Fatal("reconciliation form uses stale revision")
			}
			form.Set("revision", strconv.Itoa(current.Revision))
			w = webTestRequest(mux, "POST", path, form)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("reconciled save: %d", w.Code)
			}
			if storeTestGet(t, s, a.ID).Body != "My unsaved text" {
				t.Fatal("reconciled text not saved")
			}
		})
	}
}

func TestWebTextSaveDuringImageGeneration(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	if err := s.QueueImage(a.ID, a.Revision, "An office scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/article/" + strconv.Itoa(a.ID) + "/save"
	form := url.Values{"revision": {strconv.Itoa(job.Revision)}, "heading": {"Saved during generation"}, "body": {"Updated article text"}}
	w := webTestRequest(mux, "POST", path, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("text save during generation: %d %s", w.Code, w.Body.String())
	}
	saved := storeTestGet(t, s, a.ID)
	if saved.Revision != job.Revision+1 || saved.ImageRevision != job.ImageRevision || saved.ImageStatus != StatusProcessing || saved.ImagePrompt != job.ImagePrompt || saved.Body != "Updated article text" {
		t.Fatalf("text save disturbed the paid image job: %+v", saved)
	}
	if err := s.CompleteImage(a.ID, job.ImageRevision, storeTestImage()); err != nil {
		t.Fatalf("image completion after text save: %v", err)
	}
	completed := storeTestGet(t, s, a.ID)
	if completed.Revision != saved.Revision+1 || completed.ImageStatus != StatusReady || completed.Image == nil || completed.Image.Name != storeTestImage().Name || completed.Headline != saved.Headline || completed.Body != saved.Body {
		t.Fatalf("image completion lost saved text or failed to update revision: %+v", completed)
	}
	form.Set("revision", strconv.Itoa(saved.Revision))
	form.Set("heading", "My unsaved <script>headline</script>")
	form.Set("body", "My next unsaved text")
	w = webTestRequest(mux, "POST", path, form)
	body := w.Body.String()
	if w.Code != http.StatusConflict || !strings.Contains(body, "My unsaved &lt;script&gt;headline&lt;/script&gt;") || !strings.Contains(body, "My next unsaved text") || !strings.Contains(body, "Nuvarande artikeltext") || !strings.Contains(body, saved.Body) || !strings.Contains(body, `name="revision" value="`+strconv.Itoa(completed.Revision)+`"`) {
		t.Fatalf("image completion conflict lost the proposed or current text: %d %s", w.Code, body)
	}
	if strings.Contains(body, "<script>headline") || storeTestGet(t, s, a.ID).Body != saved.Body {
		t.Fatal("stale editor save wrote or rendered unsafe proposed text")
	}
	form.Set("revision", strconv.Itoa(completed.Revision))
	w = webTestRequest(mux, "POST", path, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("reconciled save after image completion: %d %s", w.Code, w.Body.String())
	}
	reconciled := storeTestGet(t, s, a.ID)
	if reconciled.Body != "My next unsaved text" || reconciled.Image == nil || reconciled.Image.Name != completed.Image.Name || reconciled.ImageRevision != completed.ImageRevision {
		t.Fatal("reconciliation changed the completed image or lost proposed text")
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

func webTestPhoto() Photo {
	return Photo{
		ID: "office-photo", URL: "https://images.unsplash.com/photo-office?ixid=tracking%2Bvalue&ixlib=rb-4.1.0",
		Alt: "A desk & window", Photographer: "Alex & Sam",
		PhotographerURL: "https://unsplash.com/@alex?existing=a%26b",
		PageURL:         "https://unsplash.com/photos/office-photo",
		DownloadURL:     "https://api.unsplash.com/photos/office-photo/download",
		Width:           1800, Height: 1200,
	}
}

func TestWebPhotoURLs(t *testing.T) {
	photo := webTestPhoto()
	for _, width := range []int{400, 800, 1200} {
		u, err := url.Parse(imageURL(&photo, width))
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if u.Host != "images.unsplash.com" || u.Path != "/photo-office" || q.Get("w") != strconv.Itoa(width) || q.Get("h") != strconv.Itoa((width*2+1)/3) || q.Get("fit") != "crop" || q.Get("auto") != "format" || q.Get("q") != "80" || q.Get("ixid") != "tracking+value" || q.Get("ixlib") != "rb-4.1.0" {
			t.Fatalf("incorrect image URL: %s", u)
		}
	}
	u, err := url.Parse(attributionURL(photo.PhotographerURL))
	if err != nil || u.Query().Get("utm_source") != "kumpanposten" || u.Query().Get("utm_medium") != "referral" || u.Query().Get("existing") != "a&b" {
		t.Fatalf("incorrect attribution URL: %v %v", u, err)
	}
	for _, raw := range []string{"javascript:alert(1)", "https://evil.example/photo", "https://unsplash.com.evil.example/photo", "https://user@unsplash.com/photo", "http://unsplash.com/photo"} {
		if attributionURL(raw) != "" {
			t.Errorf("unsafe attribution accepted: %s", raw)
		}
		photo.URL = raw
		if imageURL(&photo, 800) != "" {
			t.Errorf("unsafe image accepted: %s", raw)
		}
	}
}

func TestWebPhotosDisplayOrderAndEscaping(t *testing.T) {
	web := NewWeb(nil, webTestAccess{})
	question, news := webTestPhoto(), webTestPhoto()
	question.URL = "https://images.unsplash.com/photo-question?ixid=question"
	question.Alt = `Window "<script>"`
	news.URL = "https://images.unsplash.com/photo-news?ixid=news"
	news.Photographer = "News photographer"
	news.PhotographerURL = "https://unsplash.com/@news"
	news.PageURL = "https://unsplash.com/photos/news-photo"
	articles := []Article{
		{ID: 1, Kind: KindQuestion, Status: "ready", Headline: "Question heading", Question: "Question text", Body: "Answer", Photo: &question, ImageStatus: "failed"},
		{ID: 2, Kind: KindReport, Status: "ready", Headline: "News heading", Body: "News text", Photo: &news, ImageStatus: "pending"},
	}
	w := httptest.NewRecorder()
	web.render(w, httptest.NewRequest("GET", "/draft", nil), http.StatusOK, "issue", webPage{Issue: &Issue{ID: 1}, Articles: articles, Draft: true})
	body := w.Body.String()
	if w.Code != http.StatusOK || strings.Count(body, `fetchpriority="high"`) != 1 || strings.Count(body, `loading="eager"`) != 1 || strings.Count(body, `loading="lazy"`) != 1 || strings.Count(body, "<figcaption>Foto:") != 2 {
		t.Fatalf("incorrect photo rendering: %d %s", w.Code, body)
	}
	newsImage, questionImage := strings.Index(body, "photo-news"), strings.Index(body, "photo-question")
	if newsImage < strings.Index(body, "<h2>News heading</h2>") || newsImage > strings.Index(body, "News text") || questionImage < strings.Index(body, "<h3>Question heading</h3>") || questionImage > strings.Index(body, "Question text") || newsImage > questionImage {
		t.Fatal("photo not displayed between headline and article text")
	}
	if !strings.Contains(body[newsImage:questionImage], `fetchpriority="high"`) || strings.Contains(body[questionImage:], `fetchpriority="high"`) {
		t.Fatal("question photo prioritized ahead of news")
	}
	for _, text := range []string{"400w,", "800w,", "1200w", `width="1200" height="800"`, "utm_source=kumpanposten", "utm_medium=referral", "Alex &amp; Sam", "&lt;script&gt;", "https://unsplash.com/@news?", "https://unsplash.com/photos/news-photo?", "https://unsplash.com/@alex?", "https://unsplash.com/photos/office-photo?"} {
		if !strings.Contains(body, text) {
			t.Errorf("missing %q", text)
		}
	}
	if strings.Contains(body, "Illustrationsbild") || strings.Contains(body, "<script>") || strings.Contains(body, "#ZgotmplZ") {
		t.Fatal("unexpected caption or unsafe HTML")
	}
	// Without a news photo, the first question photo becomes the only eager image.
	articles[1].Photo = nil
	w = httptest.NewRecorder()
	web.render(w, httptest.NewRequest("GET", "/draft", nil), http.StatusOK, "issue", webPage{Issue: &Issue{ID: 1}, Articles: articles, Draft: true})
	if strings.Count(w.Body.String(), `fetchpriority="high"`) != 1 || !strings.Contains(w.Body.String(), "photo-question") {
		t.Fatal("first question photo was not prioritized")
	}
	question.URL = "https://evil.example/photo"
	w = httptest.NewRecorder()
	web.render(w, httptest.NewRequest("GET", "/draft", nil), http.StatusOK, "issue", webPage{Issue: &Issue{ID: 1}, Articles: articles, Draft: true})
	if strings.Contains(w.Body.String(), "<img") || strings.Contains(w.Body.String(), "evil.example") {
		t.Fatal("invalid photo rendered")
	}
}

func TestWebImageQueueAndOptOut(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	web := NewWeb(s, webTestAccess{reader: true, editor: true})
	mux := http.NewServeMux()
	web.Register(mux)
	path := "/editor/article/" + strconv.Itoa(a.ID)
	if w := webTestRequest(mux, "POST", path+"/image", url.Values{"revision": {strconv.Itoa(a.Revision)}, "image_query": {"old search"}}); w.Code != http.StatusBadRequest {
		t.Fatalf("legacy search field queued an image: %d", w.Code)
	}
	form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "image_prompt": {"office window"}}
	for _, query := range []string{"", "   ", strings.Repeat("a", 3001), strings.Repeat("å", 1501), string([]byte{0xff})} {
		form.Set("image_prompt", query)
		if w := webTestRequest(mux, "POST", path+"/image", form); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid query: %d %s", w.Code, w.Body.String())
		}
	}
	form.Set("image_prompt", "office window")
	w := webTestRequest(mux, "POST", path+"/image", form)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != path {
		t.Fatalf("queue: %d %s", w.Code, w.Body.String())
	}
	queued, err := s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.ImageStatus != "pending" || queued.ImagePrompt != "office window" || queued.Status != "ready" || queued.Revision != a.Revision+1 {
		t.Fatal("image queue altered text readiness or failed to update revision")
	}
	for _, suffix := range []string{"/image", "/image/remove"} {
		if w := webTestRequest(mux, "POST", path+suffix, form); w.Code != http.StatusConflict {
			t.Fatalf("stale %s: %d", suffix, w.Code)
		}
	}
	w = webTestRequest(mux, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), path+"/image/remove") || !strings.Contains(w.Body.String(), "Bilden väntar") || !strings.Contains(w.Body.String(), `name="image_prompt"`) || !strings.Contains(w.Body.String(), ">office window</textarea>") || !strings.Contains(w.Body.String(), `name="csrf_token"`) {
		t.Fatalf("pending editor: %d %s", w.Code, w.Body.String())
	}
	form.Set("revision", strconv.Itoa(queued.Revision))
	w = webTestRequest(mux, "POST", path+"/image/remove", form)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != path {
		t.Fatalf("remove pending: %d %s", w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		webTestRequest(mux, "GET", path, nil)
		webTestRequest(mux, "GET", "/draft", nil)
	}
	removed, err := s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ImageStatus != "removed" || removed.Image != nil || removed.Photo != nil || removed.Revision != queued.Revision+1 {
		t.Fatal("GET requests requeued an opted-out image")
	}
}

func TestWebMultilineImagePrompt(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "image_prompt": {"A laptop on a desk.\r\nA bitten apple beside it."}}
	w := webTestRequest(mux, "POST", "/editor/article/"+strconv.Itoa(a.ID)+"/image", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("multiline prompt rejected: %d %s", w.Code, w.Body.String())
	}
	stored := storeTestGet(t, s, a.ID)
	if stored.ImagePrompt != "A laptop on a desk.\nA bitten apple beside it." {
		t.Fatal("form line endings were not normalized")
	}
}

func TestWebRetainsImageAndPublishedImage(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	if err := s.QueueImage(a.ID, a.Revision, "office desk"); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImage(a.ID, claim.ImageRevision, storeTestImage()); err != nil {
		t.Fatal(err)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/article/" + strconv.Itoa(a.ID)
	form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "heading": {"Edited headline"}, "body": {"Edited body"}, "image_prompt": {"forged replacement"}}
	w := webTestRequest(mux, "POST", path+"/save", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("text save: %d %s", w.Code, w.Body.String())
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Image == nil || a.Image.Name != storeTestImage().Name || a.ImagePrompt != "office desk" || a.ImageStatus != "ready" {
		t.Fatal("text save changed the image or image prompt")
	}
	if err := s.QueueImage(a.ID, a.Revision, "new office desk"); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailImage(a.ID, claim.ImageRevision); err != nil {
		t.Fatal(err)
	}
	w = webTestRequest(mux, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), storeTestImage().Name) || !strings.Contains(w.Body.String(), "Bilden kunde inte genereras") || !strings.Contains(w.Body.String(), path+"/image/remove") || strings.Contains(w.Body.String(), `fetchpriority="high"`) {
		t.Fatalf("failed replacement editor: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/issues/" + strconv.Itoa(a.IssueID)} {
		w := webTestRequest(mux, "GET", path, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), storeTestImage().Name) || strings.Contains(w.Body.String(), "/editor/article/") || strings.Contains(w.Body.String(), "Bilden kunde inte genereras") {
			t.Fatalf("published photo: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestWebEditorImageForms(t *testing.T) {
	web := NewWeb(nil, webTestAccess{})
	asset := storeTestImage()
	for _, state := range []string{"pending", "processing", "failed", "ready", "removed"} {
		for _, kind := range []string{KindReport, KindQuestion} {
			var buf bytes.Buffer
			a := &Article{ID: 7, Kind: kind, Revision: 9, Status: "ready", ImageStatus: state, ImagePrompt: "office <desk>"}
			if state == "ready" {
				a.Image = &asset
			}
			page := webPage{Article: a, User: auth.User{CSRF: "token<&"}}
			if err := web.views.ExecuteTemplate(&buf, "editor", page); err != nil {
				t.Fatal(err)
			}
			body := buf.String()
			if !strings.Contains(body, `name="image_prompt"`) || strings.Contains(body, `/7/image/remove`) != (state != "removed") {
				t.Fatalf("wrong controls for %s: %s", state, body)
			}
			// Every form, including logout, carries the escaped CSRF token.
			if strings.Count(body, "<form ") != strings.Count(body, `name="csrf_token" value="token&lt;&amp;"`) {
				t.Fatal("image form missing an escaped CSRF token")
			}
			if !strings.Contains(body, `maxlength="3000" aria-describedby="image-hint"`) || !strings.Contains(body, "office &lt;desk&gt;</textarea>") {
				t.Fatal("image prompt lacks length limit, hint or escaping")
			}
			if state == "ready" && !strings.Contains(body, "Generera ny bild") {
				t.Fatal("existing image does not offer replacement")
			}
			message := map[string]string{
				"pending": "Bilden väntar på att genereras.", "processing": "Bilden genereras.",
				"failed": "Bilden kunde inte genereras.", "removed": "Ingen bild genereras automatiskt.",
			}[state]
			if message != "" && !strings.Contains(body, message) {
				t.Fatalf("missing generation status for %s", state)
			}
			if strings.Contains(body, "svart tidningsteckning med genomskinlig bakgrund") != (kind == KindQuestion) || strings.Contains(body, "UNSPLASH_ACCESS_KEY") {
				t.Fatal("incorrect image style or disabled generation")
			}
			panel := strings.Index(body, `<section class="editor-images"`)
			textForm := strings.Index(body, `<form class="editor"`)
			if panel <= textForm || !strings.Contains(body[textForm:panel], "</form>") {
				t.Fatal("image panel is nested inside the text form")
			}
		}
	}
	var buf bytes.Buffer
	if err := web.views.ExecuteTemplate(&buf, "editor", webPage{Article: &Article{Status: "pending", ImageStatus: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "editor-images") || strings.Contains(buf.String(), `name="image_prompt"`) {
		t.Fatal("unfinished article exposes image controls")
	}
}

func TestWebAssetURLs(t *testing.T) {
	asset := storeTestImage()
	for _, width := range []int{0, 400, 800, 1536} {
		want := "/images/7/" + asset.Name + "?w=" + strconv.Itoa(width)
		if got := assetURL(7, &asset, width); got != want {
			t.Fatalf("asset URL = %q, want %q", got, want)
		}
	}
	if assetURL(0, &asset, 800) != "" || assetURL(7, nil, 800) != "" || assetURL(7, &asset, 1200) != "" {
		t.Fatal("invalid image URL accepted")
	}
	for _, name := range []string{"../secret.png", "https://evil.example/image.png", "data:image/png;base64,AA", "0123456789abcdef0123456789abcdef.svg"} {
		asset.Name = name
		if assetURL(7, &asset, 800) != "" {
			t.Fatalf("unsafe asset URL accepted: %s", name)
		}
	}
}

func TestWebGeneratedAndLegacyImageOrder(t *testing.T) {
	web := NewWeb(nil, webTestAccess{})
	asset, photo := storeTestImage(), webTestPhoto()
	asset.Name = "0123456789abcdef0123456789abcdef.png"
	asset.Height = 1024
	for _, generatedNews := range []bool{false, true} {
		news := Article{ID: 2, Kind: KindReport, Status: "ready", Headline: "News", Body: "News body", Photo: &photo}
		question := Article{ID: 1, Kind: KindQuestion, Status: "ready", Headline: "Question", Question: "Question text", Body: "Answer", Image: &asset, Photo: &photo}
		if generatedNews {
			news.Image = &asset
			question.Image = nil
		}
		w := httptest.NewRecorder()
		web.render(w, httptest.NewRequest("GET", "/draft", nil), http.StatusOK, "issue", webPage{Issue: &Issue{ID: 1}, Articles: []Article{question, news}, Draft: true})
		body := w.Body.String()
		if w.Code != http.StatusOK || strings.Count(body, "<img ") != 2 || strings.Count(body, "<figcaption>") != 1 || strings.Count(body, `fetchpriority="high"`) != 1 || strings.Count(body, `loading="lazy"`) != 1 {
			t.Fatalf("mixed images: %d %s", w.Code, body)
		}
		newsStart, questionStart := strings.Index(body, "<h2>News</h2>"), strings.Index(body, "<h3>Question</h3>")
		if newsStart < 0 || questionStart < newsStart || !strings.Contains(body[newsStart:questionStart], `fetchpriority="high"`) || strings.Contains(body[questionStart:], `fetchpriority="high"`) {
			t.Fatal("priority does not follow displayed order")
		}
		for _, text := range []string{`width="1536" height="1024" alt=""`, "1536w", "1200w", "Foto:", "utm_source=kumpanposten"} {
			if !strings.Contains(body, text) {
				t.Errorf("missing %q", text)
			}
		}
		if strings.Contains(body, "Illustrationsbild") || strings.Contains(body, "AI-generated") || strings.Contains(body, "#ZgotmplZ") {
			t.Fatal("generated image has extra caption or invalid URL")
		}
	}
}

func webTestCompleteImage(t *testing.T, s *Store, id int, asset ImageAsset) {
	t.Helper()
	a := storeTestGet(t, s, id)
	if err := s.QueueImage(id, a.Revision, "An office scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil || job.ID != id {
		t.Fatalf("claim image: %v %v", job, err)
	}
	if err := s.CompleteImage(id, job.ImageRevision, asset); err != nil {
		t.Fatal(err)
	}
}

func webTestImageFile(t *testing.T, directory, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	pixels := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	var err error
	if strings.HasSuffix(name, ".png") {
		err = png.Encode(&buf, pixels)
	} else {
		err = jpeg.Encode(&buf, pixels, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWebImageFileAccess(t *testing.T) {
	for _, extension := range []string{".jpg", ".png"} {
		t.Run(extension, func(t *testing.T) {
			s := webTestStore(t)
			a := webTestReady(t, s)
			asset := storeTestImage()
			asset.Name = strings.TrimSuffix(asset.Name, ".jpg") + extension
			webTestCompleteImage(t, s, a.ID, asset)
			directory := t.TempDir()
			contents := webTestImageFile(t, directory, asset.Name)
			for _, width := range []int{400, 800} {
				webTestImageFile(t, directory, imageVariantName(asset.Name, width))
			}
			path := "/images/" + strconv.Itoa(a.ID) + "/" + asset.Name
			muxFor := func(access webTestAccess) *http.ServeMux {
				web := NewWeb(s, access)
				web.imageDirectory = directory
				mux := http.NewServeMux()
				web.Register(mux)
				return mux
			}
			reader := muxFor(webTestAccess{reader: true})
			editor := muxFor(webTestAccess{reader: true, editor: true})
			if w := webTestRequest(muxFor(webTestAccess{}), "GET", path, nil); w.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated image: %d", w.Code)
			}
			// Draft authorization must run even when the associated file is missing.
			for _, dir := range []string{directory, filepath.Join(directory, "does-not-exist")} {
				web := NewWeb(s, webTestAccess{reader: true})
				web.imageDirectory = dir
				mux := http.NewServeMux()
				web.Register(mux)
				w := webTestRequest(mux, "GET", path, nil)
				if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), directory) {
					t.Fatalf("draft reader: %d %s", w.Code, w.Body.String())
				}
				if _, err := os.Stat(filepath.Join(directory, "does-not-exist")); !os.IsNotExist(err) {
					t.Fatal("image GET created a directory")
				}
			}
			for _, query := range []string{"", "?w=0", "?w=400", "?w=800", "?w=1536"} {
				w := webTestRequest(editor, "GET", path+query, nil)
				wantType := "image/jpeg"
				if extension == ".png" {
					wantType = "image/png"
				}
				if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), contents) || w.Header().Get("Content-Type") != wantType || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("image response: %d %v %s", w.Code, w.Header(), w.Body.String())
				}
			}
			for _, query := range []string{"?w=1200", "?w=-1", "?w=", "?w=0400", "?w=abc", "?w=400&w=800", "?w=400&w=400", "?w=%FF", "?w=400;w=800", "?w=%ZZ"} {
				if w := webTestRequest(editor, "GET", path+query, nil); w.Code != http.StatusBadRequest {
					t.Errorf("invalid width %s: %d", query, w.Code)
				}
			}
			other := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + extension
			webTestImageFile(t, directory, other)
			for _, badPath := range []string{
				"/images/" + strconv.Itoa(a.ID) + "/" + other,
				"/images/9999/" + asset.Name,
				"/images/0/" + asset.Name,
				"/images/" + strconv.Itoa(a.ID) + "/%2e%2e%2f" + asset.Name,
				"/images/" + strconv.Itoa(a.ID) + "/%2e%2e",
				"/images/" + strconv.Itoa(a.ID) + "/%252e%252e%252f" + asset.Name,
			} {
				w := webTestRequest(editor, "GET", badPath, nil)
				if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), directory) || strings.Contains(w.Body.String(), "sql:") {
					t.Errorf("unassociated image %s: %d %s", badPath, w.Code, w.Body.String())
				}
			}
			newAsset := asset
			newAsset.Name = other
			webTestCompleteImage(t, s, a.ID, newAsset)
			if w := webTestRequest(editor, "GET", path, nil); w.Code != http.StatusNotFound {
				t.Fatal("regenerated image still serves the old filename")
			}
			newPath := "/images/" + strconv.Itoa(a.ID) + "/" + other
			if _, err := s.Publish(a.IssueID); err != nil {
				t.Fatal(err)
			}
			if w := webTestRequest(reader, "GET", newPath, nil); w.Code != http.StatusOK {
				t.Fatalf("published image denied to workspace reader: %d", w.Code)
			}
		})
	}
}

func TestWebImageRemovedAndInvalidMetadata(t *testing.T) {
	for _, state := range []string{"image-removed", "article-removed", "invalid-asset", "invalid-photo"} {
		t.Run(state, func(t *testing.T) {
			s := webTestStore(t)
			a := webTestReady(t, s)
			asset := storeTestImage()
			webTestCompleteImage(t, s, a.ID, asset)
			directory := t.TempDir()
			webTestImageFile(t, directory, asset.Name)
			a = storeTestGet(t, s, a.ID)
			var err error
			switch state {
			case "image-removed":
				err = s.RemoveImage(a.ID, a.Revision)
			case "article-removed":
				err = s.RemoveArticle(a.ID, a.Revision)
			case "invalid-asset":
				_, err = s.DB.Exec("UPDATE kp_articles SET generated_image = ? WHERE id = ?", `{"Name":"../secret.jpg","Width":1536,"Height":1024}`, a.ID)
			case "invalid-photo":
				_, err = s.DB.Exec("UPDATE kp_articles SET image_photo = ? WHERE id = ?", `{"URL":"https://evil.example/photo"}`, a.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			web := NewWeb(s, webTestAccess{reader: true, editor: true})
			web.imageDirectory = directory
			mux := http.NewServeMux()
			web.Register(mux)
			w := webTestRequest(mux, "GET", "/images/"+strconv.Itoa(a.ID)+"/"+asset.Name, nil)
			if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), directory) || strings.Contains(w.Body.String(), "invalid stored") {
				t.Fatalf("invalid association: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestWebLegacyPhotoSurvivesFailedGeneration(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	photoJSON, err := json.Marshal(webTestPhoto())
	if err != nil {
		t.Fatal(err)
	}
	// Legacy photos are persisted data, not results from the new image pipeline.
	if _, err := s.DB.Exec("UPDATE kp_articles SET image_photo = ? WHERE id = ?", string(photoJSON), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueImage(a.ID, a.Revision, "A new office scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailImage(a.ID, job.ImageRevision); err != nil {
		t.Fatal(err)
	}
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/article/" + strconv.Itoa(a.ID)
	w := webTestRequest(mux, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "photo-office") || !strings.Contains(w.Body.String(), "Bilden kunde inte genereras") || !strings.Contains(w.Body.String(), "Generera ny bild") {
		t.Fatalf("legacy replacement editor: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	w = webTestRequest(mux, "GET", "/", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<figcaption>Foto:") || !strings.Contains(w.Body.String(), "utm_source=kumpanposten") || !strings.Contains(w.Body.String(), "photo-office") {
		t.Fatalf("published legacy attribution: %d %s", w.Code, w.Body.String())
	}
}

func TestWebGeneratedImageHasNoCaption(t *testing.T) {
	web := NewWeb(nil, webTestAccess{})
	asset := storeTestImage()
	asset.Width, asset.Height = 1024, 1536
	var buf bytes.Buffer
	view := webImage{ImageAsset: &asset, ArticleID: 7, Sizes: "92vw"}
	if err := web.views.ExecuteTemplate(&buf, "image", &view); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, `width="1024" height="1536" alt=""`) || !strings.Contains(body, "/images/7/"+asset.Name) || strings.Contains(body, "figcaption") || strings.Contains(body, "Unsplash") || strings.Contains(body, "Illustrationsbild") || strings.Contains(body, "AI-generated") {
		t.Fatalf("generated image metadata or caption: %s", body)
	}
}

func TestWebCartoonStylesPreserveTransparency(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source unavailable")
	}
	css, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../static/css/kp.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{".article-image {", ".article-image img {"} {
		_, rules, found := strings.Cut(string(css), selector)
		if !found {
			t.Fatalf("missing %s", selector)
		}
		rules, _, _ = strings.Cut(rules, "}")
		if !strings.Contains(rules, "background: transparent;") || strings.Contains(rules, "aspect-ratio:") || strings.Contains(rules, "cover") {
			t.Fatalf("generated image loses transparency or natural ratio: %s", rules)
		}
		if selector == ".article-image img {" && (!strings.Contains(rules, "object-fit: contain;") || !strings.Contains(rules, "height: auto;")) {
			t.Fatal("cartoon line art can be cropped")
		}
	}
}

func TestWebImageFileRejectsSymlinksAndNonRegularFiles(t *testing.T) {
	for _, state := range []string{"file-symlink", "directory-symlink", "non-regular"} {
		t.Run(state, func(t *testing.T) {
			s := webTestStore(t)
			a := webTestReady(t, s)
			asset := storeTestImage()
			webTestCompleteImage(t, s, a.ID, asset)
			directory := t.TempDir()
			var err error
			switch state {
			case "file-symlink":
				other := t.TempDir()
				webTestImageFile(t, other, asset.Name)
				err = os.Symlink(filepath.Join(other, asset.Name), filepath.Join(directory, asset.Name))
			case "directory-symlink":
				other := t.TempDir()
				webTestImageFile(t, other, asset.Name)
				directory = filepath.Join(directory, "linked")
				err = os.Symlink(other, directory)
			case "non-regular":
				err = os.Mkdir(filepath.Join(directory, asset.Name), 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			web := NewWeb(s, webTestAccess{reader: true, editor: true})
			web.imageDirectory = directory
			mux := http.NewServeMux()
			web.Register(mux)
			w := webTestRequest(mux, "GET", "/images/"+strconv.Itoa(a.ID)+"/"+asset.Name, nil)
			if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), directory) {
				t.Fatalf("unsafe image file served: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
