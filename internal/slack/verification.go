package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

func VerifySignature(signingSecret, timestamp, body, signature string) bool {
	// parse the signature format
	// Slack sends signatures like: "v0=a2114d57b48eac39b9ad189dd8316235a7b4a8d21a10bd27519666489c69b503"
	if !strings.HasPrefix(signature, "v0=") {
		slog.Warn("Invalid signarure format - missing v0= prefix", "signature", signature)
		return false
	}

	providedSignature := signature[3:] // remove the "v0=" prefix

	if !isTimestampFresh(timestamp) {
		slog.Warn("Too old timestamp", "timestamp", timestamp)
		return false
	}

	expectedSignature := computeSignature(signingSecret, timestamp, body)

	// Step 4: Compare signatures using constant-time comparison
	// This prevents timing attacks where attackers could guess signatures
	// by measuring how long comparisons take
	return hmac.Equal([]byte(providedSignature), []byte(expectedSignature))
}

// computeExpectedSignature is a helper for tests to generate valid signatures
// It returns the full signature with "v0=" prefix
func computeExpectedSignature(signingSecret, timestamp, body string) string {
	signature := computeSignature(signingSecret, timestamp, body)
	return fmt.Sprintf("v0=%s", signature)
}

// computeSignature computes the HMAC-SHA256 signature that Slack would generate
func computeSignature(signingSecret, timestamp, body string) string {
	// Step 1: Create the signing string exactly as Slack does
	// Format: "v0:{timestamp}:{body}"
	signingString := fmt.Sprintf("v0:%s:%s", timestamp, body)

	// Step 2: Compute HMAC-SHA256
	h := hmac.New(sha256.New, []byte(signingSecret))
	h.Write([]byte(signingString))

	// Step 3: Convert to hex string (lowercase)
	return hex.EncodeToString(h.Sum(nil))
}

// isTimestampFresh checks if the timestamp is within acceptable range
func isTimestampFresh(timestampStr string) bool {
	// Step 1: Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		slog.Error("Failed to parse timestamp", "timestamp", timestampStr, "error", err)
		return false
	}

	// Step 2: Check freshness (Slack recommends 5 minutes)
	now := time.Now().Unix()
	age := now - timestamp

	// Reject requests older than 5 minutes
	const maxAge = 5 * 60 // 5 minutes in seconds

	if age > maxAge {
		slog.Warn("Request too old", "age_seconds", age, "max_age", maxAge)
		return false
	}

	// Also reject requests from the future (clock skew protection)
	if age < -60 { //  Allow 1 minute of clock skew
		slog.Warn("Request from future", "age_seconds", age)
		return false
	}

	return true
}
