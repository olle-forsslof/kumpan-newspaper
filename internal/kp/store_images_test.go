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
			if a.ImageQuery != "" || a.ImageStatus != "none" || a.Photo != nil {
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
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
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

func TestStoreImageQueryValidation(t *testing.T) {
	for _, query := range []string{"", "   ", strings.Repeat("x", 161), strings.Repeat("word ", 13), "trees\nforest", "trees\x00", "trees\u0085"} {
		if err := ValidateImageQuery(query); err == nil {
			t.Fatalf("accepted query %q", query)
		}
		if strings.TrimSpace(query) != "" && ValidateGenerated(KindReport, Generated{Headline: "H", Body: "B", ImageQuery: query}) == nil {
			t.Fatalf("accepted generated query %q", query)
		}
	}
	for _, query := range []string{strings.Repeat("x", 160), strings.TrimSpace(strings.Repeat("word ", 12)), " trees forest "} {
		if err := ValidateImageQuery(query); err != nil {
			t.Fatalf("valid query %q: %v", query, err)
		}
	}
}

func TestStoreAutoImageQueryAndTextEdits(t *testing.T) {
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
			g := Generated{Headline: "H", Body: "B", Question: "Q", Signature: "S", ImageQuery: " trees forest "}
			if err := s.CompleteArticle(job.ID, job.Revision, g); err != nil {
				t.Fatal(err)
			}
			a = storeTestGet(t, s, a.ID)
			if a.ImageQuery != "trees forest" || a.ImageStatus != StatusPending || a.Photo != nil {
				t.Fatalf("automatic query: %+v", a)
			}
			image, err := s.ClaimNextImage()
			if err != nil || image.Revision != a.Revision+1 || image.ImageStatus != StatusProcessing {
				t.Fatalf("image claim: %+v, %v", image, err)
			}
			photo := storeTestPhoto()
			if err := s.CompleteImage(image.ID, image.Revision, photo); err != nil {
				t.Fatal(err)
			}
			a = storeTestGet(t, s, a.ID)
			g.ImageQuery = "different query"
			g.Body = "Edited body"
			if err := s.SaveArticle(a.ID, a.Revision, g); err != nil {
				t.Fatal(err)
			}
			edited := storeTestGet(t, s, a.ID)
			if edited.Body != g.Body || edited.ImageQuery != a.ImageQuery || edited.ImageStatus != StatusReady || !reflect.DeepEqual(edited.Photo, &photo) {
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
	photo := storeTestPhoto()
	if err := s.CompleteImage(job.ID, job.Revision, photo); err != nil {
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
	if err != nil || !reflect.DeepEqual(job.Photo, &photo) {
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
	if recovered.ImageStatus != StatusPending || recovered.Revision != job.Revision+1 || !reflect.DeepEqual(recovered.Photo, &photo) {
		t.Fatalf("recovery: %+v", recovered)
	}
	if err := s.CompleteImage(job.ID, job.Revision, photo); !errors.Is(err, ErrConflict) {
		t.Fatalf("abandoned completion: %v", err)
	}
	job, err = s.ClaimNextImage()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailImage(job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.ImageStatus != StatusFailed || a.Revision != job.Revision+1 || !reflect.DeepEqual(a.Photo, &photo) {
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
	if err := s.CompleteImage(job.ID, job.Revision, photo); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed image completion: %v", err)
	}
	if err := s.FailImage(job.ID, job.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed image failure: %v", err)
	}
	a = storeTestGet(t, s, a.ID)
	if a.ImageStatus != StatusRemoved || a.Photo != nil || a.ImageQuery != "new replacement" {
		t.Fatalf("opt-out: %+v", a)
	}
	if err := s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Edited", Body: "Body", ImageQuery: "automatic"}); err != nil {
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
					if err := s.FailImage(job.ID, job.Revision); err != nil {
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
				func() error { return s.CompleteImage(a.ID, a.Revision, storeTestPhoto()) },
				func() error { return s.FailImage(a.ID, a.Revision) },
			} {
				if err := mutate(); !errors.Is(err, ErrConflict) {
					t.Fatalf("published image mutation: %v", err)
				}
			}
			for _, column := range []string{"image_query", "image_status", "image_photo"} {
				if _, err := s.DB.Exec("UPDATE kp_articles SET "+column+" = 'removed' WHERE id = ?", a.ID); err == nil {
					t.Fatalf("trigger permitted published %s change", column)
				}
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
	for _, field := range []string{"URL", "PhotographerURL", "PageURL", "DownloadURL"} {
		photo := storeTestPhoto()
		reflect.ValueOf(&photo).Elem().FieldByName(field).SetString("https://unsplash.com.evil.example/stolen")
		if err := s.CompleteImage(job.ID, job.Revision, photo); err == nil {
			t.Fatalf("accepted unsafe %s", field)
		}
	}
	if after := storeTestGet(t, s, a.ID); !reflect.DeepEqual(job, after) {
		t.Fatalf("unsafe completion mutated image: %+v", after)
	}
	if err := s.RemoveArticle(job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	removed := storeTestGet(t, s, a.ID)
	if err := s.CompleteImage(removed.ID, removed.Revision, storeTestPhoto()); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed article image completion: %v", err)
	}
	if err := s.FailImage(removed.ID, removed.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed article image failure: %v", err)
	}
	if err := s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	if after := storeTestGet(t, s, a.ID); !reflect.DeepEqual(removed, after) {
		t.Fatalf("recovered removed article image: %+v", after)
	}
	for _, metadata := range []string{"{", "null", `{"URL":"javascript:alert(1)"}`} {
		if _, err := s.DB.Exec("UPDATE kp_articles SET image_photo = ? WHERE id = ?", metadata, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetArticle(a.ID); err == nil || err.Error() != "invalid stored image metadata" {
			t.Fatalf("unsafe database metadata read: %v", err)
		}
	}
}
