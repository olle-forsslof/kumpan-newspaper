-- Weekly Newsletter Automation Schema
-- Add columns to existing newsletter_issues table and create new tables for weekly automation

-- Extend newsletter_issues table with missing columns for weekly automation
ALTER TABLE newsletter_issues ADD COLUMN week_number INTEGER;
ALTER TABLE newsletter_issues ADD COLUMN year INTEGER;  
ALTER TABLE newsletter_issues ADD COLUMN publication_date DATETIME;
ALTER TABLE newsletter_issues ADD COLUMN status TEXT DEFAULT 'draft';

-- Person assignments table for tracking weekly content assignments
CREATE TABLE person_assignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id INTEGER NOT NULL,
    person_id TEXT NOT NULL, -- Slack user ID
    content_type TEXT NOT NULL, -- 'feature', 'general', 'body_mind'
    question_id INTEGER, -- Optional reference to questions table
    assigned_date DATETIME DEFAULT CURRENT_TIMESTAMP,
    submission_date DATETIME,
    status TEXT DEFAULT 'assigned', -- 'assigned', 'submitted', 'overdue'
    FOREIGN KEY (issue_id) REFERENCES newsletter_issues(id),
    FOREIGN KEY (question_id) REFERENCES questions(id)
);

-- Anonymous body/mind question pool (NO user tracking for privacy)
CREATE TABLE body_mind_questions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    question_text TEXT NOT NULL,
    category TEXT NOT NULL, -- 'wellness', 'mental_health', 'work_life_balance'
    status TEXT DEFAULT 'active', -- 'active', 'used', 'retired'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    used_at DATETIME,
    usage_count INTEGER DEFAULT 0
);

-- Person rotation history for intelligent assignment algorithm
CREATE TABLE person_rotation_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id TEXT NOT NULL, -- Slack user ID
    last_feature_assignment DATETIME,
    last_general_assignment DATETIME,
    feature_assignment_count INTEGER DEFAULT 0,
    general_assignment_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for performance
CREATE INDEX idx_person_assignments_issue_id ON person_assignments(issue_id);
CREATE INDEX idx_person_assignments_person_id ON person_assignments(person_id);
CREATE INDEX idx_person_assignments_content_type ON person_assignments(content_type);
CREATE INDEX idx_body_mind_questions_status ON body_mind_questions(status);
CREATE INDEX idx_body_mind_questions_category ON body_mind_questions(category);
CREATE INDEX idx_person_rotation_history_person_id ON person_rotation_history(person_id);

-- Insert some sample anonymous body/mind questions to bootstrap the pool
INSERT INTO body_mind_questions (question_text, category) VALUES
('What''s one small habit you''ve developed recently that makes you feel more energized during the workday?', 'wellness'),
('How do you mentally transition from work mode to personal time?', 'work_life_balance'),
('What''s a simple mindfulness practice that works for you during busy periods?', 'mental_health'),
('What''s one thing you do to maintain good posture or physical comfort while working?', 'wellness'),
('How do you handle stress when multiple deadlines are approaching?', 'mental_health'),
('What''s your favorite way to take a mental break during the workday?', 'work_life_balance'),
('What''s one piece of advice you''d give to someone struggling with work-life balance?', 'work_life_balance'),
('How do you stay motivated when working on repetitive tasks?', 'mental_health'),
('What''s a healthy snack or drink that helps you stay focused?', 'wellness'),
('How do you create boundaries between work and personal time when working from home?', 'work_life_balance');