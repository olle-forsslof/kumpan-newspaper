package database

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateWeeklyNewsletterIssue creates a new newsletter issue for the specified week
func (db *DB) CreateWeeklyNewsletterIssue(weekNumber, year int) (*WeeklyNewsletterIssue, error) {
	// Calculate publication date (Thursday of the given week)
	publicationDate := getThursdayOfWeek(weekNumber, year)

	query := `
		INSERT INTO newsletter_issues (
			week_number, year, title, content, status, publication_date
		) VALUES (?, ?, ?, ?, ?, ?)`

	title := fmt.Sprintf("Week %d Newsletter - %d", weekNumber, year)

	result, err := db.Exec(query,
		weekNumber,
		year,
		title,
		"", // content starts empty
		IssueStatusDraft,
		publicationDate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create weekly newsletter issue: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get newsletter issue ID: %w", err)
	}

	// Return the created issue
	return db.GetWeeklyNewsletterIssue(int(id))
}

// GetWeeklyNewsletterIssue retrieves a newsletter issue by ID
func (db *DB) GetWeeklyNewsletterIssue(id int) (*WeeklyNewsletterIssue, error) {
	query := `
		SELECT id, week_number, year, title, content, status, publication_date, published_at, created_at
		FROM newsletter_issues 
		WHERE id = ?`

	row := db.QueryRow(query, id)

	var issue WeeklyNewsletterIssue
	var publishedAt sql.NullTime
	var weekNumber sql.NullInt64
	var year sql.NullInt64
	var status sql.NullString

	err := row.Scan(
		&issue.ID,
		&weekNumber,
		&year,
		&issue.Title,
		&issue.Content,
		&status,
		&issue.PublicationDate,
		&publishedAt,
		&issue.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("newsletter issue with ID %d not found", id)
		}
		return nil, fmt.Errorf("failed to get newsletter issue: %w", err)
	}

	// Handle nullable fields from migration 4
	if weekNumber.Valid {
		issue.WeekNumber = int(weekNumber.Int64)
	}
	if year.Valid {
		issue.Year = int(year.Int64)
	}
	if status.Valid {
		issue.Status = NewsletterIssueStatus(status.String)
	} else {
		issue.Status = IssueStatusDraft
	}
	if publishedAt.Valid {
		issue.PublishedAt = &publishedAt.Time
	}

	return &issue, nil
}

// GetOrCreateWeeklyIssue gets the newsletter issue for a specific week, creating it if it doesn't exist
func (db *DB) GetOrCreateWeeklyIssue(weekNumber, year int) (*WeeklyNewsletterIssue, error) {
	// Try to find existing issue first
	query := `
		SELECT id FROM newsletter_issues 
		WHERE week_number = ? AND year = ?
		LIMIT 1`

	var existingID int
	err := db.QueryRow(query, weekNumber, year).Scan(&existingID)
	if err == nil {
		// Issue exists, return it
		return db.GetWeeklyNewsletterIssue(existingID)
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to check for existing issue: %w", err)
	}

	// Issue doesn't exist, create it
	return db.CreateWeeklyNewsletterIssue(weekNumber, year)
}

// CreatePersonAssignment creates a new person assignment for a newsletter issue
func (db *DB) CreatePersonAssignment(assignment PersonAssignment) (int, error) {
	// Validate the assignment before inserting
	if err := assignment.Validate(); err != nil {
		return 0, fmt.Errorf("validation failed: %w", err)
	}

	// Check for existing assignments for this user in the same issue
	checkQuery := `
		SELECT COUNT(*) 
		FROM person_assignments 
		WHERE issue_id = ? AND person_id = ?`

	var count int
	err := db.QueryRow(checkQuery, assignment.IssueID, assignment.PersonID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to check existing assignments: %w", err)
	}

	if count > 0 {
		return 0, fmt.Errorf("user %s already has an assignment for this week (issue ID: %d)",
			assignment.PersonID, assignment.IssueID)
	}

	query := `
		INSERT INTO person_assignments (
			issue_id, person_id, content_type, question_id, submission_id, assigned_at
		) VALUES (?, ?, ?, ?, ?, ?)`

	result, err := db.Exec(query,
		assignment.IssueID,
		assignment.PersonID,
		assignment.ContentType,
		assignment.QuestionID,
		assignment.SubmissionID,
		assignment.AssignedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create person assignment: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get assignment ID: %w", err)
	}

	return int(id), nil
}

// GetPersonAssignmentsByIssue retrieves all person assignments for a specific newsletter issue
func (db *DB) GetPersonAssignmentsByIssue(issueID int) ([]PersonAssignment, error) {
	query := `
		SELECT id, issue_id, person_id, content_type, question_id, submission_id, assigned_at, created_at
		FROM person_assignments 
		WHERE issue_id = ?
		ORDER BY created_at ASC`

	rows, err := db.Query(query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query person assignments: %w", err)
	}
	defer rows.Close()

	return db.scanPersonAssignments(rows)
}

// GetActiveAssignmentByUser retrieves a person's active assignment for current week by content type
func (db *DB) GetActiveAssignmentByUser(userID string, contentType ContentType) (*PersonAssignment, error) {
	// Get current week's issue
	now := time.Now()
	year, week := now.ISOWeek()

	issue, err := db.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		return nil, fmt.Errorf("failed to get current week issue: %w", err)
	}

	query := `
		SELECT id, issue_id, person_id, content_type, question_id, submission_id, assigned_at, created_at
		FROM person_assignments 
		WHERE issue_id = ? AND person_id = ? AND content_type = ?
		LIMIT 1`

	row := db.QueryRow(query, issue.ID, userID, contentType)
	assignment, err := db.scanSinglePersonAssignment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no active assignment found for user %s with content type %s", userID, contentType)
		}
		return nil, fmt.Errorf("failed to get active assignment: %w", err)
	}

	return assignment, nil
}

// GetActiveAssignmentsByUser retrieves all assignments for a user in the current week
func (db *DB) GetActiveAssignmentsByUser(userID string) ([]PersonAssignment, error) {
	// Get current week's issue
	now := time.Now()
	year, week := now.ISOWeek()

	issue, err := db.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		return nil, fmt.Errorf("failed to get current week issue: %w", err)
	}

	return db.GetAssignmentsByUserAndIssue(userID, issue.ID)
}

// GetAssignmentsByUserAndIssue retrieves all assignments for a user in a specific issue
func (db *DB) GetAssignmentsByUserAndIssue(userID string, issueID int) ([]PersonAssignment, error) {
	query := `
		SELECT id, issue_id, person_id, content_type, question_id, submission_id, assigned_at, created_at
		FROM person_assignments 
		WHERE issue_id = ? AND person_id = ?
		ORDER BY created_at ASC`

	rows, err := db.Query(query, issueID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user assignments: %w", err)
	}
	defer rows.Close()

	return db.scanPersonAssignments(rows)
}

// DeletePersonAssignmentsByUser deletes all assignments for a user in a specific issue
func (db *DB) DeletePersonAssignmentsByUser(userID string, issueID int) error {
	query := `DELETE FROM person_assignments WHERE person_id = ? AND issue_id = ?`

	result, err := db.Exec(query, userID, issueID)
	if err != nil {
		return fmt.Errorf("failed to delete person assignments: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// It's ok if no rows were affected - user might not have had assignments
	_ = rowsAffected

	return nil
}

// DeleteAllPersonAssignmentsByUser deletes ALL assignments for a user across all issues
func (db *DB) DeleteAllPersonAssignmentsByUser(userID string) error {
	query := `DELETE FROM person_assignments WHERE person_id = ?`

	result, err := db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete all person assignments: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// It's ok if no rows were affected - user might not have had assignments
	_ = rowsAffected

	return nil
}

// GetAssignmentBySubmissionID finds the assignment that a submission is linked to
func (db *DB) GetAssignmentBySubmissionID(submissionID int) (*PersonAssignment, error) {
	query := `
		SELECT id, issue_id, person_id, content_type, question_id, submission_id, assigned_at, created_at
		FROM person_assignments 
		WHERE submission_id = ?
		LIMIT 1`

	row := db.QueryRow(query, submissionID)
	assignment, err := db.scanSinglePersonAssignment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no assignment found for submission %d", submissionID)
		}
		return nil, fmt.Errorf("failed to get assignment by submission ID: %w", err)
	}

	return assignment, nil
}

// LinkSubmissionToAssignment links a submission to an existing assignment
func (db *DB) LinkSubmissionToAssignment(assignmentID, submissionID int) error {
	query := `
		UPDATE person_assignments 
		SET submission_id = ?
		WHERE id = ?`

	result, err := db.Exec(query, submissionID, assignmentID)
	if err != nil {
		return fmt.Errorf("failed to link submission to assignment: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("assignment with ID %d not found", assignmentID)
	}

	return nil
}

// GetPersonAssignmentByID retrieves a specific person assignment by ID
func (db *DB) GetPersonAssignmentByID(assignmentID int) (*PersonAssignment, error) {
	query := `
		SELECT id, issue_id, person_id, content_type, question_id, submission_id, assigned_at, created_at
		FROM person_assignments 
		WHERE id = ?`

	row := db.QueryRow(query, assignmentID)
	assignment, err := db.scanSinglePersonAssignment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("assignment with ID %d not found", assignmentID)
		}
		return nil, fmt.Errorf("failed to get assignment: %w", err)
	}

	return assignment, nil
}

// CreateBodyMindQuestion creates a new anonymous body/mind question for the pool
func (db *DB) CreateBodyMindQuestion(questionText, category string) (int, error) {
	query := `
		INSERT INTO body_mind_questions (question_text, category, status)
		VALUES (?, ?, 'active')`

	result, err := db.Exec(query, questionText, category)
	if err != nil {
		return 0, fmt.Errorf("failed to create body/mind question: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get body/mind question ID: %w", err)
	}

	return int(id), nil
}

// GetActiveBodyMindQuestions retrieves all active questions from the anonymous pool
func (db *DB) GetActiveBodyMindQuestions() ([]BodyMindQuestion, error) {
	query := `
		SELECT id, question_text, category, status, created_at, used_at
		FROM body_mind_questions 
		WHERE status = 'active'
		ORDER BY created_at ASC`

	return db.queryBodyMindQuestions(query)
}

// GetBodyMindQuestionsByCategory retrieves active questions by category
func (db *DB) GetBodyMindQuestionsByCategory(category string) ([]BodyMindQuestion, error) {
	query := `
		SELECT id, question_text, category, status, created_at, used_at
		FROM body_mind_questions 
		WHERE status = 'active' AND category = ?
		ORDER BY created_at ASC`

	return db.queryBodyMindQuestions(query, category)
}

// MarkBodyMindQuestionUsed marks a question as used with timestamp
func (db *DB) MarkBodyMindQuestionUsed(questionID int) error {
	query := `
		UPDATE body_mind_questions 
		SET status = 'used', used_at = ?
		WHERE id = ?`

	result, err := db.Exec(query, time.Now(), questionID)
	if err != nil {
		return fmt.Errorf("failed to mark body/mind question as used: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("body/mind question with ID %d not found", questionID)
	}

	return nil
}

// AddPersonRotationHistory adds an entry to track assignment history
func (db *DB) AddPersonRotationHistory(personID string, contentType ContentType, weekNumber, year int) error {
	query := `
		INSERT INTO person_rotation_history (person_id, content_type, week_number, year)
		VALUES (?, ?, ?, ?)`

	_, err := db.Exec(query, personID, contentType, weekNumber, year)
	if err != nil {
		return fmt.Errorf("failed to add person rotation history: %w", err)
	}

	return nil
}

// GetPersonRotationHistory retrieves recent assignment history for intelligent rotation
func (db *DB) GetPersonRotationHistory(personID string, contentType ContentType, weeksBack int) ([]PersonRotationHistory, error) {
	// Calculate the week range to check
	currentWeek, currentYear := getCurrentWeekAndYear()
	startWeek := currentWeek - weeksBack
	startYear := currentYear

	// Handle year boundary (simplified - assumes we don't go back more than 1 year)
	if startWeek <= 0 {
		startWeek += 52
		startYear--
	}

	query := `
		SELECT id, person_id, content_type, week_number, year, created_at
		FROM person_rotation_history 
		WHERE person_id = ? AND content_type = ? 
		AND ((year = ? AND week_number >= ?) OR (year = ? AND week_number <= ?))
		ORDER BY year DESC, week_number DESC`

	rows, err := db.Query(query, personID, contentType, startYear, startWeek, currentYear, currentWeek)
	if err != nil {
		return nil, fmt.Errorf("failed to query person rotation history: %w", err)
	}
	defer rows.Close()

	var history []PersonRotationHistory
	for rows.Next() {
		var entry PersonRotationHistory
		err := rows.Scan(
			&entry.ID,
			&entry.PersonID,
			&entry.ContentType,
			&entry.WeekNumber,
			&entry.Year,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rotation history: %w", err)
		}

		history = append(history, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rotation history: %w", err)
	}

	return history, nil
}

// Helper functions

// scanPersonAssignments scans rows into PersonAssignment structs
func (db *DB) scanPersonAssignments(rows *sql.Rows) ([]PersonAssignment, error) {
	var assignments []PersonAssignment
	for rows.Next() {
		assignment, err := db.scanSinglePersonAssignment(rows)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, *assignment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over assignments: %w", err)
	}

	return assignments, nil
}

// scanSinglePersonAssignment scans a single row into a PersonAssignment struct
func (db *DB) scanSinglePersonAssignment(scanner interface{}) (*PersonAssignment, error) {
	var assignment PersonAssignment
	var questionID sql.NullInt64
	var submissionID sql.NullInt64

	var err error
	switch s := scanner.(type) {
	case *sql.Rows:
		err = s.Scan(
			&assignment.ID,
			&assignment.IssueID,
			&assignment.PersonID,
			&assignment.ContentType,
			&questionID,
			&submissionID,
			&assignment.AssignedAt,
			&assignment.CreatedAt,
		)
	case *sql.Row:
		err = s.Scan(
			&assignment.ID,
			&assignment.IssueID,
			&assignment.PersonID,
			&assignment.ContentType,
			&questionID,
			&submissionID,
			&assignment.AssignedAt,
			&assignment.CreatedAt,
		)
	default:
		return nil, fmt.Errorf("unsupported scanner type")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to scan person assignment: %w", err)
	}

	// Handle nullable fields with clean pointer creation
	if questionID.Valid {
		qid := int(questionID.Int64)
		assignment.QuestionID = &qid
	}
	if submissionID.Valid {
		sid := int(submissionID.Int64)
		assignment.SubmissionID = &sid
	}

	return &assignment, nil
}

// queryBodyMindQuestions executes a query and returns the results
func (db *DB) queryBodyMindQuestions(query string, args ...interface{}) ([]BodyMindQuestion, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query body/mind questions: %w", err)
	}
	defer rows.Close()

	return db.scanBodyMindQuestions(rows)
}

// scanBodyMindQuestions scans rows into BodyMindQuestion structs
func (db *DB) scanBodyMindQuestions(rows *sql.Rows) ([]BodyMindQuestion, error) {
	var questions []BodyMindQuestion
	for rows.Next() {
		var question BodyMindQuestion
		var usedAt sql.NullTime

		err := rows.Scan(
			&question.ID,
			&question.QuestionText,
			&question.Category,
			&question.Status,
			&question.CreatedAt,
			&usedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan body/mind question: %w", err)
		}

		// Handle nullable used_at field
		if usedAt.Valid {
			question.UsedAt = &usedAt.Time
		}

		questions = append(questions, question)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over body/mind questions: %w", err)
	}

	return questions, nil
}

// getThursdayOfWeek calculates the Thursday of a given ISO week
func getThursdayOfWeek(weekNumber, year int) time.Time {
	// January 4th is always in week 1 of ISO week numbering
	jan4 := time.Date(year, 1, 4, 9, 30, 0, 0, time.UTC)

	// Find the Monday of week 1
	daysFromMonday := int(jan4.Weekday()) - 1
	if daysFromMonday < 0 {
		daysFromMonday = 6 // Sunday becomes 6
	}

	mondayOfWeek1 := jan4.AddDate(0, 0, -daysFromMonday)

	// Calculate the Monday of the target week
	targetMonday := mondayOfWeek1.AddDate(0, 0, (weekNumber-1)*7)

	// Thursday is 3 days after Monday, with 9:30 AM publication time
	thursday := targetMonday.AddDate(0, 0, 3)
	return thursday
}

// getCurrentWeekAndYear returns the current ISO week number and year
func getCurrentWeekAndYear() (int, int) {
	now := time.Now()
	year, week := now.ISOWeek()
	return week, year
}
