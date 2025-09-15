package database

import (
	"context"
	"database/sql"
	"fmt"
)

// QuestionSelector handles intelligent question selection
type QuestionSelector struct {
	db *sql.DB
}

func NewQuestionSelector(db *sql.DB) *QuestionSelector {
	return &QuestionSelector{db: db}
}

// MarkQuestionUsed updates the last_used_at timestamp
func (qs *QuestionSelector) MarkQuestionUsed(ctx context.Context, questionID int) error {
	query := `UPDATE questions SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`

	_, err := qs.db.ExecContext(ctx, query, questionID)
	if err != nil {
		return fmt.Errorf("failed to mark question as used: %w", err)
	}

	return nil
}

// SelectNextQuestion picks the best question based on rotation logic
func (qs *QuestionSelector) SelectNextQuestion(ctx context.Context, category string) (*Question, error) {
	// Strategy: Pick the least recently used question in the category
	// If multiple questions have never been used, pick randomly among them

	query := `
             SELECT id, text, category, last_used_at, created_at
             FROM questions
             WHERE category = ?
             ORDER BY
                 CASE WHEN last_used_at IS NULL THEN 0 ELSE 1 END,  -- Unused questions first
                 last_used_at ASC,                                   -- Then oldest used ones
                 RANDOM()                                            -- Random tiebreaker
             LIMIT 1
         `

	var q Question
	var lastUsedAt sql.NullTime

	err := qs.db.QueryRowContext(ctx, query, category).Scan(
		&q.ID,
		&q.Text,
		&q.Category,
		&lastUsedAt,
		&q.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no questions found for category: %s", category)
		}
		return nil, fmt.Errorf("failed to select question: %w", err)
	}

	// Handle nullable timestamp
	if lastUsedAt.Valid {
		q.LastUsedAt = &lastUsedAt.Time
	}

	return &q, nil
}

// GetQuestionsByCategory retrieves all questions in a category
func (qs *QuestionSelector) GetQuestionsByCategory(ctx context.Context, category string) ([]Question, error) {
	query := `
             SELECT id, text, category, last_used_at, created_at
             FROM questions
             WHERE category = ?
             ORDER BY created_at DESC
         `

	rows, err := qs.db.QueryContext(ctx, query, category)
	if err != nil {
		return nil, fmt.Errorf("failed to query questions: %w", err)
	}
	defer rows.Close()

	var questions []Question

	for rows.Next() {
		var q Question
		var lastUsedAt sql.NullTime

		err := rows.Scan(&q.ID, &q.Text, &q.Category, &lastUsedAt, &q.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan question: %w", err)
		}

		if lastUsedAt.Valid {
			q.LastUsedAt = &lastUsedAt.Time
		}

		questions = append(questions, q)
	}

	return questions, nil
}

// AddQuestion inserts a new question into the database
func (qs *QuestionSelector) AddQuestion(ctx context.Context, text, category string) (*Question, error) {
	query := `INSERT INTO questions (text, category) VALUES (?, ?)`

	result, err := qs.db.ExecContext(ctx, query, text, category)
	if err != nil {
		return nil, fmt.Errorf("failed to add question: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get question ID: %w", err)
	}

	// Query back the created question
	return qs.GetQuestionByID(ctx, int(id))
}

// GetQuestionByID retrieves a question by ID
func (qs *QuestionSelector) GetQuestionByID(ctx context.Context, id int) (*Question, error) {
	query := `SELECT id, text, category, last_used_at, created_at FROM questions WHERE id = ?`

	var q Question
	var lastUsedAt sql.NullTime

	err := qs.db.QueryRowContext(ctx, query, id).Scan(
		&q.ID,
		&q.Text,
		&q.Category,
		&lastUsedAt,
		&q.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get question: %w", err)
	}

	if lastUsedAt.Valid {
		q.LastUsedAt = &lastUsedAt.Time
	}

	return &q, nil
}
