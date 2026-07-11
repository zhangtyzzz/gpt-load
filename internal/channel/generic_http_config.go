package channel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	GenericHTTPChannelType = "generic-http"

	GenericAuthHeader = "header"

	GenericStreamNever  = "never"
	GenericStreamAuto   = "auto"
	GenericStreamAlways = "always"

	defaultGenericRequestBodyLimit = int64(16 << 20)
	maxGenericRequestBodyLimit     = int64(64 << 20)
	defaultGenericErrorBodyLimit   = int64(64 << 10)
	maxGenericErrorBodyLimit       = int64(1 << 20)
	defaultValidationResponseLimit = defaultGenericErrorBodyLimit
)

var httpTokenPattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

type GenericHTTPAuthConfig struct {
	Placement string `json:"placement"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
}

type GenericHTTPValidationConfig struct {
	Enabled         bool              `json:"enabled"`
	BaseURL         string            `json:"base_url"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Headers         map[string]string `json:"headers"`
	Body            any               `json:"body"`
	ValidStatuses   []int             `json:"valid_statuses"`
	InvalidStatuses []int             `json:"invalid_statuses"`
}

type GenericHTTPRetryConfig struct {
	SafeMethods      []string `json:"safe_methods"`
	FailoverStatuses []int    `json:"failover_statuses"`
}

// GenericHTTPConfig is deliberately data-only. It supports common credential
// injection and health probes without allowing executable templates or scripts.
type GenericHTTPConfig struct {
	Version             int                         `json:"version"`
	PresetID            string                      `json:"preset_id"`
	Auth                GenericHTTPAuthConfig       `json:"auth"`
	Validation          GenericHTTPValidationConfig `json:"validation"`
	StreamMode          string                      `json:"stream_mode"`
	Retry               GenericHTTPRetryConfig      `json:"retry"`
	MaxRequestBodyBytes int64                       `json:"max_request_body_bytes"`
	MaxErrorBodyBytes   int64                       `json:"max_error_body_bytes"`
}

func ParseGenericHTTPConfig(raw []byte) (GenericHTTPConfig, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return GenericHTTPConfig{}, fmt.Errorf("channel_config is required for %s", GenericHTTPChannelType)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cfg GenericHTTPConfig
	if err := decoder.Decode(&cfg); err != nil {
		return GenericHTTPConfig{}, fmt.Errorf("invalid generic HTTP channel_config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return GenericHTTPConfig{}, fmt.Errorf("invalid generic HTTP channel_config: trailing JSON data")
	}

	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return GenericHTTPConfig{}, fmt.Errorf("unsupported generic HTTP config version %d", cfg.Version)
	}
	cfg.PresetID = strings.TrimSpace(cfg.PresetID)
	if len(cfg.PresetID) > 100 {
		return GenericHTTPConfig{}, fmt.Errorf("preset_id is too long")
	}

	cfg.Auth.Placement = strings.ToLower(strings.TrimSpace(cfg.Auth.Placement))
	cfg.Auth.Name = strings.TrimSpace(cfg.Auth.Name)
	if cfg.Auth.Placement != GenericAuthHeader {
		return GenericHTTPConfig{}, fmt.Errorf("auth.placement must be header")
	}
	if !IsValidHTTPHeaderName(cfg.Auth.Name) {
		return GenericHTTPConfig{}, fmt.Errorf("auth.name is not a valid HTTP token")
	}
	if !IsValidHTTPHeaderValue(cfg.Auth.Prefix) || len(cfg.Auth.Prefix) > 128 {
		return GenericHTTPConfig{}, fmt.Errorf("auth.prefix contains invalid characters or is too long")
	}
	if IsReservedProxyHeader(cfg.Auth.Name) {
		return GenericHTTPConfig{}, fmt.Errorf("auth header %q is protected", cfg.Auth.Name)
	}
	cfg.Auth.Name = http.CanonicalHeaderKey(cfg.Auth.Name)

	if cfg.StreamMode == "" {
		cfg.StreamMode = GenericStreamAuto
	}
	if cfg.StreamMode != GenericStreamNever && cfg.StreamMode != GenericStreamAuto && cfg.StreamMode != GenericStreamAlways {
		return GenericHTTPConfig{}, fmt.Errorf("unsupported stream_mode %q", cfg.StreamMode)
	}

	if cfg.MaxRequestBodyBytes == 0 {
		cfg.MaxRequestBodyBytes = defaultGenericRequestBodyLimit
	}
	if cfg.MaxRequestBodyBytes < 1 || cfg.MaxRequestBodyBytes > maxGenericRequestBodyLimit {
		return GenericHTTPConfig{}, fmt.Errorf("max_request_body_bytes must be between 1 and %d", maxGenericRequestBodyLimit)
	}
	if cfg.MaxErrorBodyBytes == 0 {
		cfg.MaxErrorBodyBytes = defaultGenericErrorBodyLimit
	}
	if cfg.MaxErrorBodyBytes < 1 || cfg.MaxErrorBodyBytes > maxGenericErrorBodyLimit {
		return GenericHTTPConfig{}, fmt.Errorf("max_error_body_bytes must be between 1 and %d", maxGenericErrorBodyLimit)
	}

	if err := normalizeValidationConfig(&cfg); err != nil {
		return GenericHTTPConfig{}, err
	}
	if err := normalizeRetryConfig(&cfg.Retry); err != nil {
		return GenericHTTPConfig{}, err
	}
	return cfg, nil
}

func NormalizeGenericHTTPConfig(raw []byte) ([]byte, GenericHTTPConfig, error) {
	cfg, err := ParseGenericHTTPConfig(raw)
	if err != nil {
		return nil, GenericHTTPConfig{}, err
	}
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, GenericHTTPConfig{}, fmt.Errorf("marshal generic HTTP channel_config: %w", err)
	}
	return normalized, cfg, nil
}

func normalizeValidationConfig(cfg *GenericHTTPConfig) error {
	v := &cfg.Validation
	if !v.Enabled {
		v.Method = ""
		v.Path = ""
		v.BaseURL = ""
		v.Headers = map[string]string{}
		v.Body = nil
		v.ValidStatuses = []int{}
		v.InvalidStatuses = []int{}
		return nil
	}

	v.Method = strings.ToUpper(strings.TrimSpace(v.Method))
	if v.Method == "" {
		v.Method = http.MethodGet
	}
	if v.Method != http.MethodGet && v.Method != http.MethodHead && v.Method != http.MethodPost {
		return fmt.Errorf("validation.method must be GET, HEAD, or POST")
	}
	v.Path = strings.TrimSpace(v.Path)
	if v.Path == "" || !strings.HasPrefix(v.Path, "/") || strings.Contains(v.Path, "://") {
		return fmt.Errorf("validation.path must be an absolute path without a scheme")
	}
	if v.BaseURL != "" {
		parsed, err := ParseAbsoluteHTTPURL(v.BaseURL)
		if err != nil {
			return fmt.Errorf("validation.base_url must be an absolute HTTP(S) URL without credentials or fragment")
		}
		v.BaseURL = strings.TrimRight(parsed.String(), "/")
	}
	if (v.Method == http.MethodGet || v.Method == http.MethodHead) && v.Body != nil {
		return fmt.Errorf("validation.body is not allowed for GET or HEAD")
	}
	if v.Body != nil {
		body, err := json.Marshal(v.Body)
		if err != nil || len(body) > 64<<10 {
			return fmt.Errorf("validation.body must be JSON smaller than 65536 bytes")
		}
	}

	normalizedHeaders := make(map[string]string, len(v.Headers))
	seenHeaders := make(map[string]struct{}, len(v.Headers))
	for name, value := range v.Headers {
		name = strings.TrimSpace(name)
		if !IsValidHTTPHeaderName(name) || IsReservedProxyHeader(name) || !IsValidHTTPHeaderValue(value) {
			return fmt.Errorf("validation header %q is invalid or protected", name)
		}
		if strings.EqualFold(name, cfg.Auth.Name) {
			return fmt.Errorf("validation header %q conflicts with auth header", name)
		}
		canonicalName := http.CanonicalHeaderKey(name)
		foldedName := strings.ToLower(canonicalName)
		if _, exists := seenHeaders[foldedName]; exists {
			return fmt.Errorf("validation header %q is duplicated", canonicalName)
		}
		seenHeaders[foldedName] = struct{}{}
		normalizedHeaders[canonicalName] = value
	}
	v.Headers = normalizedHeaders

	if len(v.ValidStatuses) == 0 {
		for status := 200; status <= 299; status++ {
			v.ValidStatuses = append(v.ValidStatuses, status)
		}
	}
	if len(v.InvalidStatuses) == 0 {
		v.InvalidStatuses = []int{http.StatusUnauthorized}
	}
	var err error
	v.ValidStatuses, err = normalizeHTTPStatuses("validation.valid_statuses", v.ValidStatuses, 100)
	if err != nil {
		return err
	}
	v.InvalidStatuses, err = normalizeHTTPStatuses("validation.invalid_statuses", v.InvalidStatuses, 100)
	if err != nil {
		return err
	}
	validSet := make(map[int]struct{}, len(v.ValidStatuses))
	for _, status := range v.ValidStatuses {
		validSet[status] = struct{}{}
	}
	for _, status := range v.InvalidStatuses {
		if _, exists := validSet[status]; exists {
			return fmt.Errorf("validation status %d appears in both valid_statuses and invalid_statuses", status)
		}
	}
	return nil
}

func normalizeHTTPStatuses(name string, statuses []int, minimum int) ([]int, error) {
	seen := make(map[int]struct{}, len(statuses))
	normalized := make([]int, 0, len(statuses))
	for _, status := range statuses {
		if status < minimum || status > 599 {
			return nil, fmt.Errorf("%s contains invalid status %d", name, status)
		}
		if _, exists := seen[status]; exists {
			continue
		}
		seen[status] = struct{}{}
		normalized = append(normalized, status)
	}
	sort.Ints(normalized)
	return normalized, nil
}

func normalizeRetryConfig(retry *GenericHTTPRetryConfig) error {
	if len(retry.SafeMethods) == 0 {
		retry.SafeMethods = []string{http.MethodGet, http.MethodHead}
	}
	seenMethods := make(map[string]struct{}, len(retry.SafeMethods))
	normalizedMethods := make([]string, 0, len(retry.SafeMethods))
	for _, method := range retry.SafeMethods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" || !httpTokenPattern.MatchString(method) {
			return fmt.Errorf("retry.safe_methods contains invalid method %q", method)
		}
		if _, ok := seenMethods[method]; ok {
			continue
		}
		seenMethods[method] = struct{}{}
		normalizedMethods = append(normalizedMethods, method)
	}
	retry.SafeMethods = normalizedMethods
	sort.Strings(retry.SafeMethods)

	seenStatuses := make(map[int]struct{}, len(retry.FailoverStatuses))
	normalizedStatuses := make([]int, 0, len(retry.FailoverStatuses))
	for _, status := range retry.FailoverStatuses {
		if status < 300 || status > 599 {
			return fmt.Errorf("retry.failover_statuses contains invalid status %d", status)
		}
		if _, ok := seenStatuses[status]; ok {
			continue
		}
		seenStatuses[status] = struct{}{}
		normalizedStatuses = append(normalizedStatuses, status)
	}
	sort.Ints(normalizedStatuses)
	retry.FailoverStatuses = normalizedStatuses
	return nil
}

// IsReservedProxyHeader reports fixed transport headers that configuration
// must never control. The configured credential header is protected
// dynamically by the service because its name is configuration-defined.
func IsReservedProxyHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "content-length", "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade", "last-event-id", "x-gpt-load-key":
		return true
	default:
		return false
	}
}

// IsValidHTTPHeaderName reports whether a name is a non-empty RFC 9110 token.
func IsValidHTTPHeaderName(name string) bool {
	return httpTokenPattern.MatchString(strings.TrimSpace(name))
}

// IsValidHTTPHeaderValue rejects CR/LF and other control bytes that net/http
// would refuse only when the upstream request is already being sent.
func IsValidHTTPHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] == '\t' {
			continue
		}
		if value[i] < 0x20 || value[i] == 0x7f {
			return false
		}
	}
	return true
}

// ParseAbsoluteHTTPURL strictly validates an upstream target. url.Parse alone
// accepts relative and opaque references, which are not valid proxy targets.
func ParseAbsoluteHTTPURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.Contains(trimmed, "#") {
		return nil, fmt.Errorf("URL fragment is not allowed")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil {
		return nil, fmt.Errorf("invalid URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if !parsed.IsAbs() || parsed.Opaque != "" || (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return nil, fmt.Errorf("URL must be absolute HTTP(S) with a host and without credentials, query, or fragment")
	}
	parsed.Scheme = scheme
	return parsed, nil
}

func containsNewline(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func containsStatus(statuses []int, status int) bool {
	for _, candidate := range statuses {
		if candidate == status {
			return true
		}
	}
	return false
}
