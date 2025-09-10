-- Drop tables in reverse order to handle foreign key constraints
DROP TABLE IF EXISTS schema_migrations;
DROP TABLE IF EXISTS newsletter_issues;
DROP TABLE IF EXISTS submissions;
DROP TABLE IF EXISTS questions;
