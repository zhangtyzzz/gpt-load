package utils

import (
	"strings"
	"testing"
)

func TestMaskKeyIdentifierKeepsRecognizableEdges(t *testing.T) {
	const key = "sk-abcdefghijklmnop"
	if got, want := MaskKeyIdentifier(key), "sk-a****mnop"; got != want {
		t.Fatalf("MaskKeyIdentifier(%q) = %q, want %q", key, got, want)
	}
}

func TestMaskKeyIdentifierMatchesMaskAPIKeyForLongKeys(t *testing.T) {
	// The two helpers must agree wherever both are safe to use, so a key looks the
	// same in diagnostic logs and in the request-log identifier column.
	for _, key := range []string{
		"sk-abcdefghijklmnop",
		"sk-proj-0123456789abcdef",
		"AIzaSyD-1234567890abcdefghij",
	} {
		if got, want := MaskKeyIdentifier(key), MaskAPIKey(key); got != want {
			t.Errorf("MaskKeyIdentifier(%q) = %q, MaskAPIKey() = %q", key, got, want)
		}
	}
}

func TestMaskKeyIdentifierNeverRevealsShortKey(t *testing.T) {
	// MaskAPIKey returns short keys verbatim. That is tolerable for its
	// diagnostic callers but not for request logs, which are served by the API and
	// downloadable as CSV.
	for _, key := range []string{"a", "sk-12345", "12345678"} {
		got := MaskKeyIdentifier(key)
		if got != KeyMaskMarker {
			t.Errorf("MaskKeyIdentifier(%q) = %q, want %q", key, got, KeyMaskMarker)
		}
		if got == key {
			t.Errorf("MaskKeyIdentifier(%q) returned the key verbatim", key)
		}
	}
}

func TestMaskKeyIdentifierEmptyKeyStaysEmpty(t *testing.T) {
	// An empty key means the log row had no key selected; it must not render as a
	// mask, which would imply a key existed.
	if got := MaskKeyIdentifier(""); got != "" {
		t.Fatalf("MaskKeyIdentifier(\"\") = %q, want empty", got)
	}
}

func TestMaskKeyIdentifierDoesNotLeakMiddleOfKey(t *testing.T) {
	const secretMiddle = "SUPERSECRETMIDDLE"
	key := "sk-a" + secretMiddle + "mnop"
	if got := MaskKeyIdentifier(key); strings.Contains(got, secretMiddle) {
		t.Fatalf("MaskKeyIdentifier leaked the middle of the key: %q", got)
	}
}
