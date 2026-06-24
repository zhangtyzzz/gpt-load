package keypool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// affinityRegexCache caches compiled regexes for affinity rules to avoid repeated compilation.
var affinityRegexCache sync.Map

// AffinityManager handles key affinity extraction and mapping.
type AffinityManager struct {
	store store.Store
}

// NewAffinityManager creates a new AffinityManager instance.
func NewAffinityManager(store store.Store) *AffinityManager {
	return &AffinityManager{store: store}
}

// AffinityResult holds the result of affinity value extraction.
type AffinityResult struct {
	Value       string              // The extracted affinity value
	Hash        string              // SHA256 hash of the value
	MatchedRule *models.AffinityRule // The rule that matched
}

// ExtractValue extracts the affinity value from the request based on the group's affinity rules.
// Returns an AffinityResult with empty Value/Hash if no rule matches or no value could be extracted.
func (am *AffinityManager) ExtractValue(c *gin.Context, body []byte, model string, rules []models.AffinityRule) AffinityResult {
	if len(rules) == 0 {
		return AffinityResult{}
	}

	for i := range rules {
		rule := &rules[i]
		if am.matchRule(c, model, rule) {
			value := am.extractByKeySource(c, body, &rule.KeySource)
			if value != "" {
				hash := ComputeAffinityHash(value)
				logrus.WithFields(logrus.Fields{
					"rule_name":    rule.Name,
					"affinityHash": hash[:8],
				}).Debug("Affinity value extracted")
				return AffinityResult{
					Value:       value,
					Hash:        hash,
					MatchedRule: rule,
				}
			}
		}
	}

	return AffinityResult{}
}

// matchRule checks if a request matches an affinity rule's conditions.
func (am *AffinityManager) matchRule(c *gin.Context, model string, rule *models.AffinityRule) bool {
	// Check path regex
	if rule.Match.PathRegex != "" {
		regex, err := compileAffinityRegex(rule.Match.PathRegex)
		if err != nil {
			logrus.WithError(err).WithField("pattern", rule.Match.PathRegex).Warn("Invalid affinity path regex")
			return false
		}
		if !regex.MatchString(c.Request.URL.Path) {
			return false
		}
	}

	// Check model regex
	if rule.Match.ModelRegex != "" {
		regex, err := compileAffinityRegex(rule.Match.ModelRegex)
		if err != nil {
			logrus.WithError(err).WithField("pattern", rule.Match.ModelRegex).Warn("Invalid affinity model regex")
			return false
		}
		if !regex.MatchString(model) {
			return false
		}
	}

	return true
}

// extractByKeySource extracts the affinity value based on the key source type.
func (am *AffinityManager) extractByKeySource(c *gin.Context, body []byte, source *models.AffinityKeySource) string {
	switch source.Type {
	case "header":
		if source.Key == "" {
			return ""
		}
		return c.Request.Header.Get(source.Key)

	case "body_json":
		if source.Path == "" || len(body) == 0 {
			return ""
		}
		return extractJSONPath(body, source.Path)

	case "body_regex":
		if source.Pattern == "" || len(body) == 0 {
			return ""
		}
		return extractByBodyRegex(body, source.Pattern)

	default:
		logrus.WithField("type", source.Type).Warn("Unknown affinity key source type")
		return ""
	}
}

// extractJSONPath extracts a value from JSON bytes using a dot-notation path.
// Supports paths like "user", "metadata.user_id", "data.0.id".
func extractJSONPath(data []byte, path string) string {
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return ""
	}

	parts := strings.Split(path, ".")
	var current interface{} = result

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return ""
			}
			current = val
		case []interface{}:
			// Try to parse as array index
			idx := 0
			for _, ch := range part {
				if ch >= '0' && ch <= '9' {
					idx = idx*10 + int(ch-'0')
				} else {
					return ""
				}
			}
			if idx < 0 || idx >= len(v) {
				return ""
			}
			current = v[idx]
		default:
			return ""
		}
	}

	if current == nil {
		return ""
	}
	return fmt.Sprintf("%v", current)
}

// extractByBodyRegex extracts a value from the body using a regex with a named capture group "value".
func extractByBodyRegex(body []byte, pattern string) string {
	regex, err := compileAffinityRegex(pattern)
	if err != nil {
		logrus.WithError(err).WithField("pattern", pattern).Warn("Invalid affinity body regex")
		return ""
	}

	match := regex.FindSubmatch(body)
	if match == nil {
		return ""
	}

	// Find the named group "value"
	subexpNames := regex.SubexpNames()
	for i, name := range subexpNames {
		if name == "value" && i < len(match) {
			return string(match[i])
		}
	}

	// If no named group "value", try the first capture group
	if len(match) > 1 {
		return string(match[1])
	}

	return ""
}

// ComputeAffinityHash computes a SHA256 hash of the affinity value.
func ComputeAffinityHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

// GetMapping retrieves the key ID mapped to an affinity hash for a group.
// Returns the key ID string, or empty string if not found.
func (am *AffinityManager) GetMapping(groupID uint, affinityHash string) (string, error) {
	cacheKey := buildAffinityCacheKey(groupID, affinityHash)
	value, err := am.store.Get(cacheKey)
	if err != nil {
		if err == store.ErrNotFound {
			return "", nil
		}
		return "", fmt.Errorf("failed to get affinity mapping: %w", err)
	}
	return string(value), nil
}

// SetMapping stores an affinity hash -> key ID mapping with TTL.
func (am *AffinityManager) SetMapping(groupID uint, affinityHash string, keyID uint, ttl time.Duration) error {
	cacheKey := buildAffinityCacheKey(groupID, affinityHash)
	if ttl <= 0 {
		ttl = 3600 * time.Second // Default 1 hour
	}
	return am.store.Set(cacheKey, []byte(fmt.Sprintf("%d", keyID)), ttl)
}

// buildAffinityCacheKey builds the store key for an affinity mapping.
func buildAffinityCacheKey(groupID uint, affinityHash string) string {
	return fmt.Sprintf("affinity:%d:%s", groupID, affinityHash)
}

// compileAffinityRegex compiles and caches a regex pattern.
func compileAffinityRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := affinityRegexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	affinityRegexCache.Store(pattern, regex)
	return regex, nil
}

// GetEffectiveTTL returns the effective TTL for an affinity rule.
func GetEffectiveTTL(rule *models.AffinityRule, defaultTTL int) time.Duration {
	if rule != nil && rule.TTL > 0 {
		return time.Duration(rule.TTL) * time.Second
	}
	if defaultTTL > 0 {
		return time.Duration(defaultTTL) * time.Second
	}
	return 3600 * time.Second
}

// ExtractModelFromBody extracts the model name from the request body.
func ExtractModelFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	return extractJSONPath(body, "model")
}

// ExtractModelFromPath extracts the model name from the URL path for endpoints like /v1/models/{model}.
func ExtractModelFromPath(path string) string {
	// Handle /v1/models/{model} style paths
	if strings.HasPrefix(path, "/v1/models/") {
		parts := strings.Split(strings.TrimPrefix(path, "/v1/models/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}
	return ""
}
