package slack

import "context"

type SlackConfig struct {
	Token         string
	SigningSecret string
}

type SlashCommand struct {
	Token       string
	Command     string
	Text        string
	UserID      string
	ChannelID   string
	ResponseURL string
}

type SlashCommandResponse struct {
	Text         string `json:"text"`
	ResponseType string `json:"response_type,omitempty"`
}

type Bot interface {
	SendMessage(ctx context.Context, channelID, text string) error
	HandleSlashCommand(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error)
	HandleEventCallback(ctx context.Context, event SlackEvent) error
}

type SlackEvent struct {
	Type    string `json:"type"`
	User    string `json:"user"`
	Text    string `json:"text"`
	Channel string `json:"channel"`
	BotID   string `json:"bot_id,omitempty"`
}
