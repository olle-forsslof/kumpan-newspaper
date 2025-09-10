package slack

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestVerifySignature_ValidSignature(t *testing.T) {
	signingSecret := "8f742231b10e8888abcd99yyyzzz85a5"

	currentTime := time.Now().Unix()
	timestamp := fmt.Sprintf("%d", currentTime)
	body := "token=xoxb-abc123&team_id=T1DC2JH3J&command=/weather&text=94070"

	expectedSignature := computeExpectedSignature(signingSecret, timestamp, body)

	// Test: Our verification function should return true for valid signatures
	valid := VerifySignature(signingSecret, timestamp, body, expectedSignature)

	if !valid {
		t.Error("Expected valid signature to be verified as true")
	}
}

func TestVerifySignature_InvalidSignature(t *testing.T) {
	// WHAT: Test that we reject signatures that don't match
	// WHY: This prevents attackers from sending fake requests with wrong signatures

	signingSecret := "8f742231b10e8888abcd99yyyzzz85a5"
	timestamp := "1531420618"
	body := "token=xoxb-abc123&team_id=T1DC2JH3J&command=/weather&text=94070"

	// This signature is wrong - maybe an attacker guessed it
	fakeSignature := "v0=wrongsignaturehereabcd1234567890abcd1234567890abcd1234567890abcd12"

	// Test: Our function should return false for invalid signatures
	valid := VerifySignature(signingSecret, timestamp, body, fakeSignature)

	if valid {
		t.Error("Expected invalid signature to be rejected")
	}
}

func TestVerifySignature_OldTimestamp(t *testing.T) {
	// WHAT: Test that we reject requests with old timestamps
	// WHY: Prevents replay attacks where someone records a valid request and resends it later

	signingSecret := "8f742231b10e8888abcd99yyyzzz85a5"

	// This timestamp is from 10 minutes ago (older than the 5-minute window Slack recommends)
	tenMinutesAgo := time.Now().Add(-10 * time.Minute).Unix()
	timestamp := fmt.Sprintf("%d", tenMinutesAgo)

	body := "token=xoxb-abc123&command=/submit&text=hello"

	// Compute what would be a valid signature for this old timestamp
	expectedSignature := computeExpectedSignature(signingSecret, timestamp, body)

	// Test: Even with correct signature, old timestamps should be rejected
	valid := VerifySignature(signingSecret, timestamp, body, expectedSignature)

	// DEBUG: Let's see what we actually got
	t.Logf("DEBUG: valid=%v, timestamp=%s", valid, timestamp)

	if valid {
		t.Error("Expected old timestamp to be rejected (prevents replay attacks)")
	}

	if valid {
		t.Error("Expected old timestamp to be rejected (prevents replay attacks)")
	}
}

func TestVerifySignature_MalformedSignature(t *testing.T) {
	// WHAT: Test that we handle malformed signature headers gracefully
	// WHY: Attackers might send weird data to try to crash our verification

	signingSecret := "8f742231b10e8888abcd99yyyzzz85a5"
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	body := "token=test&command=/submit"

	malformedSignatures := []string{
		"not-a-signature", //       No v0= prefix
		"v0=",             //       Empty signature
		"v1=abcd1234",     //       Wrong version
		"v0=not-hex",      //       Invalid hex
		"",                //       Empty string
	}

	for _, badSig := range malformedSignatures {
		valid := VerifySignature(signingSecret, timestamp, body, badSig)
		if valid {
			t.Errorf("Expected malformed signature '%s' to be rejected", badSig)
		}
	}
}

func TestSlashCommandHandler_SignatureVerification(t *testing.T) {
	// WHAT: Test that our HTTP handler actually calls signature verification
	// WHY: Integration test to ensure the security layer is wired up correctly

	mockBot := NewMockBot()

	// Create handler with signing secret
	handler := NewSlashCommandHandlerWithSecurity(mockBot, "test-signing-secret")

	// Create request with INVALID signature
	formData := url.Values{}
	formData.Set("token", "test-token")
	formData.Set("command", "/submit")
	formData.Set("text", "test submission")

	req := httptest.NewRequest("POST", "/slack/commands",
		strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Signature", "v0=fakesignature123")
	req.Header.Set("X-Slack-Request-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Test: Should return 401 Unauthorized for invalid signature
	if status := rr.Code; status != http.StatusUnauthorized {
		t.Errorf("Expected 401 for invalid signature, got %v", status)
	}

	// Test: Bot should NOT have been called (security blocked the request)
	if len(mockBot.messages) > 0 {
		t.Error("Expected bot to not be called when signature is invalid")
	}
}
