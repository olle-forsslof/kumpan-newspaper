package slack

import (
	"context"
	"testing"
)

func TestSlackBot_SendMessage(t *testing.T) {
	// We want our bot to be able to send messages
	bot := NewMockBot()

	err := bot.SendMessage(context.Background(), "C1234567", "Hello, World!")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
}

func TestSlackBot_HandleSlashCommand(t *testing.T) {
	// We want to handle slash commands like /submit
	bot := NewBot(SlackConfig{Token: "xoxb-test-token"}, &MockQuestionSelector{}, []string{"U1234567"})

	// Simulate a slash command payload
	command := SlashCommand{
		Command: "/submit",
		Text:    "This is my newsletter submission",
		UserID:  "U1234567",
	}

	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	if response.Text == "" {
		t.Fatal("Expected response text, got empty string")
	}
}
