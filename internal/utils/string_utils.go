package utils

import (
	"fmt"
	"strings"
)

// MaskAPIKey masks an API key for safe logging.
func MaskAPIKey(key string) string {
	length := len(key)
	if length <= 8 {
		return key
	}
	return fmt.Sprintf("%s****%s", key[:4], key[length-4:])
}

// MaskKeyIdentifier masks a key for display in request logs and exports, using
// the same first-four/last-four window the key management UI shows so an
// operator can match a log row against a key row by eye.
//
// Unlike MaskAPIKey it never returns a key verbatim: a key short enough that
// the window would cover all of it is reduced to the mask marker alone. Request
// logs reach a wider surface than MaskAPIKey's diagnostic callers (API
// responses and downloadable CSV exports), so a short key must not survive.
func MaskKeyIdentifier(key string) string {
	length := len(key)
	if length == 0 {
		return ""
	}
	if length <= 8 {
		return KeyMaskMarker
	}
	return key[:4] + KeyMaskMarker + key[length-4:]
}

// TruncateString shortens a string to a maximum length.
func TruncateString(s string, maxLength int) string {
	if len(s) > maxLength {
		return s[:maxLength]
	}
	return s
}

// SplitAndTrim splits a string by a separator
func SplitAndTrim(s string, sep string) []string {
	if s == "" {
		return []string{}
	}

	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// StringToSet converts a separator-delimited string into a set
func StringToSet(s string, sep string) map[string]struct{} {
	parts := SplitAndTrim(s, sep)
	if len(parts) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		set[part] = struct{}{}
	}
	return set
}
