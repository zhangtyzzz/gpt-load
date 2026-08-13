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

// KeyIdentifier returns the identifier shown for a key in request logs: its
// mask, followed by the fingerprint body as a discriminator.
//
// The mask alone is not unique. It retains eight characters, and provider
// prefixes are shared — every OpenAI project key starts "sk-p" — so two keys in
// one group sharing both the first and the last four characters is likely at a
// few thousand keys, not hypothetical. Two log rows would then be
// indistinguishable, and an operator would read one key's failure as another's.
//
// The discriminator removes that ambiguity while keeping the mask as an exact
// prefix, so the value still matches the key management column by eye. It is the
// full fingerprint body rather than a shortened hash so that it cannot collide
// where the mask already did: the identifier is unique exactly when the
// fingerprint is.
//
// The discriminator exposes nothing new: it is the same hash prefix
// KeyFingerprint has always published.
func KeyIdentifier(key, keyHash string) string {
	mask := MaskKeyIdentifier(key)
	if mask == "" {
		return ""
	}

	discriminator := strings.ToLower(strings.TrimSpace(keyHash))
	if len(discriminator) > keyIdentifierDiscriminatorLength {
		discriminator = discriminator[:keyIdentifierDiscriminatorLength]
	}
	if discriminator == "" {
		return mask
	}
	return mask + KeyIdentifierSeparator + discriminator
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
