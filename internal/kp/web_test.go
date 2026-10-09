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
				if err := s.CompleteImage(a.ID, job.Revision, storeTestPhoto()); err != nil {
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
	web.imagesEnabled = true
	mux := http.NewServeMux()
	web.Register(mux)
	path := "/editor/article/" + strconv.Itoa(a.ID)
	form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "image_query": {"office window"}}
	for _, query := range []string{"", strings.Repeat("a", 161)} {
		form.Set("image_query", query)
		if w := webTestRequest(mux, "POST", path+"/image", form); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid query: %d %s", w.Code, w.Body.String())
		}
	}
	form.Set("image_query", "office window")
	w := webTestRequest(mux, "POST", path+"/image", form)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != path {
		t.Fatalf("queue: %d %s", w.Code, w.Body.String())
	}
	queued, err := s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.ImageStatus != "pending" || queued.ImageQuery != "office window" || queued.Status != "ready" || queued.Revision != a.Revision+1 {
		t.Fatal("image queue altered text readiness or failed to update revision")
	}
	for _, suffix := range []string{"/image", "/image/remove"} {
		if w := webTestRequest(mux, "POST", path+suffix, form); w.Code != http.StatusConflict {
			t.Fatalf("stale %s: %d", suffix, w.Code)
		}
	}
	w = webTestRequest(mux, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), path+"/image/remove") || !strings.Contains(w.Body.String(), "Bilden väntar") || !strings.Contains(w.Body.String(), `name="image_query" value="office window"`) || !strings.Contains(w.Body.String(), `name="csrf_token"`) {
		t.Fatalf("pending editor: %d %s", w.Code, w.Body.String())
	}
	web.imagesEnabled = false
	form.Set("revision", strconv.Itoa(queued.Revision))
	w = webTestRequest(mux, "POST", path+"/image", form)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "UNSPLASH_ACCESS_KEY") {
		t.Fatalf("disabled image fetch: %d %s", w.Code, w.Body.String())
	}
	w = webTestRequest(mux, "GET", path, nil)
	if strings.Contains(w.Body.String(), `name="image_query"`) || !strings.Contains(w.Body.String(), path+"/image/remove") {
		t.Fatal("disabled fetch form shown or pending cancellation hidden")
	}
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
	if removed.ImageStatus != "removed" || removed.Photo != nil || removed.Revision != queued.Revision+1 {
		t.Fatal("GET requests requeued an opted-out image")
	}
}

func TestWebRetainsPhotoAndPublishedImage(t *testing.T) {
	s := webTestStore(t)
	a := webTestReady(t, s)
	if err := s.QueueImage(a.ID, a.Revision, "office desk"); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImage(a.ID, claim.Revision, webTestPhoto()); err != nil {
		t.Fatal(err)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	mux := webTestMux(s, webTestAccess{reader: true, editor: true})
	path := "/editor/article/" + strconv.Itoa(a.ID)
	form := url.Values{"revision": {strconv.Itoa(a.Revision)}, "heading": {"Edited headline"}, "body": {"Edited body"}, "image_query": {"forged replacement"}}
	w := webTestRequest(mux, "POST", path+"/save", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("text save: %d %s", w.Code, w.Body.String())
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Photo == nil || a.Photo.ID != webTestPhoto().ID || a.ImageQuery != "office desk" || a.ImageStatus != "ready" {
		t.Fatal("text save changed the photo or image query")
	}
	if err := s.QueueImage(a.ID, a.Revision, "new office desk"); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailImage(a.ID, claim.Revision); err != nil {
		t.Fatal(err)
	}
	w = webTestRequest(mux, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "photo-office") || !strings.Contains(w.Body.String(), "Bilden kunde inte hämtas") || !strings.Contains(w.Body.String(), path+"/image/remove") || strings.Contains(w.Body.String(), `fetchpriority="high"`) {
		t.Fatalf("failed replacement editor: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/issues/" + strconv.Itoa(a.IssueID)} {
		w := webTestRequest(mux, "GET", path, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "photo-office") || strings.Contains(w.Body.String(), "/editor/article/") || strings.Contains(w.Body.String(), "Bilden kunde inte hämtas") {
			t.Fatalf("published photo: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestWebEditorImageForms(t *testing.T) {
	web := NewWeb(nil, webTestAccess{})
	photo := webTestPhoto()
	for _, state := range []string{"pending", "processing", "failed", "ready", "removed"} {
		for _, enabled := range []bool{false, true} {
			var buf bytes.Buffer
			a := &Article{ID: 7, Revision: 9, Status: "ready", ImageStatus: state, ImageQuery: "office desk"}
			if state == "ready" {
				a.Photo = &photo
			}
			page := webPage{Article: a, ImagesEnabled: enabled, User: auth.User{CSRF: "token<&"}}
			if err := web.views.ExecuteTemplate(&buf, "editor", page); err != nil {
				t.Fatal(err)
			}
			body := buf.String()
			if strings.Contains(body, `name="image_query"`) != enabled || strings.Contains(body, `/7/image/remove`) != (state != "removed") {
				t.Fatalf("wrong controls for %s, enabled %v: %s", state, enabled, body)
			}
			// Every form, including logout, carries the escaped CSRF token.
			if strings.Count(body, "<form ") != strings.Count(body, `name="csrf_token" value="token&lt;&amp;"`) {
				t.Fatal("image form missing an escaped CSRF token")
			}
			if enabled && !strings.Contains(body, `maxlength="160" aria-describedby="image-hint"`) {
				t.Fatal("image query lacks length limit or associated hint")
			}
			if enabled && state == "ready" && !strings.Contains(body, "Hämta ny bild") {
				t.Fatal("existing photo does not offer replacement")
			}
			panel := strings.Index(body, `<section class="editor-images"`)
			textForm := strings.Index(body, `<form class="editor"`)
			if panel <= textForm || !strings.Contains(body[textForm:panel], "</form>") {
				t.Fatal("image panel is nested inside the text form")
			}
		}
	}
	var buf bytes.Buffer
	if err := web.views.ExecuteTemplate(&buf, "editor", webPage{Article: &Article{Status: "pending", ImageStatus: "pending"}, ImagesEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "editor-images") || strings.Contains(buf.String(), `name="image_query"`) {
		t.Fatal("unfinished article exposes image controls")
	}
}
