package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQL database connection
type DB struct {
	*sql.DB
}

// Config holds database configuration
type Config struct {
	DataSourceName  string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// New creates a new database connection with proper configuration
func New(cfg Config) (*DB, error) {
	// Use default config if not provided
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 25
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 5
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = time.Hour
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(cfg.DataSourceName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", cfg.DataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// NewSimple creates a database connection with default config
func NewSimple(dataSourceName string) (*DB, error) {
	return New(Config{DataSourceName: dataSourceName})
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}

// GetUnderlyingDB returns the *database.DB itself
func (db *DB) GetUnderlyingDB() *DB {
	return db
}

// Migrate runs database migrations
func (db *DB) Migrate() error {
	// Create schema_migrations table if it doesn't exist
	migrationTableSQL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`

	if _, err := db.Exec(migrationTableSQL); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// For now, we'll run our initial migration directly
	// In a real system, you'd read migration files from disk
	initialMigration := `
	-- Create questions table
	CREATE TABLE IF NOT EXISTS questions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		text TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT 'general',
		last_used_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Create submissions table  
	CREATE TABLE IF NOT EXISTS submissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		question_id INTEGER NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (question_id) REFERENCES questions(id)
	);

	-- Create newsletter_issues table
	CREATE TABLE IF NOT EXISTS newsletter_issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		published_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(initialMigration); err != nil {
		return fmt.Errorf("failed to run initial migration: %w", err)
	}

	// Mark migration as applied
	if _, err := db.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (1)"); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// Run migration 2: Make question_id nullable
	var hasNullableMigration int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&hasNullableMigration); err != nil {
		return fmt.Errorf("failed to check migration 2: %w", err)
	}

	if hasNullableMigration == 0 {
		nullableMigration := `
		-- Make question_id nullable to support general news submissions
		-- SQLite doesn't support ALTER COLUMN directly, so we need to recreate the table

		-- Create new submissions table with nullable question_id
		CREATE TABLE submissions_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			question_id INTEGER, -- Now nullable
			content TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (question_id) REFERENCES questions(id)
		);

		-- Copy existing data
		INSERT INTO submissions_new (id, user_id, question_id, content, created_at)
		SELECT id, user_id, question_id, content, created_at FROM submissions;

		-- Drop old table and rename new one
		DROP TABLE submissions;
		ALTER TABLE submissions_new RENAME TO submissions;`

		if _, err := db.Exec(nullableMigration); err != nil {
			return fmt.Errorf("failed to run migration 2: %w", err)
		}

		// Mark migration as applied
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (2)"); err != nil {
			return fmt.Errorf("failed to record migration 2: %w", err)
		}
	}

	// Run migration 3: Add processed_articles table for AI-generated content
	var hasProcessedArticlesMigration int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 3").Scan(&hasProcessedArticlesMigration); err != nil {
		return fmt.Errorf("failed to check migration 3: %w", err)
	}

	if hasProcessedArticlesMigration == 0 {
		processedArticlesMigration := `
		-- Migration 3: Add processed_articles table for AI-generated content
		CREATE TABLE IF NOT EXISTS processed_articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			submission_id INTEGER NOT NULL,
			newsletter_issue_id INTEGER,
			
			-- AI Processing data
			journalist_type TEXT NOT NULL,
			processed_content TEXT,
			processing_prompt TEXT,
			
			-- Template formatting (separate from content)
			template_format TEXT NOT NULL DEFAULT 'column',
			
			-- Manual retry system
			processing_status TEXT NOT NULL DEFAULT 'pending',
			error_message TEXT,
			retry_count INTEGER NOT NULL DEFAULT 0,
			
			-- Metadata
			word_count INTEGER NOT NULL DEFAULT 0,
			processed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			
			-- Foreign key constraints - NO CASCADE DELETE to preserve archives
			FOREIGN KEY (submission_id) REFERENCES submissions(id),
			FOREIGN KEY (newsletter_issue_id) REFERENCES newsletter_issues(id)
		);

		-- Indexes for common query patterns
		CREATE INDEX IF NOT EXISTS idx_processed_articles_submission_id ON processed_articles(submission_id);
		CREATE INDEX IF NOT EXISTS idx_processed_articles_status ON processed_articles(processing_status);
		CREATE INDEX IF NOT EXISTS idx_processed_articles_newsletter_issue ON processed_articles(newsletter_issue_id);`

		if _, err := db.Exec(processedArticlesMigration); err != nil {
			return fmt.Errorf("failed to run migration 3: %w", err)
		}

		// Mark migration as applied
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (3)"); err != nil {
			return fmt.Errorf("failed to record migration 3: %w", err)
		}
	}

	// Run migration 4: Add weekly newsletter automation tables
	var hasWeeklyAutomationMigration int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 4").Scan(&hasWeeklyAutomationMigration); err != nil {
		return fmt.Errorf("failed to check migration 4: %w", err)
	}

	if hasWeeklyAutomationMigration == 0 {
		weeklyAutomationMigration := `
		-- Migration 4: Add weekly newsletter automation tables
		
		-- Enhance newsletter_issues table for weekly automation
		ALTER TABLE newsletter_issues ADD COLUMN week_number INTEGER;
		ALTER TABLE newsletter_issues ADD COLUMN year INTEGER;
		ALTER TABLE newsletter_issues ADD COLUMN status TEXT NOT NULL DEFAULT 'draft';
		ALTER TABLE newsletter_issues ADD COLUMN publication_date DATETIME;

		-- Create person_assignments table for weekly content assignments
		CREATE TABLE IF NOT EXISTS person_assignments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id INTEGER NOT NULL,
			person_id TEXT NOT NULL,
			content_type TEXT NOT NULL,
			question_id INTEGER,
			submission_id INTEGER,
			assigned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			
			FOREIGN KEY (issue_id) REFERENCES newsletter_issues(id),
			FOREIGN KEY (question_id) REFERENCES questions(id),
			FOREIGN KEY (submission_id) REFERENCES submissions(id)
		);

		-- Create body_mind_questions table for anonymous wellness question pool
		CREATE TABLE IF NOT EXISTS body_mind_questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			question_text TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'wellness',
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			used_at DATETIME
		);

		-- Create person_rotation_history table for intelligent assignment tracking  
		CREATE TABLE IF NOT EXISTS person_rotation_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			person_id TEXT NOT NULL,
			content_type TEXT NOT NULL,
			week_number INTEGER NOT NULL,
			year INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		-- Indexes for performance
		CREATE INDEX IF NOT EXISTS idx_newsletter_issues_week_year ON newsletter_issues(week_number, year);
		CREATE INDEX IF NOT EXISTS idx_newsletter_issues_status ON newsletter_issues(status);
		CREATE INDEX IF NOT EXISTS idx_person_assignments_issue_id ON person_assignments(issue_id);
		CREATE INDEX IF NOT EXISTS idx_person_assignments_person_id ON person_assignments(person_id);
		CREATE INDEX IF NOT EXISTS idx_body_mind_questions_status ON body_mind_questions(status);
		CREATE INDEX IF NOT EXISTS idx_person_rotation_history_person_type ON person_rotation_history(person_id, content_type);
		CREATE INDEX IF NOT EXISTS idx_person_rotation_history_week_year ON person_rotation_history(week_number, year);`

		if _, err := db.Exec(weeklyAutomationMigration); err != nil {
			return fmt.Errorf("failed to run migration 4: %w", err)
		}

		// Mark migration as applied
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (4)"); err != nil {
			return fmt.Errorf("failed to record migration 4: %w", err)
		}
	}

	return nil
}

// CreateNewsSubmission creates a news submission without a specific question
func (db *DB) CreateNewsSubmission(userID, content string) (int, error) {
	result, err := db.Exec(
		"INSERT INTO submissions (user_id, question_id, content) VALUES (?, NULL, ?)",
		userID, content,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create news submission: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get news submission ID: %w", err)
	}

	return int(id), nil
}

// CreateQuestion creates a new question and returns its ID
func (db *DB) CreateQuestion(text, category string) (int, error) {
	result, err := db.Exec(
		"INSERT INTO questions (text, category) VALUES (?, ?)",
		text, category,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create question: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get question ID: %w", err)
	}

	return int(id), nil
}

// CreateSubmission creates a new submission and returns its ID
func (db *DB) CreateSubmission(submission *Submission) (int, error) {
	result, err := db.Exec(
		"INSERT INTO submissions (user_id, question_id, content) VALUES (?, ?, ?)",
		submission.UserID, submission.QuestionID, submission.Content,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create submission: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get submission ID: %w", err)
	}

	return int(id), nil
}

// GetSubmission retrieves a submission by ID
func (db *DB) GetSubmission(id int) (*Submission, error) {
	var submission Submission
	var questionID sql.NullInt64

	err := db.QueryRow(
		"SELECT id, user_id, question_id, content, created_at FROM submissions WHERE id = ?",
		id,
	).Scan(&submission.ID, &submission.UserID, &questionID, &submission.Content, &submission.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("submission not found: %w", err)
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

// ListSubmissions retrieves all submissions
func (db *DB) ListSubmissions() ([]*Submission, error) {
	rows, err := db.Query(
		"SELECT id, user_id, question_id, content, created_at FROM submissions ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list submissions: %w", err)
	}
	defer rows.Close()

	var submissions []*Submission
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

		submissions = append(submissions, &submission)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating submissions: %w", err)
	}

	return submissions, nil
}

// DeleteSubmission deletes a submission by ID
func (db *DB) DeleteSubmission(id int) error {
	result, err := db.Exec("DELETE FROM submissions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete submission: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("submission not found")
	}

	return nil
}
