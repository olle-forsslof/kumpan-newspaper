package kp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func storeTestPhoto() Photo {
	return Photo{
		ID: "photo-id", URL: "https://images.unsplash.com/photo-test?w=1200",
		Alt: "Trees", Photographer: "Photographer",
		PhotographerURL: "https://unsplash.com/@photographer",
		PageURL:         "https://unsplash.com/photos/photo-id",
		DownloadURL:     "https://api.unsplash.com/photos/photo-id/download",
		Width:           1200, Height: 800,
	}
}

func storeTestImage() ImageAsset {
	return ImageAsset{Name: "0123456789abcdef0123456789abcdef.jpg", Width: 1536, Height: 1024}
}

func storeTestGet(t *testing.T, s *Store, id int) *Article {
	t.Helper()
	a, err := s.GetArticle(id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStoreImageMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO kp_issues (id, title, created_at) VALUES (7, 'Published', CURRENT_TIMESTAMP);
INSERT INTO kp_articles (id, issue_id, revision, kind, author_id, author_name, original, request_key, status, headline, body, created_at)
 VALUES (41, 7, 9, 'report', 'U1', 'Reporter', 'Original published', 'published', 'ready', 'Headline', 'Body', CURRENT_TIMESTAMP);
UPDATE kp_issues SET published_at = CURRENT_TIMESTAMP WHERE id = 7;
INSERT INTO kp_issues (id, title, created_at) VALUES (8, 'Draft', CURRENT_TIMESTAMP);
INSERT INTO kp_articles (id, issue_id, revision, kind, author_id, author_name, original, request_key, status, headline, body, created_at)
 VALUES (42, 8, 4, 'report', 'U2', 'Editor', 'Original draft', 'draft', 'ready', 'Draft headline', 'Draft body', CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	// Snapshot only v1 columns so the comparison includes all original content.
	const originalColumns = "id, issue_id, revision, kind, author_id, author_name, original, request_key, status, headline, body, question, signature, signoff, error, created_at"
	snapshot := func() []string {
		t.Helper()
		rows, err := db.Query("SELECT " + originalColumns + " FROM kp_articles ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var result []string
		for rows.Next() {
			values := make([]any, 16)
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			result = append(result, string(encoded))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	var publishedBefore string
	if err := db.QueryRow("SELECT CAST(published_at AS TEXT) FROM kp_issues WHERE id = 7").Scan(&publishedBefore); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []int{41, 42} {
			a := storeTestGet(t, s, id)
			if a.ImagePrompt != "" || a.ImageStatus != "none" || a.Photo != nil || a.Image != nil || a.ImageRevision != 1 {
				t.Fatalf("migration image defaults: %+v", a)
			}
		}
		if _, err := s.DB.Exec("UPDATE kp_articles SET image_status = 'removed' WHERE id = 41"); err == nil {
			t.Fatal("migration lost published freeze trigger")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
			t.Fatalf("migration version: %d, %v", version, err)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatalf("migration changed original rows: %v -> %v", before, after)
		}
		var publishedAfter string
		if err := db.QueryRow("SELECT CAST(published_at AS TEXT) FROM kp_issues WHERE id = 7").Scan(&publishedAfter); err != nil || publishedBefore != publishedAfter {
			t.Fatalf("migration changed publication: %q, %v", publishedAfter, err)
		}
	}
}

func TestStoreImageSchemaFailsClosed(t *testing.T) {
	for _, setup := range []string{
		"DROP TRIGGER kp_frozen_article_update",
		"PRAGMA user_version = 2",
	} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incomplete.sqlite")
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(schema + setup); err != nil {
				t.Fatal(err)
			}
			if s, err := Open(path); err == nil {
				s.Close()
				t.Fatal("opened incomplete schema")
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('kp_articles') WHERE name LIKE 'image_%'").Scan(&count); err != nil || count != 0 {
				t.Fatalf("refusal added image columns: %d, %v", count, err)
			}
			var exists bool
			if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE name = 'kp_frozen_article_update')").Scan(&exists); err != nil || exists != (setup == "PRAGMA user_version = 2") {
				t.Fatalf("refusal repaired trigger: %v, %v", exists, err)
			}
		})
	}
}

const storeTestSchemaV2 = `
ALTER TABLE kp_articles ADD COLUMN image_query TEXT NOT NULL DEFAULT '';
ALTER TABLE kp_articles ADD COLUMN image_status TEXT NOT NULL DEFAULT 'none'
 CHECK (image_status IN ('none','pending','processing','ready','failed','removed'));
ALTER TABLE kp_articles ADD COLUMN image_photo TEXT NOT NULL DEFAULT '';
PRAGMA user_version = 2;
`

func TestStoreV2MigrationPreservesHistoryAndCancelsOnlyDraftJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema + storeTestSchemaV2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kp_issues (id, title, created_at) VALUES (7, 'History', '2026-01-02 03:04:05');`); err != nil {
		t.Fatal(err)
	}
	photo := storeTestPhoto()
	metadata, err := json.Marshal(photo)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		id, issue                  int
		status, imageStatus, photo string
	}
	var fixtures []fixture
	for n, status := range []string{"none", StatusPending, StatusProcessing, StatusFailed, StatusReady, StatusRemoved} {
		fixtures = append(fixtures, fixture{41 + n, 7, StatusReady, status, string(metadata)})
	}
	for _, f := range fixtures {
		if _, err := db.Exec(`INSERT INTO kp_articles
 (id, issue_id, revision, kind, author_id, author_name, original, request_key, status, headline, body, question, signature, signoff, error, created_at, image_query, image_status, image_photo)
 VALUES (?, ?, 9, 'report', 'U1', 'Reporter', 'Original', ?, ?, 'Headline', 'Body', 'Question', 'Signature', 'Signoff', 'Error', '2026-01-02 03:04:05', 'old search terms', ?, ?)`, f.id, f.issue, f.id, f.status, f.imageStatus, f.photo); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE kp_issues SET published_at = '2026-01-03 04:05:06', notified_at = '2026-01-04 05:06:07' WHERE id = 7;
INSERT INTO kp_issues (id, title, created_at) VALUES (8, 'Draft', '2026-01-02 03:04:05');`); err != nil {
		t.Fatal(err)
	}
	for n, state := range []string{StatusPending, StatusProcessing, StatusFailed, StatusReady, StatusRemoved, "none"} {
		for p, legacy := range []string{"", string(metadata)} {
			f := fixture{100 + n*2 + p, 8, StatusReady, state, legacy}
			fixtures = append(fixtures, f)
		}
	}
	fixtures = append(fixtures, fixture{120, 8, StatusRemoved, StatusProcessing, string(metadata)}, fixture{121, 8, StatusPending, StatusPending, ""})
	for _, f := range fixtures[6:] {
		if _, err := db.Exec(`INSERT INTO kp_articles
 (id, issue_id, revision, kind, author_id, author_name, original, status, headline, body, created_at, image_query, image_status, image_photo)
 VALUES (?, ?, 9, 'report', 'U1', 'Reporter', 'Original', ?, 'Headline', 'Body', '2026-01-02 03:04:05', 'old search terms', ?, ?)`, f.id, f.issue, f.status, f.imageStatus, f.photo); err != nil {
			t.Fatal(err)
		}
	}
	// Compare every persisted v2 article and issue field, including timestamps.
	snapshot := func(query string) string {
		t.Helper()
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var records [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for n := range values {
				pointers[n] = &values[n]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			records = append(records, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	const publishedQuery = `SELECT id, issue_id, revision, kind, author_id, author_name, original, request_key, status, headline, body, question, signature, signoff, error, created_at, image_query, image_status, image_photo FROM kp_articles WHERE issue_id = 7 ORDER BY id`
	before := snapshot(publishedQuery)
	issuesBefore := snapshot("SELECT * FROM kp_issues ORDER BY id")
	for attempt := 0; attempt < 2; attempt++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fixtures {
			a := storeTestGet(t, s, f.id)
			wantStatus, wantRevision := f.imageStatus, 9
			if f.issue == 8 && f.status == StatusReady && (f.imageStatus == StatusPending || f.imageStatus == StatusProcessing || f.imageStatus == StatusFailed) {
				wantStatus, wantRevision = "none", 10
				if f.photo != "" {
					wantStatus = StatusReady
				}
			}
			if a.ID != f.id || a.IssueID != f.issue || a.Body != "Body" || a.Original != "Original" || a.CreatedAt.Format("2006-01-02 15:04:05") != "2026-01-02 03:04:05" || a.ImageStatus != wantStatus || a.Revision != wantRevision || a.ImageRevision != 1 || a.ImagePrompt != "" || a.Image != nil {
				t.Fatalf("migration article: %+v, fixture %+v", a, f)
			}
			if f.photo != "" && !reflect.DeepEqual(a.Photo, &photo) || f.photo == "" && a.Photo != nil {
				t.Fatalf("migration lost legacy photo: %+v", a)
			}
			var query, storedPhoto string
			if err := s.DB.QueryRow("SELECT image_query, image_photo FROM kp_articles WHERE id = ?", f.id).Scan(&query, &storedPhoto); err != nil || query != "old search terms" || storedPhoto != f.photo {
				t.Fatalf("migration changed legacy columns: %q, %q, %v", query, storedPhoto, err)
			}
		}
		if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("old searches queued paid work: %v", err)
		}
		if _, err := s.DB.Exec("UPDATE kp_articles SET generated_image = 'changed' WHERE id = 41"); err == nil {
			t.Fatal("migration lost publication freeze")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if snapshot(publishedQuery) != before || snapshot("SELECT * FROM kp_issues ORDER BY id") != issuesBefore {
			t.Fatal("migration changed historical article or issue metadata")
		}
	}
}

func TestStoreV3MigrationRollbackAndMissingColumns(t *testing.T) {
	for _, setup := range []string{
		"ALTER TABLE kp_articles ADD COLUMN image_prompt TEXT NOT NULL DEFAULT '';",
		"ALTER TABLE kp_articles ADD COLUMN generated_image TEXT NOT NULL DEFAULT '';",
		"ALTER TABLE kp_articles ADD COLUMN image_revision INTEGER NOT NULL DEFAULT 1;",
		"PRAGMA user_version = 3;",
		"ALTER TABLE kp_articles ADD COLUMN image_prompt TEXT NOT NULL DEFAULT ''; PRAGMA user_version = 3;",
		"ALTER TABLE kp_articles ADD COLUMN generated_image TEXT NOT NULL DEFAULT ''; PRAGMA user_version = 3;",
		"ALTER TABLE kp_articles ADD COLUMN image_prompt TEXT NOT NULL DEFAULT ''; ALTER TABLE kp_articles ADD COLUMN generated_image TEXT NOT NULL DEFAULT ''; PRAGMA user_version = 3;",
		"PRAGMA user_version = 4;",
	} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incomplete.sqlite")
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(schema + storeTestSchemaV2 + setup); err != nil {
				t.Fatal(err)
			}
			var beforeVersion, beforeColumns int
			if err := db.QueryRow("PRAGMA user_version").Scan(&beforeVersion); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('kp_articles')").Scan(&beforeColumns); err != nil {
				t.Fatal(err)
			}
			if s, err := Open(path); err == nil {
				s.Close()
				t.Fatal("opened invalid schema")
			}
			var version, columns int
			if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != beforeVersion {
				t.Fatalf("failed migration changed version: %d, %v", version, err)
			}
			if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('kp_articles')").Scan(&columns); err != nil || columns != beforeColumns {
				t.Fatalf("failed migration changed columns: %d, %v", columns, err)
			}
		})
	}
}

func TestStoreImagePromptValidation(t *testing.T) {
	for _, prompt := range []string{"", "   ", strings.Repeat("x", 3001), strings.Repeat("\u00e9", 1501), "trees\tforest", "trees\x00", "trees\u0085", "trees\xff"} {
		if err := ValidateImagePrompt(prompt); err == nil {
			t.Fatalf("accepted prompt %q", prompt)
		}
		if strings.TrimSpace(prompt) != "" && ValidateGenerated(KindReport, Generated{Headline: "H", Body: "B", ImagePrompt: prompt}) == nil {
			t.Fatalf("accepted generated prompt %q", prompt)
		}
	}
	for _, prompt := range []string{strings.Repeat("x", 3000), strings.Repeat("\u00e9", 1500), " trees\nforest ", "A scene: https://example.com, with colors!"} {
		if err := ValidateImagePrompt(prompt); err != nil {
			t.Fatalf("valid prompt %q: %v", prompt, err)
		}
	}
}

func TestStoreAutoImagePromptAndTextEdits(t *testing.T) {
	for _, kind := range []string{KindReport, KindQuestion} {
		t.Run(kind, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a, err := s.AddSubmission(kind, "U", "Name", "Original", "auto")
			if err != nil {
				t.Fatal(err)
			}
			if a.Photo != nil || a.ImageStatus != "none" {
				t.Fatalf("new article image: %+v", a)
			}
			job, err := s.ClaimNext()
			if err != nil {
				t.Fatal(err)
			}
			g := Generated{Headline: "H", Body: "B", Question: "Q", Signature: "S", ImagePrompt: " trees forest "}
			if err := s.CompleteArticle(job.ID, job.Revision, g); err != nil {
				t.Fatal(err)
			}
			a = storeTestGet(t, s, a.ID)
			if a.ImagePrompt != "trees forest" || a.ImageStatus != StatusPending || a.Image != nil {
				t.Fatalf("automatic query: %+v", a)
			}
			image, err := s.ClaimNextImage()
			if err != nil || image.Revision != a.Revision+1 || image.ImageRevision != a.ImageRevision+1 || image.ImageStatus != StatusProcessing {
				t.Fatalf("image claim: %+v, %v", image, err)
			}
			photo := storeTestImage()
			if err := s.CompleteImage(image.ID, image.ImageRevision, photo); err != nil {
				t.Fatal(err)
			}
			a = storeTestGet(t, s, a.ID)
			g.ImagePrompt = "different prompt"
			g.Body = "Edited body"
			if err := s.SaveArticle(a.ID, a.Revision, g); err != nil {
				t.Fatal(err)
			}
			edited := storeTestGet(t, s, a.ID)
			if edited.Body != g.Body || edited.ImagePrompt != a.ImagePrompt || edited.ImageStatus != StatusReady || edited.ImageRevision != a.ImageRevision || !reflect.DeepEqual(edited.Image, &photo) {
				t.Fatalf("text edit changed image: %+v", edited)
			}
			if err := s.SaveArticle(a.ID, a.Revision, g); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale editor: %v", err)
			}
		})
	}
}

func TestStoreImageReplacementRecoveryAndOptOut(t *testing.T) {
	s, path := storeTestOpen(t)
	a := storeTestReady(t, s, "image")
	if err := s.QueueImage(a.ID, a.Revision, " trees "); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	photo := storeTestImage()
	if err := s.CompleteImage(job.ID, job.ImageRevision, photo); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if err := s.QueueImage(a.ID, a.Revision, "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueImage(a.ID, a.Revision, "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale queue: %v", err)
	}
	job, err = s.ClaimNextImage()
	if err != nil || !reflect.DeepEqual(job.Image, &photo) {
		t.Fatalf("replacement dropped visible photo: %+v, %v", job, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	recovered := storeTestGet(t, s, a.ID)
	if recovered.ImageStatus != StatusFailed || recovered.Revision != job.Revision+1 || recovered.ImageRevision != job.ImageRevision+1 || !reflect.DeepEqual(recovered.Image, &photo) {
		t.Fatalf("recovery: %+v", recovered)
	}
	if err := s.CompleteImage(job.ID, job.ImageRevision, photo); !errors.Is(err, ErrConflict) {
		t.Fatalf("abandoned completion: %v", err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("abandoned failure: %v", err)
	}
	if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("recovery automatically retried paid work: %v", err)
	}
	if err := s.QueueImage(recovered.ID, recovered.Revision, recovered.ImagePrompt); err != nil {
		t.Fatal(err)
	}
	job, err = s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if job.ImageRevision != recovered.ImageRevision+2 {
		t.Fatalf("manual retry did not advance claim version: %+v", job)
	}
	if err := s.CompleteImage(job.ID, recovered.ImageRevision-1, photo); !errors.Is(err, ErrConflict) {
		t.Fatalf("new attempt accepted abandoned completion: %v", err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.ImageStatus != StatusFailed || a.Revision != job.Revision+1 || a.ImageRevision != job.ImageRevision+1 || !reflect.DeepEqual(a.Image, &photo) {
		t.Fatalf("failed replacement lost old photo: %+v", a)
	}
	if err := s.QueueImage(a.ID, a.Revision, "new replacement"); err != nil {
		t.Fatal(err)
	}
	job, err = s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveImage(job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImage(job.ID, job.ImageRevision, photo); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed image completion: %v", err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed image failure: %v", err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.ImageStatus != StatusRemoved || a.Photo != nil || a.Image != nil || a.ImagePrompt != "new replacement" {
		t.Fatalf("opt-out: %+v", a)
	}
	if err := s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Edited", Body: "Body", ImagePrompt: "automatic"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("opt-out recreated image job: %v", err)
	}
}

func TestStoreImageReviewAndPublicationFreeze(t *testing.T) {
	for _, status := range []string{StatusPending, StatusProcessing, StatusFailed} {
		t.Run(status, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestReady(t, s, "optional")
			review := ReviewKey(a.IssueID, []Article{*a})
			if err := s.QueueImage(a.ID, a.Revision, "trees"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PublishReviewed(a.IssueID, review); !errors.Is(err, ErrConflict) {
				t.Fatalf("image change accepted old review: %v", err)
			}
			if err := s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Stale", Body: "Stale"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("image change accepted stale text edit: %v", err)
			}
			if status != StatusPending {
				job, err := s.ClaimNextImage()
				if err != nil {
					t.Fatal(err)
				}
				if status == StatusFailed {
					if err := s.FailImage(job.ID, job.ImageRevision); err != nil {
						t.Fatal(err)
					}
				}
			}
			a = storeTestGet(t, s, a.ID)
			if _, err := s.PublishReviewed(a.IssueID, ReviewKey(a.IssueID, []Article{*a})); err != nil {
				t.Fatalf("optional image blocked publication: %v", err)
			}
			for _, mutate := range []func() error{
				func() error { return s.QueueImage(a.ID, a.Revision, "trees") },
				func() error { return s.RemoveImage(a.ID, a.Revision) },
				func() error { return s.CompleteImage(a.ID, a.ImageRevision, storeTestImage()) },
				func() error { return s.FailImage(a.ID, a.ImageRevision) },
			} {
				if err := mutate(); !errors.Is(err, ErrConflict) {
					t.Fatalf("published image mutation: %v", err)
				}
			}
			for _, column := range []string{"image_query", "image_prompt", "image_status", "image_photo", "generated_image"} {
				if _, err := s.DB.Exec("UPDATE kp_articles SET "+column+" = 'removed' WHERE id = ?", a.ID); err == nil {
					t.Fatalf("trigger permitted published %s change", column)
				}
			}
			if _, err := s.DB.Exec("UPDATE kp_articles SET image_revision = image_revision + 1 WHERE id = ?", a.ID); err == nil {
				t.Fatal("trigger permitted published image revision change")
			}
			if err := s.RecoverProcessing(); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("claimed published image: %v", err)
			}
			if after := storeTestGet(t, s, a.ID); !reflect.DeepEqual(a, after) {
				t.Fatalf("published image changed: %+v", after)
			}
		})
	}
}

func TestStoreImageRejectsUnsafeMetadataAndRemovedArticle(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "unsafe")
	if err := s.QueueImage(a.ID, a.Revision, "trees"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	for _, photo := range []ImageAsset{
		{Name: "../stolen.jpg", Width: 1536, Height: 1024},
		{Name: "https://example.com/stolen.jpg", Width: 1536, Height: 1024},
		{Name: storeTestImage().Name, Width: 0, Height: 1024},
		{Name: storeTestImage().Name, Width: 100000, Height: 1024},
		{Name: storeTestImage().Name, Width: 1536, Height: 100000},
	} {
		if err := s.CompleteImage(job.ID, job.ImageRevision, photo); err == nil {
			t.Fatalf("accepted unsafe asset %+v", photo)
		}
	}
	if after := storeTestGet(t, s, a.ID); !reflect.DeepEqual(job, after) {
		t.Fatalf("unsafe completion mutated image: %+v", after)
	}
	if err := s.RemoveArticle(job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	removed := storeTestGet(t, s, a.ID)
	if err := s.CompleteImage(removed.ID, removed.ImageRevision, storeTestImage()); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed article image completion: %v", err)
	}
	if err := s.FailImage(removed.ID, removed.ImageRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed article image failure: %v", err)
	}
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	if after := storeTestGet(t, s, a.ID); !reflect.DeepEqual(removed, after) {
		t.Fatalf("recovered removed article image: %+v", after)
	}
	for _, column := range []string{"image_photo", "generated_image"} {
		for _, metadata := range []string{"{", "null", `{"Name":"../stolen.jpg","URL":"javascript:alert(1)"}`} {
			if _, err := s.DB.Exec("UPDATE kp_articles SET "+column+" = ? WHERE id = ?", metadata, a.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetArticle(a.ID); err == nil || err.Error() != "invalid stored image metadata" {
				t.Fatalf("unsafe database metadata read: %v", err)
			}
		}
		if _, err := s.DB.Exec("UPDATE kp_articles SET "+column+" = '' WHERE id = ?", a.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreLegacyPhotoReplacement(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "legacy")
	photo := storeTestPhoto()
	metadata, err := json.Marshal(photo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE kp_articles SET image_photo = ?, image_query = 'historical search', image_status = 'ready' WHERE id = ?", string(metadata), a.ID); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if err := s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Edited", Body: "Edited body"}); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if !reflect.DeepEqual(a.Photo, &photo) {
		t.Fatal("text save lost legacy photo")
	}
	if err := s.QueueImage(a.ID, a.Revision, "A newly illustrated scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.ImageStatus != StatusFailed || !reflect.DeepEqual(a.Photo, &photo) || a.Image != nil {
		t.Fatalf("failed AI replacement lost legacy photo: %+v", a)
	}
	if err := s.QueueImage(a.ID, a.Revision, "Another illustrated scene"); err != nil {
		t.Fatal(err)
	}
	job, err = s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	asset := storeTestImage()
	if err := s.CompleteImage(job.ID, job.ImageRevision, asset); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.Photo != nil || !reflect.DeepEqual(a.Image, &asset) || a.ImageStatus != StatusReady {
		t.Fatalf("completion did not replace legacy photo: %+v", a)
	}
	var legacy, query, generated string
	if err := s.DB.QueryRow("SELECT image_photo, image_query, generated_image FROM kp_articles WHERE id = ?", a.ID).Scan(&legacy, &query, &generated); err != nil {
		t.Fatal(err)
	}
	var decoded ImageAsset
	if legacy != "" || query != "historical search" || json.Unmarshal([]byte(generated), &decoded) != nil || decoded != asset {
		t.Fatalf("stored replacement: %q, %q, %q", legacy, query, generated)
	}
	// Both historical and generated metadata may coexist in imported data.
	if _, err := s.DB.Exec("UPDATE kp_articles SET image_photo = ? WHERE id = ?", string(metadata), a.ID); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if !reflect.DeepEqual(a.Photo, &photo) || !reflect.DeepEqual(a.Image, &asset) {
		t.Fatal("reader discarded valid metadata")
	}
	if err := s.RemoveImage(a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.Photo != nil || a.Image != nil || a.ImagePrompt != "Another illustrated scene" {
		t.Fatalf("removal did not clear both assets: %+v", a)
	}
}

func TestStoreTextEditPreservesClaimedImage(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "edit")
	asset := storeTestImage()
	encoded, err := json.Marshal(asset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE kp_articles SET generated_image = ?, image_status = 'ready' WHERE id = ?", string(encoded), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueImage(a.ID, a.Revision, "An illustrated forest"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveArticle(job.ID, job.Revision, Generated{Headline: "Edited", Body: "Body", ImagePrompt: "Ignored"}); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.Revision != job.Revision+1 || a.ImageRevision != job.ImageRevision || a.ImageStatus != StatusProcessing || a.ImagePrompt != job.ImagePrompt || !reflect.DeepEqual(a.Image, &asset) {
		t.Fatalf("text edit changed image job: %+v", a)
	}
	if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("text edit queued another paid image: %v", err)
	}
	review := ReviewKey(a.IssueID, []Article{*a})
	asset.Name = "abcdef0123456789abcdef0123456789.png"
	if err := s.CompleteImage(job.ID, job.ImageRevision, asset); err != nil {
		t.Fatalf("text edit invalidated paid image completion: %v", err)
	}
	completed := storeTestGet(t, s, a.ID)
	if completed.Body != "Body" || completed.Headline != "Edited" || completed.Revision != a.Revision+1 || completed.ImageRevision != job.ImageRevision+1 || completed.ImageStatus != StatusReady || !reflect.DeepEqual(completed.Image, &asset) {
		t.Fatalf("completion lost text edit or metadata: %+v", completed)
	}
	if err := s.CompleteImage(job.ID, job.ImageRevision, asset); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted duplicate completion: %v", err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted failure after completion: %v", err)
	}
	if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("completed image queued duplicate work: %v", err)
	}
	if _, err := s.PublishReviewed(a.IssueID, review); !errors.Is(err, ErrConflict) {
		t.Fatalf("image completion accepted stale review: %v", err)
	}
}

func TestStoreImagePromptAndRemovalInvalidateClaims(t *testing.T) {
	for _, operation := range []string{"prompt", "remove"} {
		t.Run(operation, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestReady(t, s, "invalidate")
			if err := s.QueueImage(a.ID, a.Revision, "First scene"); err != nil {
				t.Fatal(err)
			}
			job, err := s.ClaimNextImage()
			if err != nil {
				t.Fatal(err)
			}
			if operation == "remove" {
				if err := s.RemoveImage(job.ID, job.Revision); err != nil {
					t.Fatal(err)
				}
				a = storeTestGet(t, s, job.ID)
				if a.ImageRevision != job.ImageRevision+1 || a.Revision != job.Revision+1 || a.ImageStatus != StatusRemoved {
					t.Fatalf("removal revisions: %+v", a)
				}
			} else {
				a = job
			}
			if err := s.QueueImage(a.ID, a.Revision, "Replacement scene"); err != nil {
				t.Fatal(err)
			}
			queued := storeTestGet(t, s, a.ID)
			if queued.ImageRevision != a.ImageRevision+1 || queued.Revision != a.Revision+1 {
				t.Fatalf("queue revisions: %+v", queued)
			}
			replacement, err := s.ClaimNextImage()
			if err != nil {
				t.Fatal(err)
			}
			if replacement.ImageRevision != queued.ImageRevision+1 || replacement.Revision != queued.Revision+1 {
				t.Fatalf("claim revisions: %+v", replacement)
			}
			if err := s.CompleteImage(job.ID, job.ImageRevision, storeTestImage()); !errors.Is(err, ErrConflict) {
				t.Fatalf("replacement accepted old completion: %v", err)
			}
			if err := s.FailImage(job.ID, job.ImageRevision); !errors.Is(err, ErrConflict) {
				t.Fatalf("replacement accepted old failure: %v", err)
			}
			if err := s.CompleteImage(replacement.ID, replacement.ImageRevision, storeTestImage()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoreTextEditPreservesImageFailureClaim(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "failure")
	if err := s.QueueImage(a.ID, a.Revision, "A scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveArticle(job.ID, job.Revision, Generated{Headline: "Edited", Body: "Edited body"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); err != nil {
		t.Fatalf("text edit invalidated failure callback: %v", err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.ImageStatus != StatusFailed || a.ImageRevision != job.ImageRevision+1 || a.Revision != job.Revision+2 || a.Body != "Edited body" {
		t.Fatalf("image failure after edit: %+v", a)
	}
	if err := s.FailImage(job.ID, job.ImageRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate failure accepted: %v", err)
	}
	if err := s.CompleteImage(job.ID, job.ImageRevision, storeTestImage()); !errors.Is(err, ErrConflict) {
		t.Fatalf("completion after failure accepted: %v", err)
	}
}

func TestStoreRecoveryLeavesUnattemptedImagePending(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "unattempted")
	if err := s.QueueImage(a.ID, a.Revision, "An unattempted scene"); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	if after := storeTestGet(t, s, a.ID); !reflect.DeepEqual(a, after) {
		t.Fatalf("recovery changed unattempted image: %+v", after)
	}
	if job, err := s.ClaimNextImage(); err != nil || job.ID != a.ID {
		t.Fatalf("recovery prevented safe pending work: %+v, %v", job, err)
	}
}

func TestStoreImageRevisionMustBePositive(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "positive")
	for _, revision := range []int{0, -1} {
		if _, err := s.DB.Exec("UPDATE kp_articles SET image_revision = ? WHERE id = ?", revision, a.ID); err == nil {
			t.Fatalf("accepted nonpositive image revision %d", revision)
		}
	}
	if _, err := s.DB.Exec("UPDATE kp_articles SET image_revision = NULL WHERE id = ?", a.ID); err == nil {
		t.Fatal("accepted null image revision")
	}
}

func TestStoreImageQueueSkipsEmptyPrompts(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "orphan")
	for _, prompt := range []string{"", " \n\t "} {
		if _, err := s.DB.Exec("UPDATE kp_articles SET image_status = 'pending', image_prompt = ? WHERE id = ?", prompt, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ClaimNextImage(); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("claimed orphan image job: %v", err)
		}
	}
	encoded, err := json.Marshal(Generated{Headline: "H", Body: "B", ImagePrompt: "A scene"})
	if err != nil || !strings.Contains(string(encoded), `"image_prompt":"A scene"`) || strings.Contains(string(encoded), "image_query") {
		t.Fatalf("generated prompt contract: %s, %v", encoded, err)
	}
}

func TestStoreV1ToV3MigrationIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1-duplicate.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema + "ALTER TABLE kp_articles ADD COLUMN generated_image TEXT NOT NULL DEFAULT '';"); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("opened v1 with duplicate v3 column")
	}
	var version, count int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration changed version: %d, %v", version, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('kp_articles') WHERE name IN ('image_query','image_status','image_photo','image_prompt','image_revision')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration retained partial v2/v3 changes: %d, %v", count, err)
	}
}

func TestStoreNewStoryClearsPreviousImageMetadata(t *testing.T) {
	for _, prompt := range []string{"", "An entirely new scene"} {
		t.Run(prompt, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestSubmit(t, s, "new-story")
			job, err := s.ClaimNext()
			if err != nil {
				t.Fatal(err)
			}
			photoJSON, err := json.Marshal(storeTestPhoto())
			if err != nil {
				t.Fatal(err)
			}
			imageJSON, err := json.Marshal(storeTestImage())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.Exec("UPDATE kp_articles SET image_photo = ?, generated_image = ?, image_prompt = 'Old prompt', image_status = 'ready' WHERE id = ?", string(photoJSON), string(imageJSON), a.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "New headline", Body: "New story", ImagePrompt: prompt}); err != nil {
				t.Fatal(err)
			}
			a = storeTestGet(t, s, a.ID)
			want := "none"
			if prompt != "" {
				want = StatusPending
			}
			if a.ImagePrompt != prompt || a.ImageStatus != want || a.ImageRevision != job.ImageRevision+1 || a.Image != nil || a.Photo != nil {
				t.Fatalf("new story retained old image: %+v", a)
			}
		})
	}
}

func TestStoreImageAssetBoundsAndStoredValidation(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "bounds")
	if err := s.QueueImage(a.ID, a.Revision, "A scene"); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range []ImageAsset{
		{Name: storeTestImage().Name, Width: 4097, Height: 1},
		{Name: storeTestImage().Name, Width: 1, Height: 4097},
		{Name: storeTestImage().Name, Width: 4096, Height: 4096},
	} {
		if err := s.CompleteImage(job.ID, job.ImageRevision, asset); err == nil {
			t.Fatalf("accepted oversized asset: %+v", asset)
		}
		encoded, err := json.Marshal(asset)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec("UPDATE kp_articles SET generated_image = ? WHERE id = ?", string(encoded), a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetArticle(a.ID); err == nil || err.Error() != "invalid stored image metadata" {
			t.Fatalf("read oversized metadata: %v", err)
		}
	}
	asset := ImageAsset{Name: "0123456789abcdef0123456789abcdef.png", Width: 4000, Height: 4000}
	if err := s.CompleteImage(job.ID, job.ImageRevision, asset); err != nil {
		t.Fatalf("boundary asset rejected: %v", err)
	}
	a = storeTestGet(t, s, a.ID)
	if !reflect.DeepEqual(a.Image, &asset) {
		t.Fatalf("boundary asset serialization: %+v", a)
	}
}
