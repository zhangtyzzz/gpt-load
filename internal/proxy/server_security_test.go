package proxy

import (
	"strings"
	"testing"
)

func TestSanitizeUpstreamErrorRemovesDirectCredentialEcho(t *testing.T) {
	const secret = "sk-upstream-direct-echo"
	body := []byte(`{"error":{"message":"Incorrect API key provided: ` + secret + `"}}`)

	safeBody, parsed := sanitizeUpstreamError(body, secret)
	for name, value := range map[string]string{"response body": safeBody, "parsed error": parsed} {
		if strings.Contains(value, secret) {
			t.Fatalf("%s leaked upstream credential: %s", name, value)
		}
	}
	if !strings.Contains(safeBody, "[REDACTED]") {
		t.Fatalf("safe response body has no redaction marker: %s", safeBody)
	}
}
