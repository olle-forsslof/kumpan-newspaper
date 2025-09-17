package slack

import (
	"context"
	"errors"
	"testing"

	"github.com/slack-go/slack"
)

// slackClientInterface defines the methods we need from slack.Client for testing
type slackClientInterface interface {
	PostMessageContext(ctx context.Context, channel string, options ...slack.MsgOption) (string, string, error)
	OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
}

// mockSlackClient implements slackClientInterface for testing
type mockSlackClient struct {
	postMessageCalls      []postMessageCall
	openConversationCalls []openConversationCall
	shouldFailPostMessage bool
	shouldFailOpenIM      bool
	imChannelID           string
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
