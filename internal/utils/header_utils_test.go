package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"gpt-load/internal/models"
)

func TestHeaderRulesUseKeyFingerprintWithoutExposingKey(t *testing.T) {
	const secret = "sk-inception-secret-should-not-be-forwarded"
	const hash = "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0"
	key := &models.APIKey{ID: 27, KeyValue: secret, KeyHash: hash}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := []models.HeaderRule{{Key: "X-Resin-Account", Value: "${API_KEY_FINGERPRINT}", Action: "set"}}
	ApplyHeaderRules(req, rules, NewHeaderVariableContext(nil, key))

	got := req.Header.Get("X-Resin-Account")
	if want := "fp:123456789abc"; got != want {
		t.Fatalf("account header = %q, want %q", got, want)
	}
	if strings.Contains(got, secret) {
		t.Fatal("account header exposed the upstream credential")
	}
}

func TestHeaderFingerprintFallsBackToStableKeyID(t *testing.T) {
	key := &models.APIKey{ID: 27, KeyValue: "sk-secret-without-hash"}
	got := ResolveHeaderVariables("${API_KEY_FINGERPRINT}", NewHeaderVariableContext(nil, key))
	if got != "key-id:27" {
		t.Fatalf("fingerprint fallback = %q, want key-id:27", got)
	}
}

func TestHeaderFingerprintDoesNotExposeUnkeyedHash(t *testing.T) {
	const secret = "guessable-secret"
	hash := sha256.Sum256([]byte(secret))
	key := &models.APIKey{ID: 27, KeyValue: secret, KeyHash: hex.EncodeToString(hash[:])}
	got := ResolveHeaderVariables("${API_KEY_FINGERPRINT}", NewHeaderVariableContext(nil, key))
	if got != "key-id:27" {
		t.Fatalf("unkeyed fingerprint = %q, want key-id:27", got)
	}
}
