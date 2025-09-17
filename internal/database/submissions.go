package database

import (
	"context"
	"database/sql"
	"fmt"
)

// SubmissionManager handles news submission operations
type SubmissionManager struct {
	db *sql.DB
}

// NewSubmissionManager creates a new submission manager
func NewSubmissionManager(db *sql.DB) *SubmissionManager {
	return &SubmissionManager{db: db}
}

// CreateNewsSubmission creates a news submission without a specific question
func (sm *SubmissionManager) CreateNewsSubmission(ctx context.Context, userID, content string) (*Submission, error) {
	result, err := sm.db.ExecContext(ctx,
		"INSERT INTO submissions (user_id, question_id, content) VALUES (?, NULL, ?)",
		userID, content,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create news submission: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get news submission ID: %w", err)
	}

	// Retrieve the created submission
	return sm.getSubmissionByID(ctx, int(id))
}

// GetSubmissionsByUser retrieves all submissions by a specific user
func (sm *SubmissionManager) GetSubmissionsByUser(ctx context.Context, userID string) ([]Submission, error) {
	rows, err := sm.db.QueryContext(ctx,
		"SELECT id, user_id, question_id, content, created_at FROM submissions WHERE user_id = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query submissions by user: %w", err)
	}
	defer rows.Close()

	return sm.scanSubmissions(rows)
}

// GetAllSubmissions retrieves all submissions (for admin use)
func (sm *SubmissionManager) GetAllSubmissions(ctx context.Context) ([]Submission, error) {
	rows, err := sm.db.QueryContext(ctx,
		"SELECT id, user_id, question_id, content, created_at FROM submissions ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query all submissions: %w", err)
	}
	defer rows.Close()

	return sm.scanSubmissions(rows)
}

// getSubmissionByID is a helper method to retrieve a submission by ID
func (sm *SubmissionManager) getSubmissionByID(ctx context.Context, id int) (*Submission, error) {
	var submission Submission
	var questionID sql.NullInt64

	err := sm.db.QueryRowContext(ctx,
		"SELECT id, user_id, question_id, content, created_at FROM submissions WHERE id = ?",
		id,
	).Scan(&submission.ID, &submission.UserID, &questionID, &submission.Content, &submission.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("submission not found")
		}
		return nil, fmt.Errorf("failed to get submission: %w", err)
	}

	// Handle nullable question_id
	if questionID.Valid {
		qid := int(questionID.Int64)
		submission.QuestionID = &qid
	} else {
		submission.QuestionID = nil
	}

	return &submission, nil
}

// scanSubmissions is a helper method to scan multiple submissions from query results
func (sm *SubmissionManager) scanSubmissions(rows *sql.Rows) ([]Submission, error) {
	var submissions []Submission

	for rows.Next() {
		var submission Submission
		var questionID sql.NullInt64

		err := rows.Scan(&submission.ID, &submission.UserID, &questionID, &submission.Content, &submission.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan submission: %w", err)
		}

		// Handle nullable question_id
		if questionID.Valid {
			qid := int(questionID.Int64)
			submission.QuestionID = &qid
		} else {
			submission.QuestionID = nil
		}

		submissions = append(submissions, submission)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating submissions: %w", err)
	}

	return submissions, nil
}

// CreateAnonymousSubmission creates a submission without user attribution
func (db *DB) CreateAnonymousSubmission(content, category string) (*Submission, error) {
	result, err := db.Exec(
		"INSERT INTO submissions (user_id, question_id, content) VALUES ('', NULL, ?)",
		content,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create anonymous submission: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get anonymous submission ID: %w", err)
	}

	// Return the created submission
	return &Submission{
		ID:         int(id),
		UserID:     "", // Anonymous - no user ID
		QuestionID: nil,
		Content:    content,
	}, nil
}

// GetAnonymousSubmissionsByCategory retrieves anonymous submissions by category
func (db *DB) GetAnonymousSubmissionsByCategory(category string) ([]Submission, error) {
	rows, err := db.Query(
		"SELECT id, user_id, question_id, content, created_at FROM submissions WHERE user_id = '' ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query anonymous submissions: %w", err)
	}
	defer rows.Close()

	var submissions []Submission
	for rows.Next() {
		var submission Submission
		var questionID sql.NullInt64

		err := rows.Scan(&submission.ID, &submission.UserID, &questionID, &submission.Content, &submission.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anonymous submission: %w", err)
		}

		// Handle nullable question_id
		if questionID.Valid {
			qid := int(questionID.Int64)
			submission.QuestionID = &qid
		} else {
			submission.QuestionID = nil
		}

		submissions = append(submissions, submission)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anonymous submissions: %w", err)
	}

	return submissions, nil
}
