package kp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

type SlackMessenger struct {
	client *slack.Client
}

var _ Messenger = (*SlackMessenger)(nil)

func NewMessenger(token string) *SlackMessenger {
	return &SlackMessenger{client: slack.New(token, slack.OptionHTTPClient(&http.Client{Timeout: 10 * time.Second}))}
}

func (m *SlackMessenger) Send(ctx context.Context, recipient, text string) error {
	channel := recipient
	if strings.HasPrefix(recipient, "U") || strings.HasPrefix(recipient, "W") {
		conversation, _, _, err := m.client.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: []string{recipient}})
		if err != nil || conversation == nil || conversation.ID == "" {
			return errors.New("Slack conversation unavailable")
		}
		channel = conversation.ID
	}
	_, _, err := m.client.PostMessageContext(ctx, channel, slack.MsgOptionText(text, false), slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl())
	if err != nil {
		return errors.New("Slack delivery failed")
	}
	return nil
}
