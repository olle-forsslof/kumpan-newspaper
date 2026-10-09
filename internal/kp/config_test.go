package kp

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func configEnv(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"PORT": "", "DATABASE_PATH": "", "BASE_URL": "https://kp.example.com/",
		"SLACK_WORKSPACE_ID": "T_TEST", "SLACK_SIGNING_SECRET": "test-signing-secret",
		"SLACK_BOT_TOKEN": "test-bot-token", "SLACK_CLIENT_ID": "test-client-id",
		"SLACK_CLIENT_SECRET": "test-client-secret", "SESSION_SECRET": strings.Repeat("s", 32),
		"OPENAI_API_KEY": "test-api-key", "OPENAI_MODEL": "",
		"UNSPLASH_ACCESS_KEY":   "",
		"SLACK_PUBLISH_CHANNEL": "C_TEST", "ADMIN_USERS": " U_ONE, U_TWO,U_ONE, ,U_TWO ",
	} {
		t.Setenv(name, value)
	}
	// Setenv registered restoration before removing the optional variable.
	if err := os.Unsetenv("DATABASE_PATH"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	configEnv(t)
	got, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Port: "8080", DatabasePath: "kp.db", BaseURL: "https://kp.example.com",
		WorkspaceID: "T_TEST", SigningSecret: "test-signing-secret", BotToken: "test-bot-token",
		ClientID: "test-client-id", ClientSecret: "test-client-secret", SessionSecret: strings.Repeat("s", 32),
		APIKey: "test-api-key", Model: "gpt-4.1-mini", PublishChannel: "C_TEST",
		EditorIDs:         []string{"U_ONE", "U_TWO"},
		UnsplashAccessKey: "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("configuration did not match defaults and normalized editor IDs")
	}
}

func TestLoadConfigWithoutUnsplash(t *testing.T) {
	configEnv(t)
	if err := os.Unsetenv("UNSPLASH_ACCESS_KEY"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.UnsplashAccessKey != "" {
		t.Fatal("missing optional Unsplash key must not prevent startup")
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	configEnv(t)
	t.Setenv("PORT", "65535")
	t.Setenv("DATABASE_PATH", "/data/custom.db")
	t.Setenv("BASE_URL", "http://localhost:8081/")
	t.Setenv("OPENAI_MODEL", "custom-model-version")
	t.Setenv("UNSPLASH_ACCESS_KEY", "fake-unsplash-key")
	t.Setenv("ADMIN_USERS", "U_ONLY")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "65535" || cfg.DatabasePath != "/data/custom.db" || cfg.BaseURL != "http://localhost:8081" || cfg.Model != "custom-model-version" || cfg.UnsplashAccessKey != "fake-unsplash-key" || !reflect.DeepEqual(cfg.EditorIDs, []string{"U_ONLY"}) {
		t.Fatal("configuration overrides were not preserved")
	}
}

func TestLoadConfigRequiredValues(t *testing.T) {
	for _, name := range []string{
		"BASE_URL", "SLACK_WORKSPACE_ID", "SLACK_SIGNING_SECRET", "SLACK_BOT_TOKEN",
		"SLACK_CLIENT_ID", "SLACK_CLIENT_SECRET", "SESSION_SECRET", "OPENAI_API_KEY",
		"SLACK_PUBLISH_CHANNEL", "ADMIN_USERS",
	} {
		for _, value := range []string{"", " \t\n", "private-value with-whitespace"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				configEnv(t)
				t.Setenv(name, value)
				cfg, err := LoadConfig()
				if err == nil || err.Error() != name {
					t.Fatalf("expected an error containing only %s", name)
				}
				if !reflect.DeepEqual(cfg, Config{}) {
					t.Fatal("invalid configuration returned partial credentials")
				}
			})
		}
	}
}

func TestLoadConfigInvalidValues(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"PORT", "0"}, {"PORT", "65536"}, {"PORT", "-1"}, {"PORT", "+8080"},
		{"PORT", " 8080"}, {"PORT", "8080 "}, {"PORT", "8.0"}, {"PORT", "port"},
		{"DATABASE_PATH", ""}, {"DATABASE_PATH", " \t"},
		{"SESSION_SECRET", strings.Repeat("s", 31)},
		{"SESSION_SECRET", strings.Repeat(" ", 32)},
		{"SESSION_SECRET", strings.Repeat("s", 32) + "\n"},
		{"SLACK_CLIENT_SECRET", "secret\u00a0value"},
		{"OPENAI_MODEL", " "}, {"ADMIN_USERS", " , , "},
		{"UNSPLASH_ACCESS_KEY", " "}, {"UNSPLASH_ACCESS_KEY", " \t\n"},
		{"UNSPLASH_ACCESS_KEY", " private-key"}, {"UNSPLASH_ACCESS_KEY", "private-key "},
		{"UNSPLASH_ACCESS_KEY", "private\tkey"}, {"UNSPLASH_ACCESS_KEY", "private\nkey"},
		{"UNSPLASH_ACCESS_KEY", "private\u00a0key"},
		{"BASE_URL", "http://kp.example.com"}, {"BASE_URL", "//kp.example.com"},
		{"BASE_URL", "https://"}, {"BASE_URL", "https:kp.example.com"},
		{"BASE_URL", "ftp://kp.example.com"}, {"BASE_URL", "https://user:secret@kp.example.com"},
		{"BASE_URL", "https://kp.example.com/path"}, {"BASE_URL", "https://kp.example.com//"},
		{"BASE_URL", "https://kp.example.com/%2F"}, {"BASE_URL", "https://kp.example.com/?q=secret"},
		{"BASE_URL", "https://kp.example.com?"}, {"BASE_URL", "https://kp.example.com#secret"},
		{"BASE_URL", "https://kp.example.com/#"}, {"BASE_URL", "https://kp.example.com:bad"},
		{"BASE_URL", "https://kp.example.com:0"}, {"BASE_URL", "https://kp.example.com:65536"},
		{"BASE_URL", "https://kp.example.com:"}, {"BASE_URL", "https://kp.example.com/%"},
		{"BASE_URL", "http://localhost.evil.example"}, {"BASE_URL", "http://127.0.0.2"},
	} {
		t.Run(test.name+"/"+test.value, func(t *testing.T) {
			configEnv(t)
			t.Setenv(test.name, test.value)
			if _, err := LoadConfig(); err == nil || err.Error() != test.name {
				t.Fatalf("expected an error containing only %s", test.name)
			}
		})
	}
}

func TestLoadConfigValidOriginsAndPorts(t *testing.T) {
	for _, origin := range []string{"https://kp.example.com", "https://kp.example.com:443/", "http://localhost", "http://127.0.0.1:8080/"} {
		for _, port := range []string{"1", "8080", "65535"} {
			t.Run(origin+"/"+port, func(t *testing.T) {
				configEnv(t)
				t.Setenv("BASE_URL", origin)
				t.Setenv("PORT", port)
				cfg, err := LoadConfig()
				if err != nil || cfg.BaseURL != strings.TrimSuffix(origin, "/") || cfg.Port != port {
					t.Fatal("valid origin or port rejected")
				}
			})
		}
	}
}
