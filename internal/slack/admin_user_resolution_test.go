package slack

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TestResolveUserIdentifierLogic tests the core logic for user ID resolution
// This test will drive the implementation of the resolveUserIdentifier method
func TestResolveUserIdentifierLogic(t *testing.T) {
	// For now, this is a placeholder test that will help us implement
	// the resolveUserIdentifier method step by step

	t.Run("Should strip @ prefix from user IDs", func(t *testing.T) {
		// Test that "@U123456789" becomes "U123456789"
		input := "@U123456789"
		expected := "U123456789"

		// This test will fail until we implement the method
		actual := stripAtPrefix(input)

		if actual != expected {
			t.Errorf("Expected '%s', got '%s'", expected, actual)
		}
	})

	t.Run("Should detect existing user IDs", func(t *testing.T) {
		// Test that "U123456789" is recognized as a user ID
		input := "U123456789"

		actual := isUserID(input)

		if !actual {
			t.Errorf("Expected '%s' to be recognized as user ID", input)
		}
	})

	t.Run("Should detect non-user IDs", func(t *testing.T) {
		// Test that "olle" is not recognized as a user ID
		input := "olle"

		actual := isUserID(input)

		if actual {
			t.Errorf("Expected '%s' to NOT be recognized as user ID", input)
		}
	})
}

// Helper functions that will be implemented
func stripAtPrefix(input string) string {
	return strings.TrimPrefix(input, "@")
}

func isUserID(input string) bool {
	return strings.HasPrefix(input, "U") && len(input) > 5
}

// For now, let's test the resolveUserIdentifier method indirectly
// by testing that it handles user ID detection correctly
func TestResolveUserIdentifierWithoutBroadcastManager(t *testing.T) {
	ctx := context.Background()

	// Test with nil broadcast manager (should handle user IDs directly)
	handler := &AdminHandler{
		broadcastManager: nil,
	}

	t.Run("Should handle existing user IDs", func(t *testing.T) {
		userID, err := handler.resolveUserIdentifier(ctx, "U123456789")
		if err != nil {
			t.Errorf("Unexpected error for user ID: %v", err)
		}
		if userID != "U123456789" {
			t.Errorf("Expected U123456789, got %s", userID)
		}
	})

	t.Run("Should handle user IDs with @ prefix", func(t *testing.T) {
		userID, err := handler.resolveUserIdentifier(ctx, "@U123456789")
		if err != nil {
			t.Errorf("Unexpected error for @user ID: %v", err)
		}
		if userID != "U123456789" {
			t.Errorf("Expected U123456789, got %s", userID)
		}
	})

	t.Run("Should error for username without broadcast manager", func(t *testing.T) {
		_, err := handler.resolveUserIdentifier(ctx, "olle")
		if err == nil {
			t.Error("Expected error when trying to lookup username without broadcast manager")
		}
	})
}

// TestAssignQuestionWithUsernameLookup tests assign-question with username resolution
// This test validates the end-to-end integration
func TestAssignQuestionWithUsernameLookup(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tempFile := "/tmp/test_assign_username.db"
	defer func() {
		_ = os.Remove(tempFile)
	}()

	db, err := database.NewSimple(tempFile)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Create handler with real BroadcastManager (using fake token)
	// The BroadcastManager will fail on actual API calls, but the user resolution should work
	handler := NewAdminHandlerWithWeeklyAutomation(
		&mockQuestionSelector{},
		[]string{"U123ADMIN"},
		&mockSubmissionManager{},
		db,
		"fake-token",
	)

	// Test with a user ID (should work without API calls)
	cmd := &AdminCommand{
		Action: "assign-question",
		Args:   []string{"feature", "U789USER"},
	}

	response, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd)
	if err != nil {
		t.Fatalf("Failed to handle assign-question command with user ID: %v", err)
	}

	// Should have successful assignment for user ID
	if !strings.Contains(response.Text, "Successfully assigned") {
		t.Errorf("Expected successful assignment, got: %s", response.Text)
	}

	// Test with a username (should fail due to fake token, but with better error message)
	cmd2 := &AdminCommand{
		Action: "assign-question",
		Args:   []string{"feature", "olle"},
	}

	response2, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd2)
	if err != nil {
		t.Fatalf("Failed to handle assign-question command: %v", err)
	}

	// Should show a user lookup error (not a parsing error)
	if !strings.Contains(response2.Text, "failed to find user") || !strings.Contains(response2.Text, "olle") {
		t.Errorf("Expected user lookup error for 'olle', got: %s", response2.Text)
	}
}
