package database

import (
	"time"
)

// Submission represents a newsletter submission from a team member
type Submission struct {
	ID         int       `json:"id"`
	UserID     string    `json:"user_id"`
	QuestionID int       `json:"question_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// Question represents a prompt question for newsletter submissions
type Question struct {
	ID         int        `json:"id"`
	Text       string     `json:"text"`
	Category   string     `json:"category"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewsletterIssue represents a generated newsletter
type NewsletterIssue struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}
