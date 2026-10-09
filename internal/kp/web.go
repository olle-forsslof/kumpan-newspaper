package kp

import (
	"bytes"
	"database/sql"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/auth"
)

//go:embed views/*.html
var webViews embed.FS

type Access interface {
	Require(http.Handler) http.Handler
	RequireEditor(http.Handler) http.Handler
}

type Web struct {
	store          *Store
	access         Access
	views          *template.Template
	imageDirectory string
}

type webPage struct {
	Title          string
	User           auth.User
	Issue          *Issue
	Issues         []Issue
	Articles       []Article
	News           []webArticle
	Questions      []webArticle
	Article        *Article
	CurrentArticle *Article
	Draft          bool
	Message        string
	Review         string
}

type webArticle struct {
	Article
	Draft         bool
	CSRF          string
	ImagePriority bool
	ImageSizes    string
}

type webPhoto struct {
	*Photo
	Priority bool
	Sizes    string
}

type webImage struct {
	*ImageAsset
	ArticleID int
	Priority  bool
	Sizes     string
}

func assetURL(id int, asset *ImageAsset, width int) string {
	if id < 1 || asset == nil || ValidateImageAsset(*asset) != nil {
		return ""
	}
	switch width {
	case 0, 400, 800, 1536:
		return "/images/" + strconv.Itoa(id) + "/" + asset.Name + "?w=" + strconv.Itoa(width)
	default:
		return ""
	}
}

func imageURL(photo *Photo, width int) string {
	if photo == nil || ValidatePhoto(*photo) != nil {
		return ""
	}
	u, err := url.Parse(photo.URL)
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Set("w", strconv.Itoa(width))
	query.Set("h", strconv.Itoa((width*2+1)/3))
	query.Set("fit", "crop")
	query.Set("auto", "format")
	query.Set("q", "80")
	u.RawQuery = query.Encode()
	return u.String()
}

func attributionURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "unsplash.com" || u.User != nil {
		return ""
	}
	query := u.Query()
	query.Set("utm_source", "kumpanposten")
	query.Set("utm_medium", "referral")
	u.RawQuery = query.Encode()
	return u.String()
}

func NewWeb(store *Store, access Access) *Web {
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		panic("Stockholm timezone unavailable")
	}
	views := template.Must(template.New("kp").Funcs(template.FuncMap{
		"assetURL": assetURL,
		"imageView": func(id int, asset *ImageAsset, priority bool, sizes string) *webImage {
			if assetURL(id, asset, 800) == "" {
				return nil
			}
			return &webImage{ImageAsset: asset, ArticleID: id, Priority: priority, Sizes: sizes}
		},
		"imageURL":       imageURL,
		"attributionURL": attributionURL,
		"photoView": func(photo *Photo, priority bool, sizes string) *webPhoto {
			if photo == nil || ValidatePhoto(*photo) != nil {
				return nil
			}
			return &webPhoto{Photo: photo, Priority: priority, Sizes: sizes}
		},
		"date": func(t time.Time) string { return t.In(stockholm).Format("2006-01-02") },
		"status": func(s string) string {
			switch s {
			case "ready":
				return "Klar för publicering"
			case "failed":
				return "Bearbetningen misslyckades"
			case "processing":
				return "Bearbetas"
			default:
				return "Väntar på bearbetning"
			}
		},
	}).ParseFS(webViews, "views/*.html"))
	return &Web{store: store, access: access, views: views}
}

func (web *Web) Register(mux *http.ServeMux) {
	reader := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, web.private(web.access.Require(web.private(handler))))
	}
	editor := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, web.private(web.access.RequireEditor(web.private(handler))))
	}
	reader("GET /{$}", web.home)
	reader("GET /archive", web.archive)
	reader("GET /issues/{id}", web.issue)
	reader("GET /images/{id}/{name}", web.image)
	editor("GET /draft", web.draft)
	editor("GET /editor/article/{id}", web.edit)
	editor("POST /editor/article/{id}/save", web.save)
	editor("POST /editor/article/{id}/remove", web.remove)
	editor("POST /editor/article/{id}/retry", web.retry)
	editor("POST /editor/article/{id}/image", web.queueImage)
	editor("POST /editor/article/{id}/image/remove", web.removeImage)
	editor("POST /editor/issues/{id}/publish", web.publish)
}

func (web *Web) private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		next.ServeHTTP(w, r)
	})
}

func (web *Web) render(w http.ResponseWriter, r *http.Request, status int, view string, page webPage) {
	page.User = auth.UserFrom(r.Context())
	for _, article := range page.Articles {
		item := webArticle{Article: article, Draft: page.Draft, CSRF: page.User.CSRF}
		if article.Kind == KindQuestion {
			page.Questions = append(page.Questions, item)
		} else {
			page.News = append(page.News, item)
		}
	}
	priorityAssigned := false
	for _, group := range [][]webArticle{page.News, page.Questions} {
		for i := range group {
			item := &group[i]
			item.ImageSizes = "(max-width: 48rem) 86vw, (max-width: 72rem) calc(43vw - 1rem), 29rem"
			if item.Kind != KindQuestion {
				item.ImageSizes = "(max-width: 48rem) 92vw, (max-width: 72rem) calc(46vw - 1rem), 32rem"
				if len(page.News) == 1 {
					item.ImageSizes = "(max-width: 72rem) 92vw, 66rem"
				}
			}
			hasImage := assetURL(item.ID, item.Image, 800) != "" || (item.Photo != nil && ValidatePhoto(*item.Photo) == nil)
			if !priorityAssigned && item.Status == "ready" && hasImage {
				item.ImagePriority = true
				priorityAssigned = true
			}
		}
	}
	var buf bytes.Buffer
	if err := web.views.ExecuteTemplate(&buf, view, page); err != nil {
		http.Error(w, "Sidan kunde inte visas. Försök igen senare.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (web *Web) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "Något gick fel. Försök igen senare."
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status, message = http.StatusNotFound, "Det du söker finns inte."
	case errors.Is(err, ErrConflict):
		status, message = http.StatusConflict, "Innehållet har ändrats eller publicerats. Öppna utkastet igen innan du fortsätter."
	case errors.Is(err, ErrNotReady):
		status, message = http.StatusConflict, "Numret är inte klart. Alla artiklar måste vara färdigbearbetade innan publicering."
	}
	web.render(w, r, status, "error", webPage{Title: "Det gick inte", Message: message})
}

func (web *Web) badRequest(w http.ResponseWriter, r *http.Request, message string) {
	web.render(w, r, http.StatusBadRequest, "error", webPage{Title: "Kontrollera uppgifterna", Message: message})
}

func (web *Web) pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		web.fail(w, r, sql.ErrNoRows)
		return 0, false
	}
	return id, true
}

func (web *Web) home(w http.ResponseWriter, r *http.Request) {
	issue, err := web.store.LatestPublished()
	if errors.Is(err, sql.ErrNoRows) || (err == nil && issue == nil) {
		web.render(w, r, http.StatusOK, "issue", webPage{Title: "Senaste numret"})
		return
	}
	web.showIssue(w, r, issue, err, false)
}

func (web *Web) archive(w http.ResponseWriter, r *http.Request) {
	issues, err := web.store.PublishedIssues()
	if err != nil {
		web.fail(w, r, err)
		return
	}
	web.render(w, r, http.StatusOK, "archive", webPage{Title: "Arkiv", Issues: issues})
}

func (web *Web) issue(w http.ResponseWriter, r *http.Request) {
	id, ok := web.pathID(w, r)
	if !ok {
		return
	}
	issue, err := web.store.Issue(id)
	if err == nil && (issue == nil || issue.PublishedAt == nil) {
		err = sql.ErrNoRows
	}
	web.showIssue(w, r, issue, err, false)
}

func (web *Web) draft(w http.ResponseWriter, r *http.Request) {
	issue, err := web.store.CurrentDraft()
	web.showIssue(w, r, issue, err, true)
}

func (web *Web) showIssue(w http.ResponseWriter, r *http.Request, issue *Issue, err error, draft bool) {
	if err != nil {
		web.fail(w, r, err)
		return
	}
	if issue == nil {
		web.fail(w, r, sql.ErrNoRows)
		return
	}
	articles, err := web.store.Articles(issue.ID)
	if err != nil {
		web.fail(w, r, err)
		return
	}
	title := "Nummer " + strconv.Itoa(issue.ID)
	if draft {
		title = "Veckans utkast"
	}
	web.render(w, r, http.StatusOK, "issue", webPage{Title: title, Issue: issue, Articles: articles, Draft: draft, Review: ReviewKey(issue.ID, articles)})
}

func (web *Web) editable(w http.ResponseWriter, r *http.Request) (*Article, bool) {
	id, ok := web.pathID(w, r)
	if !ok {
		return nil, false
	}
	article, err := web.store.GetArticle(id)
	if err != nil {
		web.fail(w, r, err)
		return nil, false
	}
	if article == nil || article.Status == "removed" {
		web.fail(w, r, sql.ErrNoRows)
		return nil, false
	}
	issue, err := web.store.Issue(article.IssueID)
	if err != nil {
		web.fail(w, r, err)
		return nil, false
	}
	if issue == nil {
		web.fail(w, r, sql.ErrNoRows)
		return nil, false
	}
	if issue.PublishedAt != nil {
		web.fail(w, r, ErrConflict)
		return nil, false
	}
	return article, true
}

func (web *Web) image(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	article, err := web.store.GetArticle(id)
	if err != nil || article == nil || article.Status == "removed" {
		http.NotFound(w, r)
		return
	}
	issue, err := web.store.Issue(article.IssueID)
	if err != nil || issue == nil {
		http.NotFound(w, r)
		return
	}
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if article.Image == nil || ValidateImageAsset(*article.Image) != nil || r.PathValue("name") != article.Image.Name {
			http.NotFound(w, r)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		width := 0
		if values, present := query["w"]; present {
			if len(values) != 1 {
				http.Error(w, "Ogiltig bildstorlek.", http.StatusBadRequest)
				return
			}
			switch values[0] {
			case "0", "400", "800", "1536":
				width, _ = strconv.Atoi(values[0])
			default:
				http.Error(w, "Ogiltig bildstorlek.", http.StatusBadRequest)
				return
			}
		}
		if err != nil {
			http.Error(w, "Ogiltig bildstorlek.", http.StatusBadRequest)
			return
		}
		file, err := OpenImageFile(web.imageDirectory, article.Image.Name, width)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		contentType := "image/jpeg"
		if strings.HasSuffix(article.Image.Name, ".png") {
			contentType = "image/png"
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, article.Image.Name, info.ModTime(), file)
	})
	if issue.PublishedAt == nil {
		web.access.RequireEditor(serve).ServeHTTP(w, r)
		return
	}
	serve.ServeHTTP(w, r)
}

func (web *Web) edit(w http.ResponseWriter, r *http.Request) {
	article, ok := web.editable(w, r)
	if !ok {
		return
	}
	web.render(w, r, http.StatusOK, "editor", webPage{Title: "Redigera artikel", Article: article})
}

func (web *Web) revision(w http.ResponseWriter, r *http.Request) (int, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		web.badRequest(w, r, "Formuläret kunde inte läsas. Kontrollera uppgifterna.")
		return 0, false
	}
	revision, err := strconv.Atoi(r.PostForm.Get("revision"))
	if err != nil || revision < 1 {
		web.badRequest(w, r, "Artikelns version saknas. Öppna utkastet igen.")
		return 0, false
	}
	return revision, true
}

func (web *Web) save(w http.ResponseWriter, r *http.Request) {
	article, ok := web.editable(w, r)
	if !ok {
		return
	}
	revision, ok := web.revision(w, r)
	if !ok {
		return
	}
	if article.Status != "ready" {
		web.fail(w, r, ErrConflict)
		return
	}
	g := Generated{Headline: r.PostForm.Get("heading"), Body: r.PostForm.Get("body"), Question: r.PostForm.Get("question"), Signature: r.PostForm.Get("signature"), Signoff: r.PostForm.Get("signoff")}
	if article.Revision != revision {
		web.editConflict(w, r, article, g)
		return
	}
	if err := ValidateGenerated(article.Kind, g); err != nil {
		article.Headline, article.Body, article.Question, article.Signature, article.Signoff = g.Headline, g.Body, g.Question, g.Signature, g.Signoff
		web.render(w, r, http.StatusBadRequest, "editor", webPage{Title: "Redigera artikel", Article: article, Message: "Fyll i rubrik och brödtext. Frågebidrag behöver även fråga och signatur. Rubrik, signatur och avslutning får vara högst 500 byte; brödtext och fråga högst 20 000 byte."})
		return
	}
	if err := web.store.SaveArticle(article.ID, revision, g); err != nil {
		if errors.Is(err, ErrConflict) {
			current, lookupErr := web.store.GetArticle(article.ID)
			if lookupErr == nil && current.Status == StatusReady {
				web.editConflict(w, r, current, g)
				return
			}
		}
		web.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/draft", http.StatusSeeOther)
}

func (web *Web) editConflict(w http.ResponseWriter, r *http.Request, article *Article, g Generated) {
	current := *article
	article.Headline, article.Body, article.Question, article.Signature, article.Signoff = g.Headline, g.Body, g.Question, g.Signature, g.Signoff
	web.render(w, r, http.StatusConflict, "editor", webPage{
		Title: "Granska dina ändringar", Article: article, CurrentArticle: &current,
		Message: "Artikeln eller dess bild har ändrats. Dina osparade ändringar finns kvar i formuläret. Jämför med den nuvarande texten innan du sparar igen.",
	})
}

func (web *Web) remove(w http.ResponseWriter, r *http.Request) {
	web.changeArticle(w, r, false)
}

func (web *Web) queueImage(w http.ResponseWriter, r *http.Request) {
	article, ok := web.editable(w, r)
	if !ok {
		return
	}
	revision, ok := web.revision(w, r)
	if !ok {
		return
	}
	if article.Status != "ready" || article.Revision != revision {
		web.fail(w, r, ErrConflict)
		return
	}
	prompt := strings.ReplaceAll(r.PostForm.Get("image_prompt"), "\r\n", "\n")
	if err := ValidateImagePrompt(prompt); err != nil {
		web.badRequest(w, r, "Beskriv bilden med en bildprompt, högst 3 000 byte.")
		return
	}
	if err := web.store.QueueImage(article.ID, revision, prompt); err != nil {
		web.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/editor/article/"+strconv.Itoa(article.ID), http.StatusSeeOther)
}

func (web *Web) removeImage(w http.ResponseWriter, r *http.Request) {
	article, ok := web.editable(w, r)
	if !ok {
		return
	}
	revision, ok := web.revision(w, r)
	if !ok {
		return
	}
	if article.Status != "ready" || article.Revision != revision {
		web.fail(w, r, ErrConflict)
		return
	}
	if err := web.store.RemoveImage(article.ID, revision); err != nil {
		web.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/editor/article/"+strconv.Itoa(article.ID), http.StatusSeeOther)
}

func (web *Web) retry(w http.ResponseWriter, r *http.Request) {
	web.changeArticle(w, r, true)
}

func (web *Web) changeArticle(w http.ResponseWriter, r *http.Request, retry bool) {
	article, ok := web.editable(w, r)
	if !ok {
		return
	}
	revision, ok := web.revision(w, r)
	if !ok {
		return
	}
	var err error
	if retry {
		if article.Status != "failed" {
			web.fail(w, r, ErrConflict)
			return
		}
		err = web.store.RetryArticle(article.ID, revision)
	} else {
		err = web.store.RemoveArticle(article.ID, revision)
	}
	if err != nil {
		web.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/draft", http.StatusSeeOther)
}

func (web *Web) publish(w http.ResponseWriter, r *http.Request) {
	id, ok := web.pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("confirm") != "yes" {
		web.badRequest(w, r, "Bekräfta att numret är färdigt och får publiceras.")
		return
	}
	issue, err := web.store.PublishReviewed(id, r.PostForm.Get("review"))
	if err != nil {
		web.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/issues/"+strconv.Itoa(issue.ID), http.StatusSeeOther)
}
