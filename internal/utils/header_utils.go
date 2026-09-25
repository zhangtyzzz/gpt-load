package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
)

// HeaderVariableContext holds context data for variable resolution
type HeaderVariableContext struct {
	ClientIP string
	Group    *models.Group
	APIKey   *models.APIKey
}

// ResolveHeaderVariables resolves dynamic variables in header values
func ResolveHeaderVariables(value string, ctx *HeaderVariableContext) string {
	if ctx == nil {
		return value
	}

	now := time.Now()
	result := value

	// Replace all supported variables
	variables := map[string]string{
		"${CLIENT_IP}":    ctx.ClientIP,
		"${TIMESTAMP_MS}": strconv.FormatInt(now.UnixMilli(), 10),
		"${TIMESTAMP_S}":  strconv.FormatInt(now.Unix(), 10),
	}

	if ctx.Group != nil {
		variables["${GROUP_NAME}"] = ctx.Group.Name
	}

	if ctx.APIKey != nil {
		variables["${API_KEY}"] = ctx.APIKey.KeyValue
		variables["${API_KEY_FINGERPRINT}"] = APIKeyFingerprint(ctx.APIKey)
	}

	// Replace variables in the value
	for variable, replacement := range variables {
		result = strings.ReplaceAll(result, variable, replacement)
	}

	return result
}

// APIKeyFingerprint returns the non-reversible routing identity for an
// upstream key. Keep proxy authentication and custom headers on the same
// identity so changing access mode does not change account affinity.
func APIKeyFingerprint(apiKey *models.APIKey) string {
	if apiKey == nil {
		return ""
	}
	fingerprint := KeyFingerprint(apiKey.KeyHash)
	plainHash := sha256.Sum256([]byte(apiKey.KeyValue))
	if fingerprint == "" || strings.EqualFold(apiKey.KeyHash, hex.EncodeToString(plainHash[:])) {
		// Without an encryption key, KeyHash is plain SHA-256. Use the
		// database ID so a weak credential cannot be guessed offline.
		return "key-id:" + strconv.FormatUint(uint64(apiKey.ID), 10)
	}
	return fingerprint
}

// ApplyHeaderRules applies header rules to the HTTP request
func ApplyHeaderRules(req *http.Request, rules []models.HeaderRule, ctx *HeaderVariableContext) {
	if req == nil || len(rules) == 0 {
		return
	}

	for _, rule := range rules {
		canonicalKey := http.CanonicalHeaderKey(rule.Key)

		switch rule.Action {
		case "remove":
			req.Header.Del(canonicalKey)
		case "set":
			resolvedValue := ResolveHeaderVariables(rule.Value, ctx)
			req.Header.Set(canonicalKey, resolvedValue)
		}
	}
}

// NewHeaderVariableContextFromGin creates HeaderVariableContext from Gin context
func NewHeaderVariableContextFromGin(c *gin.Context, group *models.Group, apiKey *models.APIKey) *HeaderVariableContext {
	if c == nil {
		return nil
	}

	return &HeaderVariableContext{
		ClientIP: c.ClientIP(),
		Group:    group,
		APIKey:   apiKey,
	}
}

// NewHeaderVariableContext creates HeaderVariableContext without Gin context
func NewHeaderVariableContext(group *models.Group, apiKey *models.APIKey) *HeaderVariableContext {
	return &HeaderVariableContext{
		ClientIP: "127.0.0.1",
		Group:    group,
		APIKey:   apiKey,
	}
}
