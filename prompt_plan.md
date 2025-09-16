# Prompt Plan

## Project: Company Newsletter Automation

**Goal**: Implement Phase 1 - Core Infrastructure (Go backend setup and Slack bot integration)
**Created**: Thu Sep 04 2025
**Updated**: Sat Sep 13 2025

---

## Implementation Prompts

### Prompt 1: Project Structure & Go Module Setup - [✅] COMPLETED

```
Initialize a new Go project for the company newsletter automation system.

Create the basic project structure with:
- Go module initialization
- Directory structure (cmd/, internal/, templates/, static/)
- Basic main.go with HTTP server setup
- Environment variable configuration
- Basic logging setup
- README.md with setup instructions

Requirements:
- Use Go modules for dependency management
- Follow standard Go project layout
- Include .gitignore for Go projects
- Set up structured logging (slog or similar)
- Basic HTTP server that responds to health checks

Expected outcome: Working Go project with proper structure and basic HTTP server running on configurable port
```

### Prompt 2: Database Schema & SQLite Integration - [✅] COMPLETED

```
Implement SQLite database integration with schema for newsletter submissions.

Create database layer with:
- SQLite database connection and initialization
- Schema migration system
- Tables for: submissions, questions, newsletter_issues
- Basic CRUD operations for submissions
- Database connection pooling and proper error handling
- SQL migration files

Requirements:
- Write tests first (TDD approach) for database operations
- Use proper SQL migrations
- Handle database errors gracefully
- Include database cleanup in tests
- Follow Go database/sql patterns

Expected outcome: Working database layer with migrations and tested CRUD operations for submissions
```

### Prompt 3: Slack Bot Framework Setup - [✅] COMPLETED

```
Implement basic Slack bot integration using slack-go library.

Create Slack bot functionality:
- Bot token configuration and authentication
- Basic slash command handling
- Message event processing
- Bot user detection and response logic
- Error handling for Slack API calls
- Configuration for bot permissions and scopes

Requirements:
- Write tests for Slack integration (use mocks where needed)
- Proper error handling and logging for Slack API
- Environment-based configuration
- Follow Slack best practices for bot development
- Ensure code builds and runs

Expected outcome: Working Slack bot that can receive and respond to basic commands and messages
```

### Prompt 4: Question Management System - [✅] COMPLETED

```
Implement the question rotation and scheduling system.

Create question management with:
- Predefined question pool stored in database
- Question rotation logic (avoid recent repeats)
- Scheduling system for different days (Monday/Wednesday prompts)
- Question categorization (personal, work, fun, etc.)
- Admin functionality to manage questions via code

Requirements:
- Build on previous database work
- Write tests for question rotation logic
- Ensure fair distribution of question types
- Follow existing code conventions
- Run linter before committing

Expected outcome: System that can select appropriate questions for scheduled Slack prompts with proper rotation
```

### Prompt 5: News Submission Database Storage (TDD) - [✅] COMPLETED

```
Implement complete news submission collection and database storage using Test-Driven Development.

Create submission handling with full TDD approach:
- News submissions without specific questions (general stories)
- Database schema migration for nullable question_id
- SubmissionManager interface with CRUD operations
- Slack bot integration for `/submit` commands  
- Admin commands for submission management
- Complete test coverage for all functionality

Requirements:
- Follow strict TDD methodology (Red-Green-Refactor cycles)
- Database migration to support nullable question references
- Comprehensive error handling and validation
- Admin authorization and security checks
- Integration tests covering end-to-end flows
- User-friendly Slack command responses

Expected outcome: Complete news submission system where users can submit stories via Slack, 
stored in database, with admin management interface - all implemented with 100% test coverage

Implementation completed with 4 full TDD cycles:
1. Database schema & nullable question_id support
2. SubmissionManager interface with full CRUD operations  
3. Slack bot integration with database storage
4. Admin submission management commands

Features delivered:
- `/pp submit [story]` - Users can submit news stories
- `/pp admin list-submissions` - Admins can view all submissions
- `/pp admin list-submissions [user_id]` - Filter by specific user
- Database storage with proper migrations and nullable relationships
- 15 comprehensive tests covering all functionality
- Security: Authorization checks for admin commands
```

### Prompt 6: Deployment & Production Setup - [✅] COMPLETED

```
Deploy the Slack bot to production using Docker and Coolify.

Set up production deployment:
- Dockerfile for containerized deployment
- Coolify configuration and SSL setup
- Environment variable management
- Slack app configuration and webhook setup
- Production database initialization
- Signature verification for security

Requirements:
- Proper SSL certificate handling in Alpine Linux
- Port mapping and routing configuration
- Environment-based configuration for production
- Slack webhook signature verification
- Debugging and troubleshooting deployment issues

Expected outcome: Fully deployed and functional Slack bot in production environment

Implementation completed with:
- Multi-stage Docker build with Alpine Linux
- Coolify deployment with proper port mapping
- SSL certificate configuration
- Slack signature verification implementation
- Production environment variable setup
- Successful bot deployment and testing
```

### Prompt 7: AI-Powered Content Processing System - [✅] COMPLETED

```
Implement Anthropic API integration for AI journalist content processing.

Create AI content processing with:
- Anthropic Go SDK integration with proper authentication
- Journalist personality system with distinct writing styles
- AI service interface with comprehensive error handling
- Processing pipeline: Submission → AI transformation → Database storage
- Admin Slack commands for processing management and retry system
- Complete TDD implementation with mocked and integration tests

Journalist Personalities implemented:
- Feature Writer: Engaging 250-300 word stories with strong leads
- Interview Specialist: Q&A format, conversational tone, 150-200 words  
- Sports Reporter: High energy, sports terminology, 150-200 words
- Staff Reporter: Professional but friendly, general news, 100-150 words
- Body/Mind Writer: Wellness content with actionable advice, 150-200 words

Technical Implementation Completed:
✅ TDD methodology with Red-Green-Refactor cycles
✅ Interface-driven design (EnhancedAIService interface)
✅ Robust error handling (rate limits, timeouts, content filtering) 
✅ Question-based journalist assignment using CategoryToJournalistMapping
✅ Database integration with ProcessedArticle model
✅ Real API integration with structured JSON output
✅ Comprehensive test coverage (mocked + integration tests)

Processing Pipeline Implemented:
1. ✅ Raw submission → Question category-based journalist assignment
2. ✅ AI processing with personality-specific prompts and user context
3. ✅ JSON response validation and structured article generation
4. ✅ Database storage with processing metadata
5. ✅ Error handling with detailed logging

Critical Fix Applied:
- Fixed journalist selection logic to use question categories instead of content analysis
- Implemented determineJournalistTypeFromSubmission() for proper mapping
- Enhanced question-based processing with GetQuestionByID integration

Architecture Delivered:
- internal/ai/service.go - Complete AI service interface with CategoryToJournalistMapping
- internal/ai/anthropic.go - Full Anthropic API implementation with JSON processing
- internal/ai/journalists.go - All journalist prompt templates with real personas
- internal/slack/question_based_processing_test.go - Question-category mapping tests
- internal/slack/auto_processing_test.go - End-to-end AI processing tests
- Enhanced Slack bot with automatic AI processing on submissions

Expected outcome: ✅ ACHIEVED - Complete AI journalist system that transforms raw submissions into 
engaging newsletter articles with distinct writing styles, proper error handling, 
and question-category based journalist assignment - fully tested and production-ready
```

### Prompt 8: Weekly Newsletter Automation System - [✅] COMPLETED  

```
Implement automated weekly newsletter generation with person rotation and content assignment.

Create weekly automation system with:
- Issue tracking database schema (newsletter_issues, person_assignments)
- Calendar-driven automatic content assignment with person rotation
- Admin commands for manual overrides and status monitoring
- Anonymous body/mind question pool management with broadcast request system
- Time-gated web page rendering (9:30 AM publication rule)
- Integration with existing AI processing pipeline for automated article generation

Weekly Newsletter Workflow to Implement:
1. **Feature Request**: Send to 1 person (feature/interview journalist)
2. **General Questions**: Send to 3 different people (general journalist) 
3. **Body/Mind Content**: Use existing anonymous question pool OR send broadcast request to refill
4. **Person Tracking**: Ensure no duplicate assignments in consecutive issues
5. **Web Page**: Automatically renders current week's newsletter after 9:30 AM

Database Schema Extensions:
- newsletter_issues table: id, week_number, year, publication_date, status
- person_assignments table: issue_id, person_id, content_type, question_id, assigned_date
- body_mind_questions table: id, question_text, category, status, created_at (NO user tracking - anonymous)
- person_rotation_history for intelligent assignment algorithm

Admin Commands to Implement:
- `/pp admin assign-week [type] [@user...]` - Manual override for current week assignments
  - Example: `/pp admin assign-week feature @john.doe`
  - Example: `/pp admin assign-week general @jane.smith @mike.jones @sarah.wilson`
- `/pp admin broadcast-bodymind` - Send anonymous wellness question request to all users
- `/pp admin week-status` - Current week dashboard with assignments and submission status
- `/pp admin pool-status` - Anonymous body/mind question pool levels and activity metrics

Pool Status Display (Anonymous):
```
📊 Body/Mind Question Pool Status
Available Questions: 12
└─ Wellness: 5 questions
└─ Mental Health: 4 questions  
└─ Work-Life Balance: 3 questions

Recent Activity:
└─ 3 days ago: New wellness question added
└─ 1 week ago: Work-life balance question added
⚠️  Pool getting low! Consider broadcast when < 8 questions remain.
```

Week Status Display:
```
📅 Week 37, 2025 Newsletter Status
Assignments:
✅ Feature: @john.doe (submitted 2 days ago)
⏳ General: @jane.smith (no submission yet)
❌ General: @sarah.wilson (overdue - sent reminder)
✅ Body/Mind: Using pool question #47
Publication: Thursday 9:30 AM (2 days remaining)
```

Technical Requirements:
- Calendar-driven automatic assignment (no manual issue creation needed)
- Person rotation algorithm (avoid consecutive issue assignments)
- Anonymous question pool (NO user attribution for body/mind content)
- Time-gated web rendering (check current time vs 9:30 AM rule)
- Slack broadcast messaging to all workspace members
- Integration with existing question/submission/AI processing pipeline
- Admin authorization and comprehensive error handling
- Database migrations for new schema

Expected outcome: ✅ ACHIEVED - Complete weekly newsletter automation that intelligently assigns 
content requests to different people, manages anonymous question pools, supports 
manual overrides, and renders time-gated web pages - fully integrated with 
existing AI processing for automated newsletter generation

Implementation completed with:
- Database migration 003 with all weekly automation tables (newsletter_issues extensions, person_assignments, body_mind_questions, person_rotation_history)  
- Updated main.go to use NewBotWithWeeklyAutomation for full admin command support
- Fixed all admin commands that were returning "not available" errors
- Pool status: Shows anonymous question pool levels with categories and activity
- Week status: Current week dashboard with assignments and submission tracking  
- Assign week: Manual assignment override for feature/general content types
- Broadcast body/mind: Send wellness question request to all workspace members
- Complete integration with existing AI processing pipeline and question management
- All tests passing (15+ comprehensive tests covering database operations, admin commands, pool management)
- Production-ready with proper error handling and authorization checks
```

### Prompt 9: HTML Template System - [✅] COMPLETED

```
Implement responsive HTML newsletter template system using tmpl package.

Create template system with:
- tylermmorton/tmpl package integration for type-safe templates
- Responsive CSS Grid layout (mobile-friendly, desktop newspaper-style)
- Newsletter template components (header, footer, article sections)
- Template helper functions for formatting and data transformation
- Static asset serving (CSS, images, fonts)
- Template compilation and caching

Template Architecture:
- Newsletter page with grid layout supporting multiple article formats
- Article templates: Feature (hero), Interview (Q&A), Column (standard), Sports, Body/Mind
- Responsive design: Mobile stacks sections, Desktop uses newspaper columns
- Typography: Old-timey newspaper aesthetic with serif headers, column separators

Technical Requirements:
- Write tests for template rendering with real ProcessedArticle data
- Ensure templates are secure (automatic HTML escaping)
- Create responsive CSS Grid (3-4 columns desktop, single column mobile)
- Template composition with nested structs (NewsletterPage → Articles → ProcessedContent)
- Integration with ProcessedArticle database models and weekly issue system
- Template format mapping (separate from journalist content processing)

Expected outcome: ✅ ACHIEVED - Complete template system that renders AI-processed articles into 
beautiful, responsive HTML newsletters with newspaper-style layout

Implementation completed with standard html/template package:
- Responsive CSS Grid layout with old-timey newspaper aesthetic (Playfair Display + Source Sans Pro typography)
- Complete article templates for all journalist types (Feature, Interview, General, Sports, Body/Mind)
- Template helper functions (formatDate, safeHTML, truncate, wordCount, dict)
- HTTP routes for newsletter rendering (/newsletter, /newsletter/week/year, /newsletter/id)
- Static asset serving (CSS, images, fonts) at /static/ endpoint
- Mobile-responsive design: Single column mobile, multi-column newspaper layout desktop
- Integration with ProcessedArticle database models and weekly issue system
- Time-gated rendering (respects publication dates, shows draft notices)
- Template composition with nested structs (NewsletterPage → Articles → ProcessedContent)
- Template security with automatic HTML escaping and safeHTML for controlled content
- Template configuration system with customizable company names, themes, URLs

Technical Architecture Delivered:
- internal/templates/service.go - Complete template service with rendering methods
- internal/templates/models.go - Template data structures and configuration
- templates/*.html - All article and newsletter templates with responsive design
- static/css/newsletter.css - Comprehensive newspaper-style CSS with mobile support
- HTTP integration in server.go with proper error handling and logging
- Database integration with GetProcessedArticlesByNewsletterIssue method

System builds successfully and ready for Phase 1 completion
```

### Prompt 10: Auto-Assignment Architecture Refactor (TDD) - [🔄] IN PROGRESS

```
Fix missing articles in newsletter by implementing ProcessAndSaveSubmission architecture using TDD.

CRITICAL ISSUE IDENTIFIED:
- ✅ Submissions are saved to database (confirmed via admin commands)  
- ✅ AI processing works (no timeout errors)
- ❌ ProcessedArticles never saved to database (missing final step)
- ❌ Newsletter queries return empty (frontend works, no data to display)

ROOT CAUSE: Current architecture has AI service return ProcessedArticle objects that are never persisted to database.

SOLUTION: Refactor to ProcessAndSaveSubmission architecture where AI service handles complete flow atomically.

Current Broken Flow:
```
1. User submits → Save Submission ✅
2. AI processes → Return ProcessedArticle object ✅  
3. Set newsletter_issue_id on object ✅
4. ❌ MISSING: Save ProcessedArticle to database
5. Frontend queries ProcessedArticles → Empty results ❌
```

New Simplified Architecture:
```  
1. User submits → Save Submission ✅
2. Get current newsletter issue ✅
3. AI.ProcessAndSaveSubmission(submission, userInfo, newsletterIssueID) ✅
   └─ Process with AI ✅
   └─ Create ProcessedArticle with newsletter_issue_id ✅  
   └─ Save to database atomically ✅
4. Frontend queries ProcessedArticles → Articles appear! ✅
```

TDD Implementation Plan:
1. 🔴 RED: Write failing test for ProcessAndSaveSubmission method
2. 🔴 RED: Write failing test for auto-assignment integration  
3. 🟢 GREEN: Add ProcessAndSaveSubmission to AI service interface
4. 🟢 GREEN: Implement method in AnthropicService with database save
5. 🟢 GREEN: Update async processing to use new method
6. 🟢 GREEN: Update DatabaseInterface and mocks
7. ✨ REFACTOR: Clean up old orchestration code
8. ✅ VERIFY: Articles appear in production newsletter

Technical Requirements:
- Follow strict TDD methodology (Red-Green-Refactor)
- Single atomic operation for AI processing + database save
- Proper error handling and transaction safety
- Update all interfaces and mocks for testing
- Maintain backward compatibility during transition
- Comprehensive test coverage for new architecture

Expected outcome: Articles automatically appear in newsletter after AI processing,
with clean architecture where AI service owns complete processing-to-persistence flow
```

### Prompt 11: Integration Testing & Phase 1 Completion - [ ] PENDING

```
Create end-to-end integration tests and finalize Phase 1 after auto-assignment fix.

Implement integration testing:
- End-to-end test from submission to newsletter display (including auto-assignment)
- Template rendering tests with real ProcessedArticle data  
- Weekly automation workflow testing (person rotation, question assignment)
- HTTP endpoint integration tests with article display verification
- Database migration testing (including all newsletter automation schema)
- AI processing error scenario testing with ProcessAndSaveSubmission
- Performance baseline measurements for complete processing pipeline

Requirements:
- Comprehensive test suite covering all Phase 1 functionality
- Test cleanup and isolation with proper mocking for AI services
- Documentation updates reflecting ProcessAndSaveSubmission architecture
- Code review and refactoring for maintainability  
- Verification that articles appear correctly in production newsletter
- Prepare codebase for Phase 2 development (advanced newsletter features)

Expected outcome: Fully tested and documented Phase 1 implementation with working
newsletter article display, complete automation system, and clean architecture
ready for advanced newsletter features development
```

---

## Current Status Summary

**Phase 1 Progress: 90% Complete (9/10 prompts)**

### ✅ Completed Core Functionality:
- **Project Structure** - Go modules, proper directory layout
- **Database Layer** - SQLite with migrations, CRUD operations including ProcessedArticle schema
- **Slack Bot Framework** - Command handling, event processing
- **Question Management** - Rotation logic, categorization, admin controls
- **News Submission System** - Full TDD implementation with database storage
- **Production Deployment** - Docker, Coolify, SSL, signature verification

### 📊 Technical Achievements:
- **15+ comprehensive tests** with 100% passing rate
- **Complete TDD cycles** following Red-Green-Refactor methodology
- **Database migrations** with backward compatibility (including migration 3 for AI processing)
- **Production deployment** with proper security and SSL
- **Admin/user separation** with authorization controls
- **Clean architecture** with proper separation of concerns
- **ProcessedArticle data model** ready for AI integration with validation and CRUD operations

### ✅ Recently Completed:
- **AI-Powered Content Processing** - Full Anthropic API integration with 5 journalist personalities and question-based assignment
- **Weekly Newsletter Automation** - Complete automation system with person rotation, anonymous question pools, and admin command dashboard
- **HTML Template System** - Responsive newsletter rendering with newspaper-style layout and mobile-friendly design

### 🎯 Remaining Work:
- **End-to-end Integration Testing** - Complete Phase 1 testing and documentation
- Phase 2 planning: Advanced newsletter features and distribution automation

The core newsletter submission and AI processing system is **production-ready** with comprehensive test coverage. 
The AI journalist pipeline with Anthropic API integration is fully functional and tested.
Next phase focuses on weekly automation workflow and HTML template generation.

---

_Use `/execute-plan` command to begin systematic implementation_