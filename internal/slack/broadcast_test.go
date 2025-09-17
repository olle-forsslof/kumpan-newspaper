package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// slackClientInterface defines the methods we need from slack.Client for testing
type slackClientInterface interface {
	PostMessageContext(ctx context.Context, channel string, options ...slack.MsgOption) (string, string, error)
	OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
	GetUsersContext(ctx context.Context) ([]slack.User, error)
}

// mockSlackClient implements slackClientInterface for testing
type mockSlackClient struct {
	postMessageCalls      []postMessageCall
	openConversationCalls []openConversationCall
	shouldFailPostMessage bool
	shouldFailOpenIM      bool
	shouldFailGetUsers    bool
	imChannelID           string
	users                 []slack.User
}

type postMessageCall struct {
	channel string
	options []slack.MsgOption
}

type openConversationCall struct {
	params *slack.OpenConversationParameters
}

func (m *mockSlackClient) PostMessageContext(ctx context.Context, channel string, options ...slack.MsgOption) (string, string, error) {
	m.postMessageCalls = append(m.postMessageCalls, postMessageCall{
		channel: channel,
		options: options,
	})

	if m.shouldFailPostMessage {
		return "", "", errors.New("channel_not_found")
	}

	return "timestamp", channel, nil
}

func (m *mockSlackClient) OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
	m.openConversationCalls = append(m.openConversationCalls, openConversationCall{
		params: params,
	})

	if m.shouldFailOpenIM {
		return nil, false, false, errors.New("failed to open IM")
	}

	return &slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID: m.imChannelID,
			},
		},
	}, false, false, nil
}

func (m *mockSlackClient) GetUsersContext(ctx context.Context) ([]slack.User, error) {
	if m.shouldFailGetUsers {
		return nil, errors.New("failed to get users")
	}
	return m.users, nil
}

// TestSendDirectMessageCurrentBehavior demonstrates the current failing behavior
func TestSendDirectMessageCurrentBehavior(t *testing.T) {
	ctx := context.Background()
	userID := "U12345678"
	message := "Test message"

	// Create mock client that simulates current failure
	mockClient := &mockSlackClient{
		shouldFailPostMessage: true, // This simulates the "channel_not_found" error
	}

	// Create testable broadcast manager (we'll need to modify the struct for this)
	bm := &testableBroadcastManager{
		client: mockClient,
	}

	// This should fail with current implementation
	err := bm.sendDirectMessage(ctx, userID, message)

	// Verify it fails as expected
	if err == nil {
		t.Error("Expected sendDirectMessage to fail with channel_not_found error")
	}

	// Verify it tried to send directly to userID (current broken behavior)
	if len(mockClient.postMessageCalls) != 1 {
		t.Errorf("Expected 1 PostMessage call, got %d", len(mockClient.postMessageCalls))
	}

	if mockClient.postMessageCalls[0].channel != userID {
		t.Errorf("Expected PostMessage to userID %s, got %s", userID, mockClient.postMessageCalls[0].channel)
	}

	// Verify it didn't try to open IM channel (current behavior)
	if len(mockClient.openConversationCalls) != 0 {
		t.Errorf("Expected 0 OpenConversation calls with current implementation, got %d", len(mockClient.openConversationCalls))
	}
}

// TestSendDirectMessageWithIMChannel tests the expected behavior of opening IM channels
func TestSendDirectMessageWithIMChannel(t *testing.T) {
	ctx := context.Background()
	userID := "U12345678"
	message := "Test message"
	expectedIMChannelID := "D87654321"

	// Create mock client that succeeds with IM channel
	mockClient := &mockSlackClient{
		imChannelID: expectedIMChannelID,
	}

	// Create testable broadcast manager
	bm := &testableBroadcastManager{
		client: mockClient,
	}

	// This should succeed with the new implementation
	err := bm.sendDirectMessageWithIM(ctx, userID, message)

	// Verify it succeeds
	if err != nil {
		t.Errorf("Expected sendDirectMessageWithIM to succeed, got error: %v", err)
	}

	// Verify it opened IM channel first
	if len(mockClient.openConversationCalls) != 1 {
		t.Errorf("Expected 1 OpenConversation call, got %d", len(mockClient.openConversationCalls))
	}

	if len(mockClient.openConversationCalls[0].params.Users) != 1 || mockClient.openConversationCalls[0].params.Users[0] != userID {
		t.Errorf("Expected OpenConversation with user %s, got %v", userID, mockClient.openConversationCalls[0].params.Users)
	}

	// Verify it sent message to IM channel (not directly to userID)
	if len(mockClient.postMessageCalls) != 1 {
		t.Errorf("Expected 1 PostMessage call, got %d", len(mockClient.postMessageCalls))
	}

	if mockClient.postMessageCalls[0].channel != expectedIMChannelID {
		t.Errorf("Expected PostMessage to IM channel %s, got %s", expectedIMChannelID, mockClient.postMessageCalls[0].channel)
	}
}

// TestSendDirectMessageIMChannelFailure tests error handling when IM channel opening fails
func TestSendDirectMessageIMChannelFailure(t *testing.T) {
	ctx := context.Background()
	userID := "U12345678"
	message := "Test message"

	// Create mock client that fails to open IM channel
	mockClient := &mockSlackClient{
		shouldFailOpenIM: true,
	}

	// Create testable broadcast manager
	bm := &testableBroadcastManager{
		client: mockClient,
	}

	// This should fail when trying to open IM channel
	err := bm.sendDirectMessageWithIM(ctx, userID, message)

	// Verify it fails with appropriate error
	if err == nil {
		t.Error("Expected sendDirectMessageWithIM to fail when IM channel opening fails")
	}

	// Verify it tried to open IM channel
	if len(mockClient.openConversationCalls) != 1 {
		t.Errorf("Expected 1 OpenConversation call, got %d", len(mockClient.openConversationCalls))
	}

	// Verify it didn't try to send message after IM opening failed
	if len(mockClient.postMessageCalls) != 0 {
		t.Errorf("Expected 0 PostMessage calls when IM opening fails, got %d", len(mockClient.postMessageCalls))
	}
}

// testableBroadcastManager is a version of BroadcastManager that allows client injection for testing
type testableBroadcastManager struct {
	client slackClientInterface
}

func (bm *testableBroadcastManager) sendDirectMessage(ctx context.Context, userID, message string) error {
	// Current implementation - this will fail
	_, _, err := bm.client.PostMessageContext(ctx, userID,
		slack.MsgOptionText(message, false),
		slack.MsgOptionAsUser(true),
	)
	if err != nil {
		return err
	}
	return nil
}

func (bm *testableBroadcastManager) sendDirectMessageWithIM(ctx context.Context, userID, message string) error {
	// New implementation - open IM channel first, then send message
	params := &slack.OpenConversationParameters{
		Users: []string{userID},
	}
	channel, _, _, err := bm.client.OpenConversationContext(ctx, params)
	if err != nil {
		return err
	}

	_, _, err = bm.client.PostMessageContext(ctx, channel.ID,
		slack.MsgOptionText(message, false),
		slack.MsgOptionAsUser(true),
	)
	if err != nil {
		return err
	}

	return nil
}

func (bm *testableBroadcastManager) lookupUserByName(ctx context.Context, searchName string) (string, error) {
	users, err := bm.client.GetUsersContext(ctx)
	if err != nil {
		return "", err
	}

	searchLower := strings.ToLower(searchName)

	for _, user := range users {
		// Check username, real name, and display name (case insensitive)
		if strings.ToLower(user.Name) == searchLower ||
			strings.ToLower(user.RealName) == searchLower ||
			strings.ToLower(user.Profile.DisplayName) == searchLower {
			return user.ID, nil
		}
	}

	return "", fmt.Errorf("user not found: %s", searchName)
}

// TestLookupUserByName tests username to user ID resolution
func TestLookupUserByName(t *testing.T) {
	ctx := context.Background()

	// Setup mock users
	mockUsers := []slack.User{
		{
			ID:       "U123456789",
			Name:     "olle",
			RealName: "Olle Forsslof",
			Profile:  slack.UserProfile{DisplayName: "Olle F"},
		},
		{
			ID:       "U987654321",
			Name:     "john.doe",
			RealName: "John Doe",
			Profile:  slack.UserProfile{DisplayName: "Johnny"},
		},
		{
			ID:       "U555666777",
			Name:     "jane",
			RealName: "Jane Smith",
			Profile:  slack.UserProfile{DisplayName: ""},
		},
	}

	tests := []struct {
		name           string
		searchName     string
		expectedUserID string
		expectError    bool
	}{
		{
			name:           "Find by username",
			searchName:     "olle",
			expectedUserID: "U123456789",
			expectError:    false,
		},
		{
			name:           "Find by real name",
			searchName:     "John Doe",
			expectedUserID: "U987654321",
			expectError:    false,
		},
		{
			name:           "Find by display name",
			searchName:     "Johnny",
			expectedUserID: "U987654321",
			expectError:    false,
		},
		{
			name:           "Case insensitive search",
			searchName:     "OLLE",
			expectedUserID: "U123456789",
			expectError:    false,
		},
		{
			name:           "User not found",
			searchName:     "nonexistent",
			expectedUserID: "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockSlackClient{
				users: mockUsers,
			}

			bm := &testableBroadcastManager{
				client: mockClient,
			}

			userID, err := bm.lookupUserByName(ctx, tt.searchName)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for search name '%s', but got none", tt.searchName)
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for search name '%s': %v", tt.searchName, err)
			}

			if userID != tt.expectedUserID {
				t.Errorf("Expected user ID '%s' for search name '%s', got '%s'", tt.expectedUserID, tt.searchName, userID)
			}
		})
	}
}

// TestLookupUserByNameAPIFailure tests error handling when GetUsers API fails
func TestLookupUserByNameAPIFailure(t *testing.T) {
	ctx := context.Background()

	mockClient := &mockSlackClient{
		shouldFailGetUsers: true,
	}

	bm := &testableBroadcastManager{
		client: mockClient,
	}

	userID, err := bm.lookupUserByName(ctx, "olle")

	if err == nil {
		t.Error("Expected error when GetUsers fails, but got none")
	}

	if userID != "" {
		t.Errorf("Expected empty user ID when GetUsers fails, got '%s'", userID)
	}
}
