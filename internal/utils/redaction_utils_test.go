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

func TestSanitizeURLStringForLoggingRedactsUserInfoAndCredentialQueryValues(t *testing.T) {
	secrets := []string{
		"userinfo-user-secret",
		"userinfo-password-secret",
		"query-key-secret",
		"query-api-key-secret",
		"query-token-secret",
		"query-access-token-secret",
		"query-authorization-secret",
	}
	rawURL := "https://" + secrets[0] + ":" + secrets[1] + "@upstream.example.test/v1/search" +
		"?key=" + secrets[2] +
		"&api_key=" + secrets[3] +
		"&token=" + secrets[4] +
		"&access_token=" + secrets[5] +
		"&authorization=" + secrets[6] +
		"&model=gpt-4&page=2"

	sanitized := SanitizeURLStringForLogging(rawURL)
	for _, secret := range secrets {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("sanitized URL leaked %q: %s", secret, sanitized)
		}
	}

	parsed, err := url.Parse(sanitized)
	if err != nil {
		t.Fatalf("sanitized URL is invalid: %v", err)
	}
	if parsed.User != nil {
		t.Fatalf("sanitized URL retained userinfo: %s", sanitized)
	}
	query := parsed.Query()
	for _, name := range []string{"key", "api_key", "token", "access_token", "authorization"} {
		if got := query.Get(name); got != RedactedValue {
			t.Errorf("%s = %q, want %q", name, got, RedactedValue)
		}
	}
	if got := query.Get("model"); got != "gpt-4" {
		t.Errorf("model = %q, want gpt-4", got)
	}
	if got := query.Get("page"); got != "2" {
		t.Errorf("page = %q, want 2", got)
	}
}

func TestSanitizeURLStringForLoggingRejectsMalformedEncodedCredentialName(t *testing.T) {
	const secret = "malformed-encoded-query-secret"
	rawURL := "https://upstream.example.test/search?k%65y=" + secret + "%zz&model=gpt-4"
	got := SanitizeURLStringForLogging(rawURL)
	if strings.Contains(got, secret) {
		t.Fatalf("malformed URL leaked encoded query credential: %q", got)
	}
	if !strings.Contains(got, RedactedValue) {
		t.Fatalf("malformed query did not fail closed: %q", got)
	}

	malformedPath := "https://upstream.example.test/bad%zz?k%65y=" + secret
	if got := SanitizeURLStringForLogging(malformedPath); got != RedactedValue {
		t.Fatalf("unparseable URL = %q, want %q", got, RedactedValue)
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

func TestParseKeyIdentifierAcceptsMaskedIdentifier(t *testing.T) {
	head, tail, hashPrefix, ok := ParseKeyIdentifier(" sk-a****mnop ")
	if !ok {
		t.Fatalf("ParseKeyIdentifier() rejected a well-formed mask")
	}
	if head != "sk-a" || tail != "mnop" {
		t.Fatalf("ParseKeyIdentifier() = %q, %q, want %q, %q", head, tail, "sk-a", "mnop")
	}
	if hashPrefix != "" {
		t.Fatalf("bare mask yielded hash prefix %q, want none", hashPrefix)
	}
}

func TestParseKeyIdentifierAcceptsDiscriminatedIdentifier(t *testing.T) {
	head, tail, hashPrefix, ok := ParseKeyIdentifier(" sk-a****mnop#B91B ")
	if !ok {
		t.Fatalf("ParseKeyIdentifier() rejected a discriminated identifier")
	}
	if head != "sk-a" || tail != "mnop" {
		t.Fatalf("ParseKeyIdentifier() mask = %q/%q, want sk-a/mnop", head, tail)
	}
	// Hex digests are stored lower-case, so the suffix is normalized.
	if hashPrefix != "b91b" {
		t.Fatalf("hash prefix = %q, want %q", hashPrefix, "b91b")
	}
}

func TestParseKeyIdentifierRejectsCompleteKeysAndOtherIdentifiers(t *testing.T) {
	// A complete key must never be diverted into masked-key resolution: it has to
	// reach the exact-hash lookup instead.
	invalid := []string{
		"",
		"sk-a-complete-upstream-key-value",
		"fp:abcdef012345",
		MaskKeyIdentifier("short"), // the bare marker carries no head or tail
		"sk-a***mnop",              // three stars
		"sk-a*****mno",             // five stars
		"sk-a****mno",              // too short overall
		"sk-a****mnopq",            // too long overall
		"sk**a****mnop",            // marker at the wrong offset
		"sk-a****mnop#",            // empty discriminator
		"sk-a****mnop#b91",         // discriminator too short
		"sk-a****mnop#b91bb",       // discriminator too long
		"sk-a****mnop#b91z",        // discriminator not hex
		"sk-a****mnop#b91b#c0de",   // two discriminators
	}
	for _, value := range invalid {
		if head, tail, hashPrefix, ok := ParseKeyIdentifier(value); ok {
			t.Errorf("ParseKeyIdentifier(%q) accepted invalid identifier as %q/%q/%q",
				value, head, tail, hashPrefix)
		}
	}
}

func TestParseKeyIdentifierRoundTripsKeyIdentifier(t *testing.T) {
	const key = "sk-abcdefghijklmnop"
	const keyHash = "b91b0e6129940123456789abcdef0123"

	identifier := KeyIdentifier(key, keyHash)
	head, tail, hashPrefix, ok := ParseKeyIdentifier(identifier)
	if !ok {
		t.Fatalf("ParseKeyIdentifier() rejected KeyIdentifier() output %q", identifier)
	}
	if !KeyMatchesMask(key, head, tail) {
		t.Fatalf("KeyMatchesMask() rejected the key its own identifier was built from")
	}
	if !strings.HasPrefix(keyHash, hashPrefix) {
		t.Fatalf("hash prefix %q is not a prefix of %q", hashPrefix, keyHash)
	}
}

func TestParseKeyIdentifierRoundTripsBareMaskForBackwardCompatibility(t *testing.T) {
	// An identifier copied from a build that displayed the bare mask must still
	// resolve, just without the narrowing suffix.
	const key = "sk-abcdefghijklmnop"
	head, tail, hashPrefix, ok := ParseKeyIdentifier(MaskKeyIdentifier(key))
	if !ok {
		t.Fatalf("ParseKeyIdentifier() rejected MaskKeyIdentifier(%q)", key)
	}
	if hashPrefix != "" {
		t.Fatalf("bare mask yielded hash prefix %q, want none", hashPrefix)
	}
	if !KeyMatchesMask(key, head, tail) {
		t.Fatalf("KeyMatchesMask() rejected the key its own mask was built from")
	}
}

func TestKeyMatchesMaskDistinguishesKeys(t *testing.T) {
	head, tail, _, ok := ParseKeyIdentifier(MaskKeyIdentifier("sk-alpha-key-value-1111"))
	if !ok {
		t.Fatalf("ParseKeyIdentifier() rejected a generated mask")
	}

	if !KeyMatchesMask("sk-alpha-key-value-1111", head, tail) {
		t.Errorf("KeyMatchesMask() rejected the originating key")
	}
	// Shares the head but not the tail.
	if KeyMatchesMask("sk-alpha-key-value-2222", head, tail) {
		t.Errorf("KeyMatchesMask() matched a key with a different tail")
	}
	// A key short enough to have no real head/tail window must never match.
	if KeyMatchesMask("sk-11111", head, tail) {
		t.Errorf("KeyMatchesMask() matched a short key")
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

func TestSanitizeKnownSecretsFailsClosedForNonCanonicalReversibleEncodings(t *testing.T) {
	const secret = `sk-live/a+b="private"`
	unicodeEncoded := `\u0073\u006b\u002d\u006c\u0069\u0076\u0065\u002f\u0061\u002b\u0062\u003d\u0022\u0070\u0072\u0069\u0076\u0061\u0074\u0065\u0022`
	tests := []string{
		`upstream echoed sk-live%2fa%2bb%3d%22private%22`,
		`{"message":"sk-live\/a+b=\"private\""}`,
		`{"message":"` + unicodeEncoded + `"}`,
		`{"message":"sk-live%252fa%252bb%253d%2522private%2522"}`,
	}
	for _, input := range tests {
		if got := SanitizeKnownSecrets(input, secret); got != RedactedValue {
			t.Fatalf("encoded secret did not fail closed: %q", got)
		}
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
