-- Publish all existing draft newsletters
-- This is a one-time migration to mark all previously created draft issues as published

UPDATE newsletter_issues 
SET status = 'published', 
    published_at = publication_date 
WHERE status = 'draft' OR status IS NULL;
