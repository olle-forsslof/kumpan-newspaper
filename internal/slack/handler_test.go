package slack

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testSlashCommandHandler(t *testing.T) {
	mockBot := NewMockBot()
	handler := NewSlashCommandHandler(mockBot)

	formData := url.Values{}
	formData.Set("token", "test-token")
	formData.Set("command", "/submit")
	formData.Set("text", "My newsletter submission")
	formData.Set("user_id", "U1234567")
	formData.Set("channel_id", "C1234567")
	formData.Set("response_url", "https:hooks.slack.com/commands/1234")

	req := httptest.NewRequest("POST", "/slack/commands", strings.NewReader(formData.Encode()))

	// recorde the response
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
}
