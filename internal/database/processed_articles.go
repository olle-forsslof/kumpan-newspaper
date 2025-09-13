package database

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateProcessedArticle creates a new processed article record
func (db *DB) CreateProcessedArticle(article ProcessedArticle) (int, error) {
	// Validate the article before inserting
	if err := article.Validate(); err != nil {
		return 0, fmt.Errorf("validation failed: %w", err)
	}

	// Set processed_at timestamp if article is successful and doesn't have one
	var processedAt *time.Time
	if article.ProcessingStatus == ProcessingStatusSuccess && article.ProcessedAt == nil {
		now := time.Now()
		processedAt = &now
	} else {
		processedAt = article.ProcessedAt
	}

	query := `
		INSERT INTO processed_articles (
			submission_id, newsletter_issue_id, journalist_type, processed_content, 
			processing_prompt, template_format, processing_status, error_message, 
			retry_count, word_count, processed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := db.Exec(query,
		article.SubmissionID,
		article.NewsletterIssueID,
		article.JournalistType,
		article.ProcessedContent,
		article.ProcessingPrompt,
		article.TemplateFormat,
		article.ProcessingStatus,
		article.ErrorMessage,
		article.RetryCount,
		article.WordCount,
		processedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create processed article: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get processed article ID: %w", err)
	}

	return int(id), nil
}

// GetProcessedArticle retrieves a processed article by ID
func (db *DB) GetProcessedArticle(id int) (*ProcessedArticle, error) {
	query := `
		SELECT id, submission_id, newsletter_issue_id, journalist_type, processed_content,
			   processing_prompt, template_format, processing_status, error_message,
			   retry_count, word_count, processed_at, created_at
		FROM processed_articles 
		WHERE id = ?`

	row := db.QueryRow(query, id)

	var article ProcessedArticle
	var processedAt sql.NullTime
	var newsletterIssueID sql.NullInt64
	var errorMessage sql.NullString
	var processedContent sql.NullString
	var processingPrompt sql.NullString

	err := row.Scan(
		&article.ID,
		&article.SubmissionID,
		&newsletterIssueID,
		&article.JournalistType,
		&processedContent,
		&processingPrompt,
		&article.TemplateFormat,
		&article.ProcessingStatus,
		&errorMessage,
		&article.RetryCount,
		&article.WordCount,
		&processedAt,
		&article.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("processed article with ID %d not found", id)
		}
		return nil, fmt.Errorf("failed to get processed article: %w", err)
	}

	// Handle nullable fields
	if newsletterIssueID.Valid {
		issueID := int(newsletterIssueID.Int64)
		article.NewsletterIssueID = &issueID
	}
	if errorMessage.Valid {
		article.ErrorMessage = &errorMessage.String
	}
	if processedContent.Valid {
		article.ProcessedContent = processedContent.String
	}
	if processingPrompt.Valid {
		article.ProcessingPrompt = processingPrompt.String
	}
	if processedAt.Valid {
		article.ProcessedAt = &processedAt.Time
	}

	return &article, nil
}

// UpdateProcessedArticleStatus updates the processing status, error message, and retry count
func (db *DB) UpdateProcessedArticleStatus(id int, status string, errorMessage *string, retryCount int) error {
	// Validate the status
	if !ValidProcessingStatuses[status] {
		return fmt.Errorf("invalid processing status: %s", status)
	}

	// Set processed_at timestamp if status is success
	var processedAt *time.Time
	if status == ProcessingStatusSuccess {
		now := time.Now()
		processedAt = &now
	}

	query := `
		UPDATE processed_articles 
		SET processing_status = ?, error_message = ?, retry_count = ?, processed_at = ?
		WHERE id = ?`

	result, err := db.Exec(query, status, errorMessage, retryCount, processedAt, id)
	if err != nil {
		return fmt.Errorf("failed to update processed article status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("processed article with ID %d not found", id)
	}

	return nil
}

// GetProcessedArticlesByStatus retrieves all processed articles with a specific status
func (db *DB) GetProcessedArticlesByStatus(status string) ([]ProcessedArticle, error) {
	// Validate the status
	if !ValidProcessingStatuses[status] {
		return nil, fmt.Errorf("invalid processing status: %s", status)
	}

	query := `
		SELECT id, submission_id, newsletter_issue_id, journalist_type, processed_content,
			   processing_prompt, template_format, processing_status, error_message,
			   retry_count, word_count, processed_at, created_at
		FROM processed_articles 
		WHERE processing_status = ?
		ORDER BY created_at DESC`

	rows, err := db.Query(query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to query processed articles by status: %w", err)
	}
	defer rows.Close()

	var articles []ProcessedArticle
	for rows.Next() {
		var article ProcessedArticle
		var processedAt sql.NullTime
		var newsletterIssueID sql.NullInt64
		var errorMessage sql.NullString
		var processedContent sql.NullString
		var processingPrompt sql.NullString

		err := rows.Scan(
			&article.ID,
			&article.SubmissionID,
			&newsletterIssueID,
			&article.JournalistType,
			&processedContent,
			&processingPrompt,
			&article.TemplateFormat,
			&article.ProcessingStatus,
			&errorMessage,
			&article.RetryCount,
			&article.WordCount,
			&processedAt,
			&article.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan processed article: %w", err)
		}

		// Handle nullable fields
		if newsletterIssueID.Valid {
			issueID := int(newsletterIssueID.Int64)
			article.NewsletterIssueID = &issueID
		}
		if errorMessage.Valid {
			article.ErrorMessage = &errorMessage.String
		}
		if processedContent.Valid {
			article.ProcessedContent = processedContent.String
		}
		if processingPrompt.Valid {
			article.ProcessingPrompt = processingPrompt.String
		}
		if processedAt.Valid {
			article.ProcessedAt = &processedAt.Time
		}

		articles = append(articles, article)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over processed articles: %w", err)
	}

	return articles, nil
}

// GetProcessedArticlesBySubmissionID retrieves all processed articles for a specific submission
func (db *DB) GetProcessedArticlesBySubmissionID(submissionID int) ([]ProcessedArticle, error) {
	query := `
		SELECT id, submission_id, newsletter_issue_id, journalist_type, processed_content,
			   processing_prompt, template_format, processing_status, error_message,
			   retry_count, word_count, processed_at, created_at
		FROM processed_articles 
		WHERE submission_id = ?
		ORDER BY created_at DESC`

	rows, err := db.Query(query, submissionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query processed articles by submission ID: %w", err)
	}
	defer rows.Close()

	var articles []ProcessedArticle
	for rows.Next() {
		var article ProcessedArticle
		var processedAt sql.NullTime
		var newsletterIssueID sql.NullInt64
		var errorMessage sql.NullString
		var processedContent sql.NullString
		var processingPrompt sql.NullString

		err := rows.Scan(
			&article.ID,
			&article.SubmissionID,
			&newsletterIssueID,
			&article.JournalistType,
			&processedContent,
			&processingPrompt,
			&article.TemplateFormat,
			&article.ProcessingStatus,
			&errorMessage,
			&article.RetryCount,
			&article.WordCount,
			&processedAt,
			&article.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan processed article: %w", err)
		}

		// Handle nullable fields
		if newsletterIssueID.Valid {
			issueID := int(newsletterIssueID.Int64)
			article.NewsletterIssueID = &issueID
		}
		if errorMessage.Valid {
			article.ErrorMessage = &errorMessage.String
		}
		if processedContent.Valid {
			article.ProcessedContent = processedContent.String
		}
		if processingPrompt.Valid {
			article.ProcessingPrompt = processingPrompt.String
		}
		if processedAt.Valid {
			article.ProcessedAt = &processedAt.Time
		}

		articles = append(articles, article)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over processed articles: %w", err)
	}

	return articles, nil
}
