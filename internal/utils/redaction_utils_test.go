package utils

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeRawQueryRedactsCredentialsAndKeepsOtherValues(t *testing.T) {
	secrets := []string{
		"secret-key",
		"secret-api-key",
		"secret-token",
		"secret-access-token",
		"secret-auth",
		"secret-key-value",
	}
	raw := "page=2&model=gpt-4&key=" + secrets[0] +
		"&api_key=" + secrets[1] +
		"&token=" + secrets[2] +
		"&access-token=" + secrets[3] +
		"&AUTH=" + secrets[4] +
		"&key_value=" + secrets[5]

	sanitized := SanitizeRawQuery(raw)
	for _, secret := range secrets {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("sanitized query leaked %q: %s", secret, sanitized)
		}
	}

	query, err := url.ParseQuery(sanitized)
	if err != nil {
		t.Fatalf("sanitized query is invalid: %v", err)
	}
	for _, name := range []string{"key", "api_key", "token", "access-token", "AUTH", "key_value"} {
		if got := query.Get(name); got != RedactedValue {
			t.Errorf("%s = %q, want %q", name, got, RedactedValue)
		}
	}
	if got := query.Get("page"); got != "2" {
		t.Errorf("page = %q, want 2", got)
	}
	if got := query.Get("model"); got != "gpt-4" {
		t.Errorf("model = %q, want gpt-4", got)
	}
}

func TestSanitizeRawQueryNeverReturnsMalformedSecret(t *testing.T) {
	const secret = "secret-that-must-not-be-logged"
	got := SanitizeRawQuery("key=" + secret + ";invalid")
	if strings.Contains(got, secret) {
		t.Fatalf("malformed query leaked secret: %q", got)
	}
}

func TestKeyFingerprintUsesOnlyOneWayHash(t *testing.T) {
	const hash = "ABCDEF0123456789ABCDEF0123456789"
	if got, want := KeyFingerprint(hash), "fp:abcdef012345"; got != want {
		t.Fatalf("KeyFingerprint() = %q, want %q", got, want)
	}
}

func TestParseKeyFingerprint(t *testing.T) {
	prefix, ok := ParseKeyFingerprint(" FP:ABCDEF012345 ")
	if !ok || prefix != "abcdef012345" {
		t.Fatalf("ParseKeyFingerprint() = %q, %t", prefix, ok)
	}
	for _, invalid := range []string{"abcdef012345", "fp:short", "fp:abcdef01234z"} {
		if _, ok := ParseKeyFingerprint(invalid); ok {
			t.Errorf("ParseKeyFingerprint(%q) accepted invalid fingerprint", invalid)
		}
	}
}

func TestIsSensitiveNameCoversCommonNormalizedCredentialNames(t *testing.T) {
	for _, name := range []string{
		"AUTH_KEY",
		"auth-key",
		"client_secret",
		"SECRET_KEY",
		"refreshToken",
		"id_token",
		"private.key",
		"session-token",
		"DATABASE_DSN",
		"redis-dsn",
		"proxy_url",
		"cookie",
	} {
		if !IsSensitiveName(name) {
			t.Errorf("IsSensitiveName(%q) = false", name)
		}
	}
	for _, name := range []string{"model", "page", "monkey", "keyboard_layout"} {
		if IsSensitiveName(name) {
			t.Errorf("IsSensitiveName(%q) = true for non-sensitive name", name)
		}
	}
}

func TestSanitizeTextRedactsEmbeddedCredentialForms(t *testing.T) {
	secrets := []string{"url-secret", "json-secret", "bearer-secret", "client-secret", "proxy-password", "database-secret", "userinfo-token"}
	input := `request failed for https://example.test/path?key=` + secrets[0] +
		`&model=gpt-4 response={"api_key":"` + secrets[1] +
		`"} Authorization: Bearer ` + secrets[2] +
		` client_secret=` + secrets[3] +
		` via http://operator:` + secrets[4] + `@proxy.example.test` +
		` DATABASE_DSN=postgres://db:` + secrets[5] + `@database.example.test/app` +
		` fallback=https://` + secrets[6] + `@private.example.test` +
		` contact=https://example.test?email=user@example.test`

	got := SanitizeText(input)
	for _, secret := range secrets {
		if strings.Contains(got, secret) {
			t.Fatalf("SanitizeText leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "model=gpt-4") {
		t.Fatalf("SanitizeText removed non-sensitive context: %s", got)
	}
	if !strings.Contains(got, "email=user@example.test") {
		t.Fatalf("SanitizeText mistook a query value for URL userinfo: %s", got)
	}
	if secondPass := SanitizeText(got); secondPass != got {
		t.Fatalf("SanitizeText is not idempotent:\nfirst:  %s\nsecond: %s", got, secondPass)
	}
}

func TestSanitizeTextRedactsStructuredJSONCredentialCollections(t *testing.T) {
	const input = `{"model":"gpt-4","proxy_keys":["first-secret","second-secret"],"nested":{"auth_key":"third-secret"},"config":{"proxy_keys":"fourth-secret,fifth-secret"}}`

	got := SanitizeText(input)
	for _, secret := range []string{
		"first-secret",
		"second-secret",
		"third-secret",
		"fourth-secret",
		"fifth-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("SanitizeText leaked %q from structured JSON: %s", secret, got)
		}
	}
	if !strings.Contains(got, `"model":"gpt-4"`) {
		t.Fatalf("SanitizeText removed non-sensitive JSON context: %s", got)
	}
	if count := strings.Count(got, RedactedValue); count != 3 {
		t.Fatalf("SanitizeText produced %d redactions, want 3: %s", count, got)
	}
}

func TestSanitizeKnownSecretsRedactsUnlabelledAndEncodedEchoes(t *testing.T) {
	const secret = `sk-live/a+b="private"`
	input := "Incorrect credential " + secret +
		"; request=https://example.test?credential=" + url.QueryEscape(secret) +
		`; response={"message":"` + strings.Trim(string(mustJSONMarshal(t, secret)), `"`) + `"}`

	got := SanitizeKnownSecrets(input, secret)
	for _, candidate := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
		if strings.Contains(got, candidate) {
			t.Fatalf("SanitizeKnownSecrets leaked %q: %s", candidate, got)
		}
	}
	if !strings.Contains(got, RedactedValue) {
		t.Fatalf("SanitizeKnownSecrets did not leave a redaction marker: %s", got)
	}
}

func mustJSONMarshal(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return encoded
}
