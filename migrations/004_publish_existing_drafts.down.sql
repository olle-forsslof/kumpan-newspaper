-- Rollback: Mark all issues as draft again
-- This reverses the publish operation

UPDATE newsletter_issues 
SET status = 'draft', 
    published_at = NULL 
WHERE status = 'published';
