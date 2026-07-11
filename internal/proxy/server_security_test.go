package proxy

import (
	"net/http"
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

func TestSanitizeUpstreamErrorRemovesNonCanonicalEncodedCredentialEcho(t *testing.T) {
	const secret = `sk-live/a+b="private"`
	for _, body := range [][]byte{
		[]byte(`{"error":{"message":"sk-live%2fa%2bb%3d%22private%22"}}`),
		[]byte(`{"error":{"message":"sk-live\/a+b=\"private\""}}`),
		[]byte(`{"error":{"message":"\u0073\u006b\u002d\u006c\u0069\u0076\u0065\u002f\u0061\u002b\u0062\u003d\u0022\u0070\u0072\u0069\u0076\u0061\u0074\u0065\u0022"}}`),
	} {
		safeBody, parsed := sanitizeUpstreamError(body, secret)
		for name, value := range map[string]string{"response body": safeBody, "parsed error": parsed} {
			if value != "[REDACTED]" && strings.Contains(value, secret) {
				t.Fatalf("%s leaked encoded upstream credential: %s", name, value)
			}
		}
		if !strings.Contains(safeBody, "[REDACTED]") {
			t.Fatalf("encoded response body has no redaction marker: %s", safeBody)
		}
	}
}

func TestSanitizeUpstreamResponseHeadersPreservesMultiplicity(t *testing.T) {
	const secret = "sk-header-echo"
	headers := http.Header{
		"Location": []string{"/retry?token=" + secret},
		"X-Multi":  []string{"safe", "echo-" + secret},
	}
	sanitizeUpstreamResponseHeaders(headers, secret)
	if strings.Contains(headers.Get("Location"), secret) || strings.Contains(strings.Join(headers.Values("X-Multi"), ","), secret) {
		t.Fatalf("response headers leaked credential: %#v", headers)
	}
	if got := headers.Values("X-Multi"); len(got) != 2 {
		t.Fatalf("response header multiplicity changed: %#v", got)
	}
}

func TestSanitizeUpstreamResponseHeadersFailsClosedForEncodedCredential(t *testing.T) {
	const secret = `sk-live/a+b="private"`
	headers := http.Header{"X-Echo": []string{"sk-live%2fa%2bb%3d%22private%22"}}
	sanitizeUpstreamResponseHeaders(headers, secret)
	if got := headers.Get("X-Echo"); got != "[REDACTED]" {
		t.Fatalf("encoded credential header = %q, want redaction", got)
	}
}
