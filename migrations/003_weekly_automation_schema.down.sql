-- Rollback Weekly Newsletter Automation Schema

-- Drop indexes
DROP INDEX IF EXISTS idx_person_rotation_history_person_id;
DROP INDEX IF EXISTS idx_body_mind_questions_category;
DROP INDEX IF EXISTS idx_body_mind_questions_status;
DROP INDEX IF EXISTS idx_person_assignments_content_type;
DROP INDEX IF EXISTS idx_person_assignments_person_id;
DROP INDEX IF EXISTS idx_person_assignments_issue_id;

-- Drop tables (in reverse dependency order)
DROP TABLE IF EXISTS person_rotation_history;
DROP TABLE IF EXISTS body_mind_questions;
DROP TABLE IF EXISTS person_assignments;

-- Remove added columns from newsletter_issues table
-- Note: SQLite doesn't support DROP COLUMN directly, so we'd need to recreate the table
-- For now, we'll leave the columns as they won't break existing functionality
-- ALTER TABLE newsletter_issues DROP COLUMN status;
-- ALTER TABLE newsletter_issues DROP COLUMN publication_date; 
-- ALTER TABLE newsletter_issues DROP COLUMN year;
-- ALTER TABLE newsletter_issues DROP COLUMN week_number;