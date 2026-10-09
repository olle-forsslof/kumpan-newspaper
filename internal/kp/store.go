package kp

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"
)

const (
	KindReport       = "report"
	KindQuestion     = "question"
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"
	StatusRemoved    = "removed"
)

var (
	ErrConflict = errors.New("article or issue is frozen, stale, or in an invalid state")
	ErrNotReady = errors.New("issue is not ready to publish")
)

type Issue struct {
	ID          int
	Title       string
	CreatedAt   time.Time
	PublishedAt *time.Time
}

type Article struct {
	ID, IssueID, Revision                               int
	Kind, AuthorID, AuthorName, Original, Status        string
	Headline, Body, Question, Signature, Signoff, Error string
	CreatedAt                                           time.Time
	ImageQuery, ImageStatus                             string
	Photo                                               *Photo
}

type Generated struct {
	Headline, Body, Question, Signature, Signoff string
	ImageQuery                                   string `json:"image_query"`
}

type Store struct{ DB *sql.DB }

const schema = `
CREATE TABLE kp_issues (
 id INTEGER PRIMARY KEY,
 title TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 published_at DATETIME,
 notified_at DATETIME
);
CREATE UNIQUE INDEX kp_one_draft ON kp_issues ((1)) WHERE published_at IS NULL;
CREATE TABLE kp_articles (
 id INTEGER PRIMARY KEY,
 issue_id INTEGER NOT NULL REFERENCES kp_issues(id),
 revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
 kind TEXT NOT NULL CHECK (kind IN ('report','question')),
 author_id TEXT NOT NULL,
 author_name TEXT NOT NULL,
 original TEXT NOT NULL,
 request_key TEXT UNIQUE,
 status TEXT NOT NULL CHECK (status IN ('pending','processing','ready','failed','removed')),
 headline TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL DEFAULT '',
 question TEXT NOT NULL DEFAULT '',
 signature TEXT NOT NULL DEFAULT '',
 signoff TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 created_at DATETIME NOT NULL,
 CHECK (kind != 'question' OR (author_id = '' AND author_name = ''))
);
CREATE INDEX kp_article_queue ON kp_articles (status, id);
CREATE INDEX kp_issue_articles ON kp_articles (issue_id, id);
CREATE TABLE kp_reminders (key TEXT PRIMARY KEY, sent_at DATETIME NOT NULL);
CREATE TRIGGER kp_frozen_article_insert BEFORE INSERT ON kp_articles
WHEN EXISTS (SELECT 1 FROM kp_issues WHERE id = NEW.issue_id AND published_at IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'published issue is immutable'); END;
CREATE TRIGGER kp_frozen_article_update BEFORE UPDATE ON kp_articles
WHEN EXISTS (SELECT 1 FROM kp_issues WHERE id IN (OLD.issue_id, NEW.issue_id) AND published_at IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'published issue is immutable'); END;
CREATE TRIGGER kp_frozen_article_delete BEFORE DELETE ON kp_articles
WHEN EXISTS (SELECT 1 FROM kp_issues WHERE id = OLD.issue_id AND published_at IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'published issue is immutable'); END;
CREATE TRIGGER kp_frozen_issue_update BEFORE UPDATE ON kp_issues
WHEN OLD.published_at IS NOT NULL AND
 (NEW.id != OLD.id OR NEW.title != OLD.title OR NEW.created_at != OLD.created_at
  OR NEW.published_at IS NOT OLD.published_at)
BEGIN SELECT RAISE(ABORT, 'published issue is immutable'); END;
CREATE TRIGGER kp_frozen_issue_delete BEFORE DELETE ON kp_issues
WHEN OLD.published_at IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'published issue is immutable'); END;
PRAGMA user_version = 1;
`

// Open accepts a filesystem path, not an arbitrary SQLite DSN. Existing databases
// must belong exclusively to this schema; legacy data is never migrated.
func Open(path string) (*Store, error) {
	var dsn string
	if path == ":memory:" {
		dsn = ":memory:"
	} else {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dsn = (&url.URL{Scheme: "file", Path: absolute}).String()
	}
	// REPLACE deletes conflicting rows; recursive triggers keep those deletes
	// subject to the published-content freeze triggers too.
	db, err := sql.Open("sqlite3", dsn+"?_foreign_keys=on&_recursive_triggers=on&_busy_timeout=5000&_txlock=immediate&_loc=UTC")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{DB: db}
	if err = s.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) initialize() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != 1 && version != 2 {
		return fmt.Errorf("unsupported database version %d", version)
	}
	rows, err := tx.Query("SELECT name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT GLOB 'sqlite_*'")
	if err != nil {
		return err
	}
	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for name := range tables {
		if version == 0 || (name != "kp_issues" && name != "kp_articles" && name != "kp_reminders") {
			return fmt.Errorf("refusing legacy or unrelated database containing %q", name)
		}
	}
	if version == 0 {
		if _, err = tx.Exec(schema); err != nil {
			return err
		}
	} else if len(tables) != 3 {
		return errors.New("incomplete kp database schema")
	}
	for _, object := range []struct{ kind, name, table string }{
		{"index", "kp_one_draft", "kp_issues"},
		{"index", "kp_article_queue", "kp_articles"},
		{"index", "kp_issue_articles", "kp_articles"},
		{"trigger", "kp_frozen_article_insert", "kp_articles"},
		{"trigger", "kp_frozen_article_update", "kp_articles"},
		{"trigger", "kp_frozen_article_delete", "kp_articles"},
		{"trigger", "kp_frozen_issue_update", "kp_issues"},
		{"trigger", "kp_frozen_issue_delete", "kp_issues"},
	} {
		var exists bool
		if err = tx.QueryRow("SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = ? AND name = ? AND tbl_name = ?)", object.kind, object.name, object.table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("incomplete kp database schema: missing %s %q", object.kind, object.name)
		}
	}
	// Validate the deployed schema before migrating; never repair missing guards.
	for _, query := range []string{
		"SELECT id, title, created_at, published_at, notified_at FROM kp_issues LIMIT 0",
		"SELECT id, issue_id, revision, kind, author_id, author_name, original, request_key, status, headline, body, question, signature, signoff, error, created_at FROM kp_articles LIMIT 0",
		"SELECT key, sent_at FROM kp_reminders LIMIT 0",
	} {
		rows, err := tx.Query(query)
		if err != nil {
			return fmt.Errorf("incomplete kp database schema: %w", err)
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	if version < 2 {
		// ALTER defaults expose empty image fields without updating frozen rows.
		if _, err = tx.Exec(`ALTER TABLE kp_articles ADD COLUMN image_query TEXT NOT NULL DEFAULT '';
ALTER TABLE kp_articles ADD COLUMN image_status TEXT NOT NULL DEFAULT 'none'
 CHECK (image_status IN ('none','pending','processing','ready','failed','removed'));
ALTER TABLE kp_articles ADD COLUMN image_photo TEXT NOT NULL DEFAULT '';
PRAGMA user_version = 2;`); err != nil {
			return err
		}
	}
	imageRows, err := tx.Query("SELECT image_query, image_status, image_photo FROM kp_articles LIMIT 0")
	if err != nil {
		return fmt.Errorf("incomplete kp database image schema: %w", err)
	}
	if err = imageRows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.DB.Close() }

type scanner interface{ Scan(...any) error }

const issueColumns = "id, title, created_at, published_at"
const articleColumns = "id, issue_id, revision, kind, author_id, author_name, original, status, headline, body, question, signature, signoff, error, created_at, image_query, image_status, image_photo"

func scanIssue(row scanner) (*Issue, error) {
	i := new(Issue)
	var published sql.NullTime
	if err := row.Scan(&i.ID, &i.Title, &i.CreatedAt, &published); err != nil {
		return nil, err
	}
	i.CreatedAt = i.CreatedAt.UTC()
	if published.Valid {
		t := published.Time.UTC()
		i.PublishedAt = &t
	}
	return i, nil
}

func scanArticle(row scanner) (*Article, error) {
	a := new(Article)
	var photoJSON string
	err := row.Scan(&a.ID, &a.IssueID, &a.Revision, &a.Kind, &a.AuthorID, &a.AuthorName, &a.Original, &a.Status, &a.Headline, &a.Body, &a.Question, &a.Signature, &a.Signoff, &a.Error, &a.CreatedAt, &a.ImageQuery, &a.ImageStatus, &photoJSON)
	if err != nil {
		return nil, err
	}
	if photoJSON != "" {
		var photo Photo
		if json.Unmarshal([]byte(photoJSON), &photo) != nil || ValidatePhoto(photo) != nil {
			return nil, errors.New("invalid stored image metadata")
		}
		a.Photo = &photo
	}
	a.CreatedAt = a.CreatedAt.UTC()
	return a, nil
}

func currentDraft(tx *sql.Tx) (*Issue, error) {
	i, err := scanIssue(tx.QueryRow("SELECT " + issueColumns + " FROM kp_issues WHERE published_at IS NULL"))
	if !errors.Is(err, sql.ErrNoRows) {
		return i, err
	}
	result, err := tx.Exec("INSERT INTO kp_issues (title, created_at) VALUES (?, ?)", "Kumpanposten", time.Now().UTC())
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return scanIssue(tx.QueryRow("SELECT "+issueColumns+" FROM kp_issues WHERE id = ?", id))
}

func (s *Store) CurrentDraft() (*Issue, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	i, err := currentDraft(tx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return i, nil
}

// An empty request key disables deduplication. Nonempty keys are opaque and
// remain unique even after removal or publication of the original submission.
func (s *Store) AddSubmission(kind, authorID, authorName, text, requestKey string) (*Article, error) {
	text = strings.TrimSpace(text)
	if kind != KindReport && kind != KindQuestion {
		return nil, errors.New("invalid submission kind")
	}
	if text == "" || len(text) > 10000 {
		return nil, errors.New("submission must contain 1 to 10000 bytes")
	}
	if kind == KindQuestion {
		authorID, authorName = "", ""
	}
	if len(authorName) > 500 {
		return nil, errors.New("author name exceeds 500 bytes")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if requestKey != "" {
		a, lookupErr := scanArticle(tx.QueryRow("SELECT "+articleColumns+" FROM kp_articles WHERE request_key = ?", requestKey))
		if lookupErr == nil {
			return a, tx.Commit()
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return nil, lookupErr
		}
	}
	i, err := currentDraft(tx)
	if err != nil {
		return nil, err
	}
	var key any
	if requestKey != "" {
		key = requestKey
	}
	result, err := tx.Exec(`INSERT INTO kp_articles
 (issue_id, kind, author_id, author_name, original, request_key, status, created_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, i.ID, kind, authorID, authorName, text, key, StatusPending, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	a, err := scanArticle(tx.QueryRow("SELECT "+articleColumns+" FROM kp_articles WHERE id = ?", id))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) Issue(id int) (*Issue, error) {
	return scanIssue(s.DB.QueryRow("SELECT "+issueColumns+" FROM kp_issues WHERE id = ?", id))
}

func (s *Store) GetArticle(id int) (*Article, error) {
	return scanArticle(s.DB.QueryRow("SELECT "+articleColumns+" FROM kp_articles WHERE id = ?", id))
}

func (s *Store) Articles(issueID int) ([]Article, error) {
	rows, err := s.DB.Query("SELECT "+articleColumns+" FROM kp_articles WHERE issue_id = ? AND status != 'removed' ORDER BY id", issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles := []Article{}
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, *a)
	}
	return articles, rows.Err()
}

func (s *Store) PublishedIssues() ([]Issue, error) {
	rows, err := s.DB.Query("SELECT " + issueColumns + " FROM kp_issues WHERE published_at IS NOT NULL ORDER BY published_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	issues := []Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, *i)
	}
	return issues, rows.Err()
}

func (s *Store) LatestPublished() (*Issue, error) {
	return scanIssue(s.DB.QueryRow("SELECT " + issueColumns + " FROM kp_issues WHERE published_at IS NOT NULL ORDER BY published_at DESC, id DESC LIMIT 1"))
}

// Generated content is plain text. Rendering code must escape it, never trust
// it as HTML. Limits are bytes, matching the submission limit.
func ValidateGenerated(kind string, g Generated) error {
	if kind != KindReport && kind != KindQuestion {
		return errors.New("invalid article kind")
	}
	if strings.TrimSpace(g.Headline) == "" || strings.TrimSpace(g.Body) == "" {
		return errors.New("headline and body are required")
	}
	if kind == KindQuestion && (strings.TrimSpace(g.Question) == "" || strings.TrimSpace(g.Signature) == "") {
		return errors.New("question and signature are required")
	}
	if len(g.Headline) > 500 || len(g.Signature) > 500 || len(g.Signoff) > 500 || len(g.Body) > 20000 || len(g.Question) > 20000 {
		return errors.New("generated content exceeds length limits")
	}
	if strings.TrimSpace(g.ImageQuery) != "" {
		return ValidateImageQuery(g.ImageQuery)
	}
	return nil
}

func ValidateImageQuery(query string) error {
	if strings.TrimSpace(query) == "" || len(query) > 160 || len(strings.Fields(query)) > 12 || !utf8.ValidString(query) {
		return errors.New("image query must contain 1 to 160 bytes and at most 12 words")
	}
	for _, r := range query {
		if unicode.IsControl(r) || !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '-') {
			return errors.New("image query must contain only English words, numbers, spaces or hyphens")
		}
	}
	return nil
}

func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) writeGenerated(id, revision int, status string, g Generated) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	err = tx.QueryRow(`SELECT kind FROM kp_articles WHERE id = ? AND revision = ? AND status = ?
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, id, revision, status).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if err = ValidateGenerated(kind, g); err != nil {
		return err
	}
	err = affected(tx.Exec(`UPDATE kp_articles SET headline = ?, body = ?, question = ?, signature = ?, signoff = ?,
	status = 'ready', error = '', revision = revision + 1,
	image_status = CASE WHEN image_status = 'processing' THEN 'pending' ELSE image_status END
	WHERE id = ? AND revision = ?`,
		g.Headline, g.Body, g.Question, g.Signature, g.Signoff, id, revision))
	if err != nil {
		return err
	}
	if status == StatusProcessing {
		query := strings.TrimSpace(g.ImageQuery)
		imageStatus := "none"
		if query != "" {
			imageStatus = StatusPending
		}
		if err = affected(tx.Exec(`UPDATE kp_articles SET image_query = ?, image_status = ?, image_photo = ''
 WHERE id = ? AND revision = ?`, query, imageStatus, id, revision+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveArticle(id, revision int, g Generated) error {
	return s.writeGenerated(id, revision, StatusReady, g)
}

func (s *Store) CompleteArticle(id, revision int, g Generated) error {
	return s.writeGenerated(id, revision, StatusProcessing, g)
}

func (s *Store) RemoveArticle(id, revision int) error {
	return affected(s.DB.Exec(`UPDATE kp_articles SET status = 'removed', revision = revision + 1
 WHERE id = ? AND revision = ? AND status IN ('pending','processing','ready','failed')
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, id, revision))
}

func (s *Store) RetryArticle(id, revision int) error {
	return affected(s.DB.Exec(`UPDATE kp_articles SET status = 'pending', error = '', revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'failed'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, id, revision))
}

func (s *Store) ClaimNext() (*Article, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := scanArticle(tx.QueryRow("SELECT " + articleColumns + ` FROM kp_articles WHERE status = 'pending'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL) ORDER BY id LIMIT 1`))
	if err != nil {
		return nil, err
	}
	if err = affected(tx.Exec("UPDATE kp_articles SET status = 'processing', revision = revision + 1 WHERE id = ? AND revision = ? AND status = 'pending'", a.ID, a.Revision)); err != nil {
		return nil, err
	}
	a.Status = StatusProcessing
	a.Revision++
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) FailArticle(id, revision int) error {
	return affected(s.DB.Exec(`UPDATE kp_articles SET status = 'failed', error = ?, revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'processing'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, "Generation failed. Please retry.", id, revision))
}

func (s *Store) ClaimNextImage() (*Article, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := scanArticle(tx.QueryRow("SELECT " + articleColumns + ` FROM kp_articles
 WHERE status = 'ready' AND image_status = 'pending'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL) ORDER BY id LIMIT 1`))
	if err != nil {
		return nil, err
	}
	if err = affected(tx.Exec(`UPDATE kp_articles SET image_status = 'processing', revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'ready' AND image_status = 'pending'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, a.ID, a.Revision)); err != nil {
		return nil, err
	}
	a.ImageStatus = StatusProcessing
	a.Revision++
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) CompleteImage(id, revision int, photo Photo) error {
	if err := ValidatePhoto(photo); err != nil {
		return err
	}
	metadata, err := json.Marshal(photo)
	if err != nil {
		return err
	}
	return affected(s.DB.Exec(`UPDATE kp_articles SET image_photo = ?, image_status = 'ready', revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'ready' AND image_status = 'processing'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, string(metadata), id, revision))
}

func (s *Store) FailImage(id, revision int) error {
	return affected(s.DB.Exec(`UPDATE kp_articles SET image_status = 'failed', revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'ready' AND image_status = 'processing'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, id, revision))
}

func (s *Store) QueueImage(id, revision int, query string) error {
	if err := ValidateImageQuery(query); err != nil {
		return err
	}
	return affected(s.DB.Exec(`UPDATE kp_articles SET image_query = ?, image_status = 'pending', revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'ready'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, strings.TrimSpace(query), id, revision))
}

func (s *Store) RemoveImage(id, revision int) error {
	return affected(s.DB.Exec(`UPDATE kp_articles SET image_status = 'removed', image_photo = '', revision = revision + 1
 WHERE id = ? AND revision = ? AND status = 'ready'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`, id, revision))
}

// RecoverProcessing is for single-instance startup, before workers are started.
// Incrementing revisions invalidates callbacks from the abandoned attempts.
func (s *Store) RecoverProcessing() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE kp_articles SET status = 'pending', error = '', revision = revision + 1
 WHERE status = 'processing' AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE kp_articles SET image_status = 'pending', revision = revision + 1
 WHERE status = 'ready' AND image_status = 'processing'
 AND issue_id IN (SELECT id FROM kp_issues WHERE published_at IS NULL)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Publish(issueID int) (*Issue, error) {
	return s.publish(issueID, "")
}

// ReviewKey changes whenever the set or revision of visible articles changes.
func ReviewKey(issueID int, articles []Article) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "issue:%d\n", issueID)
	for _, article := range articles {
		fmt.Fprintf(hash, "%d:%d:%s\n", article.ID, article.Revision, article.Status)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (s *Store) PublishReviewed(issueID int, review string) (*Issue, error) {
	if review == "" {
		return nil, ErrConflict
	}
	return s.publish(issueID, review)
}

func (s *Store) publish(issueID int, review string) (*Issue, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	i, err := scanIssue(tx.QueryRow("SELECT "+issueColumns+" FROM kp_issues WHERE id = ?", issueID))
	if err != nil {
		return nil, err
	}
	if i.PublishedAt != nil {
		return i, tx.Commit()
	}
	rows, err := tx.Query("SELECT "+articleColumns+" FROM kp_articles WHERE issue_id = ? AND status != 'removed' ORDER BY id", issueID)
	if err != nil {
		return nil, err
	}
	count := 0
	var reviewed []Article
	for rows.Next() {
		a, scanErr := scanArticle(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		if a.Status != StatusReady || ValidateGenerated(a.Kind, Generated{Headline: a.Headline, Body: a.Body, Question: a.Question, Signature: a.Signature, Signoff: a.Signoff, ImageQuery: a.ImageQuery}) != nil {
			rows.Close()
			return nil, ErrNotReady
		}
		count++
		reviewed = append(reviewed, *a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrNotReady
	}
	if review != "" && review != ReviewKey(issueID, reviewed) {
		return nil, ErrConflict
	}
	now := time.Now().UTC()
	if err = affected(tx.Exec("UPDATE kp_issues SET published_at = ? WHERE id = ? AND published_at IS NULL", now, issueID)); err != nil {
		return nil, err
	}
	i.PublishedAt = &now
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Store) PendingNotification() (*Issue, error) {
	return scanIssue(s.DB.QueryRow("SELECT " + issueColumns + " FROM kp_issues WHERE published_at IS NOT NULL AND notified_at IS NULL ORDER BY published_at, id LIMIT 1"))
}

func (s *Store) MarkNotified(issueID int) error {
	return affected(s.DB.Exec("UPDATE kp_issues SET notified_at = COALESCE(notified_at, ?) WHERE id = ? AND published_at IS NOT NULL", time.Now().UTC(), issueID))
}

func (s *Store) ReminderSent(key string) (bool, error) {
	var found bool
	err := s.DB.QueryRow("SELECT EXISTS (SELECT 1 FROM kp_reminders WHERE key = ?)", key).Scan(&found)
	return found, err
}

func (s *Store) MarkReminderSent(key string) error {
	_, err := s.DB.Exec("INSERT INTO kp_reminders (key, sent_at) VALUES (?, ?) ON CONFLICT(key) DO NOTHING", key, time.Now().UTC())
	return err
}
