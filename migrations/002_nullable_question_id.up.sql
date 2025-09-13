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
ALTER TABLE submissions_new RENAME TO submissions;