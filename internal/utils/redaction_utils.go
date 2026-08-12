package utils

import (
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	// RedactedValue is used in diagnostic output instead of credential values.
	RedactedValue = "[REDACTED]"
	// keyFingerprintLength keeps identifiers compact while retaining enough entropy
	// to distinguish keys during operational troubleshooting.
	keyFingerprintLength = 12
	// KeyMaskMarker separates the retained head and tail of a masked key.
	KeyMaskMarker = "****"
	// keyMaskEdgeLength is how many leading and trailing characters a mask keeps.
	keyMaskEdgeLength = 4
	// keyMaskLength is the exact length of a well-formed mask: head + marker + tail.
	keyMaskLength = keyMaskEdgeLength*2 + len(KeyMaskMarker)
	// KeyIdentifierSeparator joins a mask to its disambiguating suffix.
	KeyIdentifierSeparator = "#"
	// keyIdentifierDiscriminatorLength is how many key-hash characters the
	// disambiguating suffix keeps. A mask retains only eight characters of the
	// key and provider prefixes are shared, so masks alone collide in practice:
	// with a few thousand keys in one group, two of them sharing both the first
	// and the last four characters is likely rather than hypothetical. The
	// suffix makes the displayed identifier unique per key.
	keyIdentifierDiscriminatorLength = 4
)

var sensitiveNames = map[string]struct{}{
	"accesskey":     {},
	"accesstoken":   {},
	"apikey":        {},
	"apikeyvalue":   {},
	"auth":          {},
	"authkey":       {},
	"authorization": {},
	"authtoken":     {},
	"bearertoken":   {},
	"clientsecret":  {},
	"cookie":        {},
	"credential":    {},
	"credentials":   {},
	"csrftoken":     {},
	"databasedsn":   {},
	"dsn":           {},
	"encryptionkey": {},
	"idtoken":       {},
	"key":           {},
	"keyvalue":      {},
	"password":      {},
	"passwd":        {},
	"privatekey":    {},
	"proxykey":      {},
	"proxykeys":     {},
	"proxyurl":      {},
	"refreshtoken":  {},
	"redisdsn":      {},
	"secret":        {},
	"secretkey":     {},
	"sessiontoken":  {},
	"sessionid":     {},
	"token":         {},
	"xapikey":       {},
	"xgoogapikey":   {},
}

const credentialNamePattern = `access[-_]?key|access[-_]?token|api[-_]?key|auth(?:[-_]?key|[-_]?token)?|authorization|bearer[-_]?token|client[-_]?secret|cookie|credentials?|csrf[-_]?token|database[-_]?dsn|dsn|id[-_]?token|key(?:[-_]?value)?|password|passwd|private[-_]?key|proxy[-_]?(?:keys?|url)|redis[-_]?dsn|refresh[-_]?token|secret(?:[-_]?key)?|session[-_]?(?:id|token)|token|x[-_]?api[-_]?key|x[-_]?goog[-_]?api[-_]?key`

var (
	credentialInQueryPattern    = regexp.MustCompile(`(?i)([?&](?:` + credentialNamePattern + `)=)[^&#\s"'<>\[]+`)
	credentialAssignmentPattern = regexp.MustCompile(`(?i)((?:"?(?:` + credentialNamePattern + `)"?)\s*[:=]\s*"?)[^\s,"'&}\])<>\[]+`)
	bearerTokenPattern          = regexp.MustCompile(`(?i)(\bBearer\s+)[^\s,"'&}\])<>\[]+`)
	urlUserInfoPattern          = regexp.MustCompile(`(://)[^/@\s?#]+@`)
	jsonUnicodeEscapePattern    = regexp.MustCompile(`(?i)\\u[0-9a-f]{4}`)
)

// IsSensitiveName reports whether a configuration or query parameter name is
// credential-shaped. Matching is case-insensitive and ignores punctuation so
// api_key, api-key, apiKey and API_KEY are handled consistently.
func IsSensitiveName(name string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
	_, sensitive := sensitiveNames[normalized]
	return sensitive
}

// SanitizeRawQuery returns a canonical query string with credential values
// replaced. It never returns the original malformed query because doing so
// could put a secret into a log line.
func SanitizeRawQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return RedactedValue
	}
	for name := range query {
		if IsSensitiveName(name) {
			query[name] = []string{RedactedValue}
		}
	}
	return query.Encode()
}

// SanitizeURLForLogging creates a safe request-target representation without
// mutating the URL used by the request handler.
func SanitizeURLForLogging(requestURL *url.URL) string {
	if requestURL == nil {
		return ""
	}

	safeURL := *requestURL
	safeURL.User = nil
	safeURL.RawQuery = SanitizeRawQuery(requestURL.RawQuery)
	return safeURL.String()
}

// SanitizeURLStringForLogging removes credentials from a serialized URL before
// it is persisted or displayed. Structured parsing handles userinfo and every
// sensitive query value. Malformed input is replaced wholesale because a
// broken escape can hide a credential-shaped parameter name from text-based
// matching.
func SanitizeURLStringForLogging(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return RedactedValue
	}
	return SanitizeText(SanitizeURLForLogging(parsed))
}

// SanitizeText removes credentials from URL fragments, structured fields and
// Authorization bearer values embedded in diagnostic or upstream error text.
// It is intentionally conservative: losing a small piece of diagnostic detail
// is preferable to retaining a reusable credential.
func SanitizeText(value string) string {
	if sanitizedJSON, ok := sanitizeJSONCredentials(value); ok {
		return sanitizedJSON
	}

	value = urlUserInfoPattern.ReplaceAllString(value, `${1}`+RedactedValue+`@`)
	value = credentialInQueryPattern.ReplaceAllString(value, `${1}`+RedactedValue)
	value = bearerTokenPattern.ReplaceAllString(value, `${1}`+RedactedValue)
	return credentialAssignmentPattern.ReplaceAllString(value, `${1}`+RedactedValue)
}

// SanitizeKnownSecrets removes credential values that are known at the call
// site, even when an upstream echoes them without a descriptive field name.
// Encoded variants are covered because transport errors frequently include a
// full request URL rather than the decoded query value.
func SanitizeKnownSecrets(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}

		jsonEncoded, _ := json.Marshal(secret)
		candidates := []string{
			secret,
			url.QueryEscape(secret),
			url.PathEscape(secret),
		}
		if len(jsonEncoded) >= 2 {
			candidates = append(candidates, string(jsonEncoded[1:len(jsonEncoded)-1]))
		}

		seen := make(map[string]struct{}, len(candidates))
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			value = strings.ReplaceAll(value, candidate, RedactedValue)
		}
		if containsEncodedSecret(value, secret) {
			return RedactedValue
		}
	}
	return SanitizeText(value)
}

// containsEncodedSecret detects reversible transport encodings that may use
// non-canonical hex case or combine URL and JSON escaping. When such a form is
// found the caller redacts the complete diagnostic value rather than trying to
// reconstruct byte offsets in hostile upstream text.
func containsEncodedSecret(value, secret string) bool {
	if value == "" || secret == "" {
		return false
	}

	seen := map[string]struct{}{value: {}}
	frontier := []string{value}
	for range 2 {
		next := make([]string, 0, len(frontier)*3)
		for _, candidate := range frontier {
			decodedValues := make([]string, 0, 3)
			if decoded, err := url.QueryUnescape(candidate); err == nil {
				decodedValues = append(decodedValues, decoded)
			}
			if decoded, err := url.PathUnescape(candidate); err == nil {
				decodedValues = append(decodedValues, decoded)
			}
			decodedValues = append(decodedValues, decodeJSONEscapes(candidate))

			for _, decoded := range decodedValues {
				if strings.Contains(decoded, secret) {
					return true
				}
				if decoded == candidate {
					continue
				}
				if _, exists := seen[decoded]; exists {
					continue
				}
				seen[decoded] = struct{}{}
				next = append(next, decoded)
			}
		}
		frontier = next
	}
	return false
}

func decodeJSONEscapes(value string) string {
	decoded := jsonUnicodeEscapePattern.ReplaceAllStringFunc(value, func(encoded string) string {
		codePoint, err := strconv.ParseUint(encoded[2:], 16, 16)
		if err != nil {
			return encoded
		}
		return string(rune(codePoint))
	})
	return strings.NewReplacer(
		`\/`, `/`,
		`\"`, `"`,
		`\\`, `\`,
		`\b`, "\b",
		`\f`, "\f",
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
	).Replace(decoded)
}

// sanitizeJSONCredentials handles complete JSON payloads structurally so
// comma-separated strings, arrays, and nested credential fields cannot evade
// the conservative text patterns below. It preserves the original text when
// the payload contains no sensitive fields.
func sanitizeJSONCredentials(value string) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return "", false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", false
	}
	if !redactJSONCredentials(payload) {
		return "", false
	}

	sanitized, err := json.Marshal(payload)
	if err != nil {
		return RedactedValue, true
	}
	return string(sanitized), true
}

func redactJSONCredentials(value any) bool {
	redacted := false
	switch typedValue := value.(type) {
	case map[string]any:
		for name, fieldValue := range typedValue {
			if IsSensitiveName(name) {
				typedValue[name] = RedactedValue
				redacted = true
				continue
			}
			if redactJSONCredentials(fieldValue) {
				redacted = true
			}
		}
	case []any:
		for _, item := range typedValue {
			if redactJSONCredentials(item) {
				redacted = true
			}
		}
	}
	return redacted
}

// KeyFingerprint converts an existing one-way key hash into a compact,
// non-reversible identifier suitable for logs and user interfaces.
func KeyFingerprint(keyHash string) string {
	keyHash = strings.TrimSpace(keyHash)
	if keyHash == "" {
		return ""
	}
	if len(keyHash) > keyFingerprintLength {
		keyHash = keyHash[:keyFingerprintLength]
	}
	return "fp:" + strings.ToLower(keyHash)
}

// ParseKeyFingerprint validates a display fingerprint and returns the hash
// prefix that can be used for request-log lookup.
func ParseKeyFingerprint(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "fp:") {
		return "", false
	}
	prefix := strings.TrimPrefix(value, "fp:")
	if len(prefix) != keyFingerprintLength {
		return "", false
	}
	for _, char := range prefix {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return "", false
		}
	}
	return prefix, true
}

// ParseKeyIdentifier recognises a request-log key identifier as produced by
// KeyIdentifier and returns the retained head and tail of the mask plus the
// key-hash prefix from the disambiguating suffix, if one is present.
//
// Both forms are accepted:
//
//	"AAAA****ZZZZ"        -> head/tail only; matches every key with that mask
//	"AAAA****ZZZZ#hhhh"   -> additionally narrowed to one key hash prefix
//
// The bare form stays valid so an identifier copied from an older build, or a
// mask typed by hand, still resolves.
//
// The shape check is deliberately exact so a complete upstream key can never be
// mistaken for an identifier and diverted away from exact-hash lookup. Mask
// matching is case-sensitive because API keys are; the hash suffix is
// lower-cased because hex digests are stored lower-case.
func ParseKeyIdentifier(value string) (head, tail, hashPrefix string, ok bool) {
	value = strings.TrimSpace(value)

	mask := value
	if separatorIndex := strings.Index(value, KeyIdentifierSeparator); separatorIndex >= 0 {
		mask = value[:separatorIndex]
		suffix := strings.ToLower(value[separatorIndex+len(KeyIdentifierSeparator):])
		if len(suffix) != keyIdentifierDiscriminatorLength || !isLowerHex(suffix) {
			return "", "", "", false
		}
		hashPrefix = suffix
	}

	if len(mask) != keyMaskLength {
		return "", "", "", false
	}
	if mask[keyMaskEdgeLength:keyMaskEdgeLength+len(KeyMaskMarker)] != KeyMaskMarker {
		return "", "", "", false
	}

	head = mask[:keyMaskEdgeLength]
	tail = mask[keyMaskEdgeLength+len(KeyMaskMarker):]
	// A head or tail containing the marker itself would make the split ambiguous
	// and cannot come from MaskKeyIdentifier applied to a real key.
	if strings.Contains(head, "*") || strings.Contains(tail, "*") {
		return "", "", "", false
	}
	return head, tail, hashPrefix, true
}

func isLowerHex(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// KeyMatchesMask reports whether a plaintext key would render as the given mask.
func KeyMatchesMask(key, head, tail string) bool {
	if len(key) <= keyMaskEdgeLength*2 {
		return false
	}
	return strings.HasPrefix(key, head) && strings.HasSuffix(key, tail)
}
