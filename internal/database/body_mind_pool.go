package database

import (
	"fmt"
	"time"
)

// BodyMindPoolManager handles the anonymous wellness question pool
type BodyMindPoolManager struct {
	db *DB
}

// NewBodyMindPoolManager creates a new pool manager instance
func NewBodyMindPoolManager(db *DB) *BodyMindPoolManager {
	return &BodyMindPoolManager{db: db}
}

// PoolStatus represents the current status of the body/mind question pool
type PoolStatus struct {
	TotalActive       int                  `json:"total_active"`
	CategoryBreakdown map[string]int       `json:"category_breakdown"`
	RecentActivity    []RecentActivityItem `json:"recent_activity"`
	LowPoolWarning    bool                 `json:"low_pool_warning"`
	RecommendedAction string               `json:"recommended_action"`
}

// RecentActivityItem represents a recent addition to the pool
type RecentActivityItem struct {
	Category string `json:"category"`
	DaysAgo  int    `json:"days_ago"`
	Action   string `json:"action"` // "added", "used"
}

// GetPoolStatus returns comprehensive status of the anonymous question pool
func (pm *BodyMindPoolManager) GetPoolStatus() (*PoolStatus, error) {
	// Get all active questions
	activeQuestions, err := pm.db.GetActiveBodyMindQuestions()
	if err != nil {
		return nil, fmt.Errorf("failed to get active questions: %w", err)
	}

	// Build category breakdown
	categoryBreakdown := make(map[string]int)
	validCategories := []string{"wellness", "mental_health", "work_life_balance"}

	for _, category := range validCategories {
		categoryBreakdown[category] = 0
	}

	for _, question := range activeQuestions {
		categoryBreakdown[question.Category]++
	}

	// Get recent activity
	recentActivity, err := pm.getRecentActivity()
	if err != nil {
		return nil, fmt.Errorf("failed to get recent activity: %w", err)
	}

	// Determine if pool is low and what action to recommend
	totalActive := len(activeQuestions)
	lowPoolWarning := totalActive < 8

	var recommendedAction string
	if totalActive == 0 {
		recommendedAction = "🚨 URGENT: Pool is empty! Broadcast request immediately."
	} else if totalActive < 5 {
		recommendedAction = "⚠️ Pool critically low! Consider broadcast request."
	} else if totalActive < 8 {
		recommendedAction = "⚠️ Pool getting low! Consider broadcast when < 8 questions remain."
	} else {
		recommendedAction = "✅ Pool levels healthy."
	}

	return &PoolStatus{
		TotalActive:       totalActive,
		CategoryBreakdown: categoryBreakdown,
		RecentActivity:    recentActivity,
		LowPoolWarning:    lowPoolWarning,
		RecommendedAction: recommendedAction,
	}, nil
}

// SelectQuestionForNewsletter selects and marks a question for use in the newsletter
func (pm *BodyMindPoolManager) SelectQuestionForNewsletter() (*BodyMindQuestion, error) {
	// Get all active questions
	activeQuestions, err := pm.db.GetActiveBodyMindQuestions()
	if err != nil {
		return nil, fmt.Errorf("failed to get active questions: %w", err)
	}

	if len(activeQuestions) == 0 {
		return nil, fmt.Errorf("no active questions available in pool")
	}

	// Select the oldest question (FIFO - First In, First Out)
	selectedQuestion := activeQuestions[0]
	for _, question := range activeQuestions {
		if question.CreatedAt.Before(selectedQuestion.CreatedAt) {
			selectedQuestion = question
		}
	}

	// Mark the question as used
	err = pm.db.MarkBodyMindQuestionUsed(selectedQuestion.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark question as used: %w", err)
	}

	// Update the status for the returned object
	selectedQuestion.Status = "used"
	now := time.Now()
	selectedQuestion.UsedAt = &now

	return &selectedQuestion, nil
}

// AddQuestionToPool adds a new anonymous question to the pool
func (pm *BodyMindPoolManager) AddQuestionToPool(questionText, category string) (*BodyMindQuestion, error) {
	// Validate category
	validCategories := map[string]bool{
		"wellness":          true,
		"mental_health":     true,
		"work_life_balance": true,
	}

	if !validCategories[category] {
		return nil, fmt.Errorf("invalid category: %s", category)
	}

	// Create the question in database
	id, err := pm.db.CreateBodyMindQuestion(questionText, category)
	if err != nil {
		return nil, fmt.Errorf("failed to create question: %w", err)
	}

	// Return the created question
	questions, err := pm.db.GetActiveBodyMindQuestions()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve created question: %w", err)
	}

	for _, question := range questions {
		if question.ID == id {
			return &question, nil
		}
	}

	return nil, fmt.Errorf("created question not found")
}

// BulkAddQuestions adds multiple questions to the pool (useful for broadcast responses)
func (pm *BodyMindPoolManager) BulkAddQuestions(questions []struct {
	Text     string
	Category string
}) ([]BodyMindQuestion, error) {
	var addedQuestions []BodyMindQuestion
	var errors []string

	for i, q := range questions {
		question, err := pm.AddQuestionToPool(q.Text, q.Category)
		if err != nil {
			errors = append(errors, fmt.Sprintf("question %d: %v", i+1, err))
			continue
		}
		addedQuestions = append(addedQuestions, *question)
	}

	if len(errors) > 0 {
		return addedQuestions, fmt.Errorf("some questions failed to add: %v", errors)
	}

	return addedQuestions, nil
}

// GetPoolMetrics returns detailed metrics about the pool
func (pm *BodyMindPoolManager) GetPoolMetrics() (*PoolMetrics, error) {
	// Get pool status
	status, err := pm.GetPoolStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get pool status: %w", err)
	}

	// Get usage statistics
	usageStats, err := pm.getUsageStatistics()
	if err != nil {
		return nil, fmt.Errorf("failed to get usage statistics: %w", err)
	}

	return &PoolMetrics{
		PoolStatus:  *status,
		UsageStats:  usageStats,
		LastUpdated: time.Now(),
	}, nil
}

// PoolMetrics contains comprehensive pool metrics
type PoolMetrics struct {
	PoolStatus  PoolStatus `json:"pool_status"`
	UsageStats  UsageStats `json:"usage_stats"`
	LastUpdated time.Time  `json:"last_updated"`
}

// UsageStats tracks question usage patterns
type UsageStats struct {
	QuestionsUsedThisWeek   int     `json:"questions_used_this_week"`
	QuestionsUsedThisMonth  int     `json:"questions_used_this_month"`
	AverageQuestionsPerWeek float64 `json:"average_questions_per_week"`
	MostUsedCategory        string  `json:"most_used_category"`
}

// getRecentActivity retrieves recent activity for the pool status
func (pm *BodyMindPoolManager) getRecentActivity() ([]RecentActivityItem, error) {
	// Query for recent additions and usage
	query := `
		SELECT category, created_at, used_at FROM body_mind_questions 
		WHERE created_at >= date('now', '-30 days') OR used_at >= date('now', '-30 days')
		ORDER BY COALESCE(used_at, created_at) DESC
		LIMIT 10`

	rows, err := pm.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent activity: %w", err)
	}
	defer rows.Close()

	var activity []RecentActivityItem
	now := time.Now()

	for rows.Next() {
		var category string
		var createdAt time.Time
		var usedAt *time.Time

		err := rows.Scan(&category, &createdAt, &usedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan activity row: %w", err)
		}

		// Determine if this was a recent usage or addition
		if usedAt != nil {
			// This question was used recently
			daysAgo := int(now.Sub(*usedAt).Hours() / 24)
			activity = append(activity, RecentActivityItem{
				Category: category,
				DaysAgo:  daysAgo,
				Action:   "used",
			})
		} else {
			// This question was added recently
			daysAgo := int(now.Sub(createdAt).Hours() / 24)
			activity = append(activity, RecentActivityItem{
				Category: category,
				DaysAgo:  daysAgo,
				Action:   "added",
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over activity: %w", err)
	}

	return activity, nil
}

// getUsageStatistics calculates usage patterns for the pool
func (pm *BodyMindPoolManager) getUsageStatistics() (UsageStats, error) {
	stats := UsageStats{}

	// Questions used this week
	weekQuery := `
		SELECT COUNT(*) FROM body_mind_questions 
		WHERE used_at >= date('now', '-7 days')`

	err := pm.db.QueryRow(weekQuery).Scan(&stats.QuestionsUsedThisWeek)
	if err != nil {
		return stats, fmt.Errorf("failed to get weekly usage: %w", err)
	}

	// Questions used this month
	monthQuery := `
		SELECT COUNT(*) FROM body_mind_questions 
		WHERE used_at >= date('now', '-30 days')`

	err = pm.db.QueryRow(monthQuery).Scan(&stats.QuestionsUsedThisMonth)
	if err != nil {
		return stats, fmt.Errorf("failed to get monthly usage: %w", err)
	}

	// Average questions per week (over last 4 weeks)
	if stats.QuestionsUsedThisMonth > 0 {
		stats.AverageQuestionsPerWeek = float64(stats.QuestionsUsedThisMonth) / 4.0
	}

	// Most used category
	categoryQuery := `
		SELECT category, COUNT(*) as usage_count FROM body_mind_questions 
		WHERE used_at IS NOT NULL 
		GROUP BY category 
		ORDER BY usage_count DESC 
		LIMIT 1`

	var usageCount int
	err = pm.db.QueryRow(categoryQuery).Scan(&stats.MostUsedCategory, &usageCount)
	if err != nil {
		// If no questions have been used yet, default to wellness
		stats.MostUsedCategory = "wellness"
	}

	return stats, nil
}

// FormatPoolStatusForSlack formats pool status for Slack display
func (pm *BodyMindPoolManager) FormatPoolStatusForSlack(status *PoolStatus) string {
	message := "📊 *Body/Mind Question Pool Status*\n\n"

	message += fmt.Sprintf("*Available Questions:* %d\n", status.TotalActive)

	for category, count := range status.CategoryBreakdown {
		categoryDisplay := formatCategoryName(category)
		message += fmt.Sprintf("└─ %s: %d questions\n", categoryDisplay, count)
	}

	if len(status.RecentActivity) > 0 {
		message += "\n*Recent Activity:*\n"
		for _, activity := range status.RecentActivity[:min(3, len(status.RecentActivity))] {
			categoryDisplay := formatCategoryName(activity.Category)
			action := activity.Action
			if action == "added" {
				action = "question added"
			} else {
				action = "question used"
			}

			timeDisplay := formatDaysAgo(activity.DaysAgo)
			message += fmt.Sprintf("└─ %s: %s %s\n", timeDisplay, categoryDisplay, action)
		}
	}

	message += fmt.Sprintf("\n%s", status.RecommendedAction)

	return message
}

// Helper functions

func formatCategoryName(category string) string {
	switch category {
	case "wellness":
		return "Wellness"
	case "mental_health":
		return "Mental Health"
	case "work_life_balance":
		return "Work-Life Balance"
	default:
		return category
	}
}

func formatDaysAgo(days int) string {
	if days == 0 {
		return "Today"
	} else if days == 1 {
		return "1 day ago"
	} else if days < 7 {
		return fmt.Sprintf("%d days ago", days)
	} else {
		weeks := days / 7
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
