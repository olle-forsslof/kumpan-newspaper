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

	return nil
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
	err := db.QueryRow(
		"SELECT id, user_id, question_id, content, created_at FROM submissions WHERE id = ?",
		id,
	).Scan(&submission.ID, &submission.UserID, &submission.QuestionID, &submission.Content, &submission.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("submission not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get submission: %w", err)
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
		err := rows.Scan(&submission.ID, &submission.UserID, &submission.QuestionID, &submission.Content, &submission.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan submission: %w", err)
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
