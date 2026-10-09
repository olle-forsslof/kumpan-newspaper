package kp

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func storeTestOpen(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kp.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func storeTestSubmit(t *testing.T, s *Store, key string) *Article {
	t.Helper()
	a, err := s.AddSubmission(KindReport, "U1", "Reporter", "  Original report  ", key)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func storeTestReady(t *testing.T, s *Store, key string) *Article {
	t.Helper()
	a := storeTestSubmit(t, s, key)
	job, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != a.ID {
		t.Fatalf("claimed %d, want %d", job.ID, a.ID)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "Headline", Body: "Body"}); err != nil {
		t.Fatal(err)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStoreReadOnlyDoesNotCreateDraft(t *testing.T) {
	s, _ := storeTestOpen(t)
	if _, err := s.LatestPublished(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latest: %v", err)
	}
	if _, err := s.PendingNotification(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("notification: %v", err)
	}
	if _, err := s.ClaimNext(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("job: %v", err)
	}
	if _, err := s.Issue(1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("issue: %v", err)
	}
	if _, err := s.GetArticle(1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("article: %v", err)
	}
	if articles, err := s.Articles(1); err != nil || len(articles) != 0 {
		t.Fatalf("articles: %v, %v", articles, err)
	}
	if issues, err := s.PublishedIssues(); err != nil || len(issues) != 0 {
		t.Fatalf("issues: %v, %v", issues, err)
	}
	if sent, err := s.ReminderSent("unused"); err != nil || sent {
		t.Fatalf("reminder: %v, %v", sent, err)
	}
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM kp_issues").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("read methods created %d issues", count)
	}
	draft, err := s.CurrentDraft()
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CurrentDraft()
	if err != nil || again.ID != draft.ID {
		t.Fatalf("draft not reused: %v, %v", again, err)
	}
	if draft.CreatedAt.IsZero() || draft.CreatedAt.Location().String() != "UTC" || draft.PublishedAt != nil {
		t.Fatalf("invalid draft: %+v", draft)
	}
	if _, err = s.DB.Exec("INSERT INTO kp_issues (title, created_at) VALUES ('extra', CURRENT_TIMESTAMP)"); err == nil {
		t.Fatal("second draft was permitted")
	}
}

func TestStoreSubmissionAnonymousAndDedupe(t *testing.T) {
	s, _ := storeTestOpen(t)
	a, err := s.AddSubmission(KindQuestion, "SECRET-ID", strings.Repeat("s", 501), "  Why? \n", " opaque key ")
	if err != nil {
		t.Fatal(err)
	}
	if a.AuthorID != "" || a.AuthorName != "" || a.Original != "Why?" || a.Status != StatusPending || a.Revision != 1 {
		t.Fatalf("bad anonymous submission: %+v", a)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil || a.AuthorID != "" || a.AuthorName != "" {
		t.Fatalf("identity persisted: %+v, %v", a, err)
	}
	duplicate, err := s.AddSubmission(KindReport, "U2", "Other", "Changed input", " opaque key ")
	if err != nil || duplicate.ID != a.ID || duplicate.Original != "Why?" {
		t.Fatalf("dedupe: %+v, %v", duplicate, err)
	}
	report := storeTestSubmit(t, s, "opaque key")
	if report.ID == a.ID || report.AuthorID != "U1" || report.AuthorName != "Reporter" || report.Original != "Original report" {
		t.Fatalf("report: %+v", report)
	}
	first := storeTestSubmit(t, s, "")
	second := storeTestSubmit(t, s, "")
	if first.ID == second.ID {
		t.Fatal("empty keys deduplicated unrelated submissions")
	}
	for _, input := range []struct{ kind, name, text string }{
		{"unknown", "", "input"}, {KindReport, "", " \n "}, {KindReport, "", strings.Repeat("x", 10001)}, {KindReport, strings.Repeat("x", 501), "input"},
	} {
		if _, err := s.AddSubmission(input.kind, "", input.name, input.text, "invalid"); err == nil {
			t.Fatalf("accepted invalid input: kind %q, name length %d, text length %d", input.kind, len(input.name), len(input.text))
		}
	}
	if _, err := s.DB.Exec(`INSERT INTO kp_articles (issue_id, kind, author_id, author_name, original, status, created_at)
 VALUES (999, 'report', '', '', 'text', 'pending', CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("foreign keys disabled")
	}
}

func TestStoreRecoveryAndRetryAreDurable(t *testing.T) {
	s, path := storeTestOpen(t)
	a := storeTestSubmit(t, s, "job")
	job, err := s.ClaimNext()
	if err != nil || job.Status != StatusProcessing || job.Revision != a.Revision+1 {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	if err = s.SaveArticle(job.ID, job.Revision, Generated{Headline: "H", Body: "B"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("edit processing: %v", err)
	}
	if err = s.RetryArticle(job.ID, job.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry processing: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	persisted, err := s.GetArticle(job.ID)
	if err != nil || persisted.Status != StatusProcessing || persisted.Revision != job.Revision {
		t.Fatalf("durability: %+v, %v", persisted, err)
	}
	if err = s.RecoverProcessing(); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "Late", Body: "Late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("abandoned callback: %v", err)
	}
	job, err = s.ClaimNext()
	if err != nil || job.Revision != a.Revision+3 {
		t.Fatalf("recovered claim: %+v, %v", job, err)
	}
	if err = s.FailArticle(job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	failed, err := s.GetArticle(job.ID)
	if err != nil || failed.Status != StatusFailed || failed.Error != "Generation failed. Please retry." {
		t.Fatalf("failed: %+v, %v", failed, err)
	}
	if err = s.FailArticle(job.ID, job.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate failure: %v", err)
	}
	if err = s.RetryArticle(failed.ID, failed.Revision); err != nil {
		t.Fatal(err)
	}
	pending, err := s.GetArticle(job.ID)
	if err != nil || pending.Status != StatusPending || pending.Error != "" || pending.Revision != failed.Revision+1 {
		t.Fatalf("retry: %+v, %v", pending, err)
	}
	if err = s.RetryArticle(pending.ID, pending.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate retry: %v", err)
	}
	job, err = s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{}); err == nil {
		t.Fatal("accepted invalid generation")
	}
	unchanged, err := s.GetArticle(job.ID)
	if err != nil || unchanged.Revision != job.Revision || unchanged.Status != StatusProcessing {
		t.Fatalf("invalid completion mutated job: %+v, %v", unchanged, err)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "H", Body: "B"}); err != nil {
		t.Fatal(err)
	}
}

func TestStorePublicationFreezeAndRollover(t *testing.T) {
	s, path := storeTestOpen(t)
	draft, err := s.CurrentDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(draft.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("empty publish: %v", err)
	}
	a := storeTestSubmit(t, s, "one")
	if _, err = s.Publish(draft.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("pending publish: %v", err)
	}
	job, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(draft.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("processing publish: %v", err)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "H", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Edited", Body: "Edited body"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Stale", Body: "Stale"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "Late", Body: "Late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("late completion: %v", err)
	}
	a, err = s.GetArticle(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	removed := storeTestSubmit(t, s, "removed")
	removedJob, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveArticle(removedJob.ID, removedJob.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArticle(removedJob.ID, removedJob.Revision, Generated{Headline: "Late", Body: "Late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed callback: %v", err)
	}
	articles, err := s.Articles(draft.ID)
	if err != nil || len(articles) != 1 || articles[0].Headline != "Edited" {
		t.Fatalf("visible articles: %+v, %v", articles, err)
	}
	published, err := s.Publish(draft.ID)
	if err != nil || published.PublishedAt == nil {
		t.Fatalf("publish: %+v, %v", published, err)
	}
	again, err := s.Publish(draft.ID)
	if err != nil || !again.PublishedAt.Equal(*published.PublishedAt) {
		t.Fatalf("idempotent publish: %+v, %v", again, err)
	}
	for name, mutate := range map[string]func() error{
		"save":   func() error { return s.SaveArticle(a.ID, a.Revision, Generated{Headline: "Frozen", Body: "Frozen"}) },
		"remove": func() error { return s.RemoveArticle(a.ID, a.Revision) },
		"complete": func() error {
			return s.CompleteArticle(a.ID, a.Revision, Generated{Headline: "Frozen", Body: "Frozen"})
		},
		"fail":  func() error { return s.FailArticle(a.ID, a.Revision) },
		"retry": func() error { return s.RetryArticle(a.ID, a.Revision) },
	} {
		if err = mutate(); !errors.Is(err, ErrConflict) {
			t.Fatalf("frozen %s: %v", name, err)
		}
	}
	for _, statement := range []struct {
		query string
		id    int
	}{
		{"UPDATE kp_articles SET body = 'overwrite' WHERE id = ?", a.ID},
		{"DELETE FROM kp_articles WHERE id = ?", a.ID},
		{"UPDATE kp_issues SET title = 'overwrite' WHERE id = ?", draft.ID},
		{"UPDATE kp_issues SET published_at = NULL WHERE id = ?", draft.ID},
		{"DELETE FROM kp_issues WHERE id = ?", draft.ID},
		{`INSERT INTO kp_articles (issue_id, kind, author_id, author_name, original, status, created_at)
 VALUES (?, 'report', '', '', 'text', 'pending', CURRENT_TIMESTAMP)`, draft.ID},
	} {
		if _, err = s.DB.Exec(statement.query, statement.id); err == nil {
			t.Fatalf("immutable trigger permitted %s", statement.query)
		}
	}
	duplicate, err := s.AddSubmission(KindReport, "", "", "redelivery", "one")
	if err != nil || duplicate.ID != a.ID {
		t.Fatalf("published dedupe: %+v, %v", duplicate, err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM kp_issues").Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedupe created draft: %d, %v", count, err)
	}
	duplicate, err = s.AddSubmission(KindReport, "", "", "redelivery", "removed")
	if err != nil || duplicate.ID != removed.ID || duplicate.Status != StatusRemoved {
		t.Fatalf("removed dedupe: %+v, %v", duplicate, err)
	}
	next := storeTestReady(t, s, "two")
	if next.IssueID == draft.ID {
		t.Fatal("submission attached to published issue")
	}
	second, err := s.Publish(next.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := s.PublishedIssues()
	if err != nil || len(issues) != 2 || issues[0].ID != second.ID || issues[1].ID != published.ID {
		t.Fatalf("publication ordering: %+v, %v", issues, err)
	}
	latest, err := s.LatestPublished()
	if err != nil || latest.ID != second.ID {
		t.Fatalf("latest: %+v, %v", latest, err)
	}
	notification, err := s.PendingNotification()
	if err != nil || notification.ID != published.ID {
		t.Fatalf("notification ordering: %+v, %v", notification, err)
	}
	if err = s.MarkNotified(published.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkNotified(published.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkReminderSent("weekly:2026-10-07"); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkReminderSent("weekly:2026-10-07"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	notification, err = s.PendingNotification()
	if err != nil || notification.ID != second.ID {
		t.Fatalf("durable notification: %+v, %v", notification, err)
	}
	if sent, err := s.ReminderSent("weekly:2026-10-07"); err != nil || !sent {
		t.Fatalf("durable reminder: %v, %v", sent, err)
	}
	if err = s.MarkNotified(second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PendingNotification(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("notifications exhausted: %v", err)
	}
	fresh, err := s.CurrentDraft()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkNotified(fresh.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("notified draft: %v", err)
	}
}

func TestStorePublishRevalidatesContent(t *testing.T) {
	s, _ := storeTestOpen(t)
	a := storeTestReady(t, s, "one")
	if _, err := s.DB.Exec("UPDATE kp_articles SET headline = ' ' WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(a.IssueID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("invalid ready content: %v", err)
	}
	i, err := s.Issue(a.IssueID)
	if err != nil || i.PublishedAt != nil {
		t.Fatalf("failed publish changed issue: %+v, %v", i, err)
	}
	if err = s.RemoveArticle(a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(a.IssueID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("removed-only issue: %v", err)
	}
}

func TestStoreQuestionPublicationAndFailedWork(t *testing.T) {
	s, _ := storeTestOpen(t)
	question, err := s.AddSubmission(KindQuestion, "secret", "secret name", "Why?", "question")
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "Advice", Body: "Answer"}); err == nil {
		t.Fatal("completed question without question/signature")
	}
	g := Generated{Headline: "Advice", Body: "Answer", Question: "Why?", Signature: "Anonymous reader"}
	if err = s.CompleteArticle(job.ID, job.Revision, g); err != nil {
		t.Fatal(err)
	}
	storeTestSubmit(t, s, "failed report")
	job, err = s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailArticle(job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(question.IssueID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("failed work permitted publication: %v", err)
	}
	failed, err := s.GetArticle(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveArticle(failed.ID, failed.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(question.IssueID); err != nil {
		t.Fatal(err)
	}
	articles, err := s.Articles(question.IssueID)
	if err != nil || len(articles) != 1 {
		t.Fatalf("published articles: %+v, %v", articles, err)
	}
	a := articles[0]
	if a.AuthorID != "" || a.AuthorName != "" || a.Question != g.Question || a.Signature != g.Signature || a.Signoff != "" {
		t.Fatalf("published question: %+v", a)
	}
}

func TestStorePublishSubmissionRace(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		s, path := storeTestOpen(t)
		other, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		a := storeTestReady(t, s, "ready")
		start := make(chan struct{})
		publishResult := make(chan error, 1)
		added := make(chan *Article, 1)
		addError := make(chan error, 1)
		go func() {
			<-start
			_, err := s.Publish(a.IssueID)
			publishResult <- err
		}()
		go func() {
			<-start
			article, err := other.AddSubmission(KindReport, "", "", "New submission", "racing")
			added <- article
			addError <- err
		}()
		close(start)
		publishErr := <-publishResult
		article := <-added
		if err := <-addError; err != nil {
			other.Close()
			t.Fatal(err)
		}
		other.Close()
		issue, err := s.Issue(a.IssueID)
		if err != nil {
			t.Fatal(err)
		}
		if publishErr == nil {
			if issue.PublishedAt == nil || article.IssueID == issue.ID {
				t.Fatalf("submission entered published issue: %+v, %+v", issue, article)
			}
		} else if errors.Is(publishErr, ErrNotReady) {
			if issue.PublishedAt != nil || article.IssueID != issue.ID {
				t.Fatalf("failed publication lost draft submission: %+v, %+v", issue, article)
			}
		} else {
			t.Fatalf("publication race: %v", publishErr)
		}
	}
}

func TestStoreRemoveAllWorkStatuses(t *testing.T) {
	for _, status := range []string{StatusPending, StatusProcessing, StatusReady, StatusFailed} {
		t.Run(status, func(t *testing.T) {
			s, _ := storeTestOpen(t)
			a := storeTestSubmit(t, s, "one")
			if status != StatusPending {
				job, err := s.ClaimNext()
				if err != nil {
					t.Fatal(err)
				}
				if status == StatusReady {
					if err = s.CompleteArticle(job.ID, job.Revision, Generated{Headline: "H", Body: "B"}); err != nil {
						t.Fatal(err)
					}
				} else if status == StatusFailed {
					if err = s.FailArticle(job.ID, job.Revision); err != nil {
						t.Fatal(err)
					}
				}
			}
			a, err := s.GetArticle(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.RemoveArticle(a.ID, a.Revision-1); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale removal: %v", err)
			}
			if err = s.RemoveArticle(a.ID, a.Revision); err != nil {
				t.Fatal(err)
			}
			removed, err := s.GetArticle(a.ID)
			if err != nil || removed.Status != StatusRemoved || removed.Revision != a.Revision+1 {
				t.Fatalf("removal: %+v, %v", removed, err)
			}
			if err = s.RemoveArticle(removed.ID, removed.Revision); !errors.Is(err, ErrConflict) {
				t.Fatalf("repeat removal: %v", err)
			}
			if _, err = s.ClaimNext(); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("claimed removed article: %v", err)
			}
		})
	}
}

func TestStoreValidateGenerated(t *testing.T) {
	valid := Generated{Headline: "H", Body: "B", Question: "Q", Signature: "S"}
	if err := ValidateGenerated(KindQuestion, valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGenerated(KindReport, Generated{Headline: "<script>plain text</script>", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		kind string
		g    Generated
	}{
		{"unknown", valid}, {KindReport, Generated{Headline: " \n ", Body: "B"}},
		{KindReport, Generated{Headline: "H", Body: " "}},
		{KindQuestion, Generated{Headline: "H", Body: "B", Signature: "S"}},
		{KindQuestion, Generated{Headline: "H", Body: "B", Question: "Q"}},
		{KindReport, Generated{Headline: strings.Repeat("h", 501), Body: "B"}},
		{KindReport, Generated{Headline: "H", Body: strings.Repeat("b", 20001)}},
		{KindQuestion, Generated{Headline: "H", Body: "B", Question: strings.Repeat("q", 20001), Signature: "S"}},
		{KindQuestion, Generated{Headline: "H", Body: "B", Question: "Q", Signature: strings.Repeat("s", 501)}},
		{KindReport, Generated{Headline: "H", Body: "B", Signoff: strings.Repeat("s", 501)}},
	} {
		if err := ValidateGenerated(input.kind, input.g); err == nil {
			t.Fatalf("accepted invalid generation for %q", input.kind)
		}
	}
	if err := ValidateGenerated(KindQuestion, Generated{Headline: strings.Repeat("h", 500), Body: strings.Repeat("b", 20000), Question: strings.Repeat("q", 20000), Signature: strings.Repeat("s", 500)}); err != nil {
		t.Fatalf("boundary lengths: %v", err)
	}
}

func TestStoreReplaceCannotOverwritePublishedContent(t *testing.T) {
	s, path := storeTestOpen(t)
	a := storeTestReady(t, s, "published")
	if _, err := s.Publish(a.IssueID); err != nil {
		t.Fatal(err)
	}
	originalIssue, err := s.Issue(a.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.CurrentDraft()
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for _, store := range []*Store{s, other} {
		var recursive bool
		if err := store.DB.QueryRow("PRAGMA recursive_triggers").Scan(&recursive); err != nil || !recursive {
			t.Fatalf("recursive triggers: %v, %v", recursive, err)
		}
		_, err = store.DB.Exec(`INSERT OR REPLACE INTO kp_issues (id, title, created_at, published_at)
 VALUES (?, 'Replacement issue', ?, ?)`, originalIssue.ID, originalIssue.CreatedAt, originalIssue.PublishedAt)
		if err == nil || !strings.Contains(err.Error(), "published issue is immutable") {
			t.Fatalf("replaced published issue: %v", err)
		}
		_, err = store.DB.Exec(`INSERT OR REPLACE INTO kp_articles
 (id, issue_id, kind, author_id, author_name, original, request_key, status, headline, body, created_at)
 VALUES (?, ?, 'report', '', '', 'Replacement original', 'published', 'ready', 'Replacement headline', 'Replacement body', ?)`, a.ID, draft.ID, a.CreatedAt)
		if err == nil || !strings.Contains(err.Error(), "published issue is immutable") {
			t.Fatalf("replaced published article into draft: %v", err)
		}
		issue, err := s.Issue(originalIssue.ID)
		if err != nil || !reflect.DeepEqual(issue, originalIssue) {
			t.Fatalf("replacement changed original issue: %+v, %v", issue, err)
		}
		article, err := s.GetArticle(a.ID)
		if err != nil || !reflect.DeepEqual(article, a) {
			t.Fatalf("replacement changed original article: %+v, %v", article, err)
		}
		articles, err := s.Articles(draft.ID)
		if err != nil || len(articles) != 0 {
			t.Fatalf("failed replacement added draft content: %+v, %v", articles, err)
		}
	}
}

func TestStoreRefusesMissingSchemaGuards(t *testing.T) {
	for _, object := range []struct{ kind, name string }{
		{"INDEX", "kp_one_draft"},
		{"INDEX", "kp_article_queue"},
		{"INDEX", "kp_issue_articles"},
		{"TRIGGER", "kp_frozen_article_insert"},
		{"TRIGGER", "kp_frozen_article_update"},
		{"TRIGGER", "kp_frozen_article_delete"},
		{"TRIGGER", "kp_frozen_issue_update"},
		{"TRIGGER", "kp_frozen_issue_delete"},
	} {
		t.Run(object.name, func(t *testing.T) {
			s, path := storeTestOpen(t)
			if _, err := s.DB.Exec("DROP " + object.kind + " " + object.name); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if reopened, err := Open(path); err == nil {
				reopened.Close()
				t.Fatal("opened schema missing required guard")
			} else if !strings.Contains(err.Error(), object.name) {
				t.Fatalf("unexpected refusal: %v", err)
			}
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var exists bool
			if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE name = ?)", object.name).Scan(&exists); err != nil || exists {
				t.Fatalf("refusal repaired missing guard: %v, %v", exists, err)
			}
			var version int
			if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
				t.Fatalf("refusal changed schema version: %d, %v", version, err)
			}
		})
	}
}

func TestStorePublishReviewedConcurrentChanges(t *testing.T) {
	for _, operation := range []string{"add", "complete", "save", "remove"} {
		for _, order := range []string{"change_first", "publish_first", "simultaneous"} {
			t.Run(operation+"/"+order, func(t *testing.T) {
				s, path := storeTestOpen(t)
				base := storeTestReady(t, s, "base")
				other, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				var target *Article
				if operation == "complete" {
					storeTestSubmit(t, s, "target")
					target, err = other.ClaimNext()
					if err != nil {
						t.Fatal(err)
					}
				} else if operation == "save" || operation == "remove" {
					target = storeTestReady(t, s, "target")
				}
				before, err := s.Articles(base.IssueID)
				if err != nil {
					t.Fatal(err)
				}
				review := ReviewKey(base.IssueID, before)
				change := func() error {
					switch operation {
					case "add":
						_, err := other.AddSubmission(KindReport, "", "", "Unreviewed submission", "new")
						return err
					case "complete":
						return other.CompleteArticle(target.ID, target.Revision, Generated{Headline: "Unreviewed generation", Body: "New body"})
					case "save":
						return other.SaveArticle(target.ID, target.Revision, Generated{Headline: "Unreviewed edit", Body: "Edited body"})
					default:
						return other.RemoveArticle(target.ID, target.Revision)
					}
				}
				start := make(chan struct{})
				published := make(chan struct{})
				changed := make(chan struct{})
				publishResult := make(chan error, 1)
				changeResult := make(chan error, 1)
				go func() {
					<-start
					if order == "change_first" {
						<-changed
					}
					_, err := s.PublishReviewed(base.IssueID, review)
					publishResult <- err
					close(published)
				}()
				go func() {
					<-start
					if order == "publish_first" {
						<-published
					}
					changeResult <- change()
					close(changed)
				}()
				close(start)
				publishErr, changeErr := <-publishResult, <-changeResult
				issue, err := s.Issue(base.IssueID)
				if err != nil {
					t.Fatal(err)
				}
				after, err := s.Articles(base.IssueID)
				if err != nil {
					t.Fatal(err)
				}
				if publishErr == nil {
					if issue.PublishedAt == nil || !reflect.DeepEqual(before, after) {
						t.Fatalf("published unreviewed content: before %+v, after %+v", before, after)
					}
					if operation == "add" {
						if changeErr != nil {
							t.Fatal(changeErr)
						}
						added, err := other.AddSubmission(KindReport, "", "", "Redelivery", "new")
						if err != nil || added.IssueID == base.IssueID {
							t.Fatalf("new submission entered published issue: %+v, %v", added, err)
						}
					} else if !errors.Is(changeErr, ErrConflict) {
						t.Fatalf("mutation after publication: %v", changeErr)
					}
					return
				}
				if !errors.Is(publishErr, ErrConflict) && !errors.Is(publishErr, ErrNotReady) {
					t.Fatalf("publication race: %v", publishErr)
				}
				if changeErr != nil || issue.PublishedAt != nil || ReviewKey(base.IssueID, after) == review {
					t.Fatalf("change did not invalidate draft review: issue %+v, change error %v", issue, changeErr)
				}
				// Make the new submission publishable so readiness cannot mask the
				// stale-review check after the racing mutation has committed.
				if operation == "add" {
					job, err := other.ClaimNext()
					if err != nil {
						t.Fatal(err)
					}
					if err := other.CompleteArticle(job.ID, job.Revision, Generated{Headline: "Unreviewed new article", Body: "Body"}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.PublishReviewed(base.IssueID, review); !errors.Is(err, ErrConflict) {
					t.Fatalf("stale review accepted after %s: %v", operation, err)
				}
				after, err = s.Articles(base.IssueID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.PublishReviewed(base.IssueID, ReviewKey(base.IssueID, after)); err != nil {
					t.Fatalf("fresh review refused: %v", err)
				}
			})
		}
	}
}

func TestStoreRefusesLegacyAndUnsupportedSchema(t *testing.T) {
	for _, setup := range []string{
		"CREATE TABLE questions (id INTEGER)",
		"CREATE TABLE schema_migrations (version INTEGER)",
		"CREATE TABLE newsletter_issues (id INTEGER)",
		"CREATE TABLE submissions (id INTEGER)",
		"CREATE VIEW questions AS SELECT 1 AS id",
		"PRAGMA user_version = 2",
		"PRAGMA user_version = 3",
		"PRAGMA user_version = 1",
	} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.sqlite")
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec(setup); err != nil {
				t.Fatal(err)
			}
			var beforeVersion, beforeObjects int
			if err = db.QueryRow("PRAGMA user_version").Scan(&beforeVersion); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&beforeObjects); err != nil {
				t.Fatal(err)
			}
			if s, err := Open(path); err == nil {
				s.Close()
				t.Fatal("opened legacy or unsupported database")
			}
			var afterVersion, afterObjects int
			if err = db.QueryRow("PRAGMA user_version").Scan(&afterVersion); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&afterObjects); err != nil {
				t.Fatal(err)
			}
			if beforeVersion != afterVersion || beforeObjects != afterObjects {
				t.Fatalf("refusal modified database: versions %d/%d, objects %d/%d", beforeVersion, afterVersion, beforeObjects, afterObjects)
			}
		})
	}
	s, path := storeTestOpen(t)
	if _, err := s.DB.Exec("CREATE TABLE questions (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("opened mixed legacy/kp database")
	}
}

func TestStoreConcurrentSubmissionAndClaims(t *testing.T) {
	s, path := storeTestOpen(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	const workers = 12
	var wg sync.WaitGroup
	ids := make(chan int, workers)
	errs := make(chan error, workers)
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store := s
			if n%2 == 0 {
				store = other
			}
			a, err := store.AddSubmission(KindReport, "U", "Name", "Original", "same opaque key")
			if err != nil {
				errs <- err
				return
			}
			ids <- a.ID
		}(n)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var id int
	for got := range ids {
		if id != 0 && id != got {
			t.Fatalf("duplicate articles %d/%d", id, got)
		}
		id = got
	}
	claims := make(chan *Article, workers)
	errs = make(chan error, workers)
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store := s
			if n%2 == 0 {
				store = other
			}
			a, err := store.ClaimNext()
			if errors.Is(err, sql.ErrNoRows) {
				return
			}
			if err != nil {
				errs <- err
				return
			}
			claims <- a
		}(n)
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(claims) != 1 {
		t.Fatalf("got %d claims for one job", len(claims))
	}
	job := <-claims
	if job.ID != id || job.Revision != 2 {
		t.Fatalf("claim: %+v", job)
	}
	var mode string
	if err := s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode: %q, %v", mode, err)
	}
}
