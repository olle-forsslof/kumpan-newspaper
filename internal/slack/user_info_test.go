package slack

import (
	"context"
	"testing"
)

// TDD: Test for fetching user information using mock
func TestMockBot_GetUserInfo(t *testing.T) {
	// Test with mock bot instead of real Slack API
	bot := NewMockBot()

	// Test getting user info by Slack user ID
	userID := "U123456789"
	userInfo, err := bot.GetUserInfo(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserInfo() failed: %v", err)
	}

	// Verify user info structure
	if userInfo.ID != userID {
		t.Errorf("Expected ID %s, got %s", userID, userInfo.ID)
	}

	if userInfo.RealName == "" {
		t.Error("Expected non-empty RealName")
	}

	if userInfo.Profile.Email == "" {
		t.Error("Expected non-empty Email")
	}

	if userInfo.Profile.Title == "" {
		t.Error("Expected non-empty Title/Department")
	}
}

// TDD: Test user info integration with submission processing
func TestMockBot_EnrichSubmissionWithUserInfo(t *testing.T) {
	// Test with mock bot instead of real Slack API
	bot := NewMockBot()

	userID := "U123456789"
	content := "Our team launched a new feature this week!"

	// Test enriching submission with user information
	enrichedSubmission, err := bot.EnrichSubmissionWithUserInfo(context.Background(), userID, content)
	if err != nil {
		t.Fatalf("EnrichSubmissionWithUserInfo() failed: %v", err)
	}

	// Verify enriched submission contains user details
	if enrichedSubmission.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, enrichedSubmission.UserID)
	}

	if enrichedSubmission.Content != content {
		t.Errorf("Expected content %s, got %s", content, enrichedSubmission.Content)
	}

	if enrichedSubmission.AuthorName == "" {
		t.Error("Expected non-empty AuthorName")
	}

	if enrichedSubmission.AuthorEmail == "" {
		t.Error("Expected non-empty AuthorEmail")
	}

	if enrichedSubmission.AuthorDepartment == "" {
		t.Error("Expected non-empty AuthorDepartment")
	}
}

// Additional test to verify the specific mock data structure
func TestMockBot_UserInfoStructure(t *testing.T) {
	bot := NewMockBot()
	userID := "U123456789"

	userInfo, err := bot.GetUserInfo(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserInfo() failed: %v", err)
	}

	// Verify specific mock values
	expectedRealName := "Test User"
	if userInfo.RealName != expectedRealName {
		t.Errorf("Expected RealName %s, got %s", expectedRealName, userInfo.RealName)
	}

	expectedEmail := "testuser@company.com"
	if userInfo.Profile.Email != expectedEmail {
		t.Errorf("Expected Email %s, got %s", expectedEmail, userInfo.Profile.Email)
	}

	expectedTitle := "Software Developer"
	if userInfo.Profile.Title != expectedTitle {
		t.Errorf("Expected Title %s, got %s", expectedTitle, userInfo.Profile.Title)
	}
}
