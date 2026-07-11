// Package middleware provides HTTP middleware for the application
package middleware

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"gpt-load/internal/channel"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/response"
	"gpt-load/internal/types"
	"gpt-load/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Logger creates a high-performance logging middleware
func Logger(config types.LogConfig) gin.HandlerFunc {
	return func(c *gin.Context) {

		start := time.Now()
		path := c.Request.URL.Path
		raw := utils.SanitizeRawQuery(c.Request.URL.RawQuery)

		// Process request
		c.Next()

		// Calculate response time
		latency := time.Since(start)

		// Get basic information
		method := c.Request.Method
		statusCode := c.Writer.Status()

		// Build full path (avoid string concatenation)
		fullPath := path
		if raw != "" {
			fullPath = path + "?" + raw
		}

		// Get key information (if exists)
		keyInfo := ""
		if keyIndex, exists := c.Get("keyIndex"); exists {
			// Log the non-sensitive internal index only. A preview still reveals
			// credential material and can be correlated via request-log fingerprint.
			keyInfo = fmt.Sprintf(" - Key[%v]", keyIndex)
		}

		// Get retry information (if exists)
		retryInfo := ""
		if retryCount, exists := c.Get("retryCount"); exists {
			retryInfo = fmt.Sprintf(" - Retry[%d]", retryCount)
		}

		// Filter health check and other monitoring endpoint logs to reduce noise
		if isMonitoringEndpoint(path) {
			// Only log errors for monitoring endpoints
			if statusCode >= 400 {
				logrus.Warnf("%s %s - %d - %v", method, fullPath, statusCode, latency)
			}
			return
		}

		// Choose log level based on status code
		if statusCode >= 500 {
			logrus.Errorf("%s %s - %d - %v%s%s", method, fullPath, statusCode, latency, keyInfo, retryInfo)
		} else if statusCode >= 400 {
			logrus.Warnf("%s %s - %d - %v%s%s", method, fullPath, statusCode, latency, keyInfo, retryInfo)
		} else {
			logrus.Infof("%s %s - %d - %v%s%s", method, fullPath, statusCode, latency, keyInfo, retryInfo)
		}
	}
}

// CORS creates a CORS middleware
func CORS(config types.CORSConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Enabled {
			c.Next()
			return
		}

		origin := c.Request.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range config.AllowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			c.Header("Access-Control-Allow-Origin", origin)
		}

		// Set other CORS headers
		c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
		c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))

		if config.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		// Handle preflight requests
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Auth creates an authentication middleware
func Auth(authConfig types.AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if isMonitoringEndpoint(path) {
			c.Next()
			return
		}

		key := extractAuthKey(c, true)

		isValid := key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(authConfig.Key)) == 1

		if !isValid {
			response.Error(c, app_errors.ErrUnauthorized)
			c.Abort()
			return
		}

		c.Next()
	}
}

// ProxyAuth
type proxyGroupLookup interface {
	GetGroupByName(string) (*models.Group, error)
}

const (
	// ProxyKeyHeader is the dedicated control-plane credential carrier for
	// proxy requests. It avoids collisions with end-to-end upstream auth.
	ProxyKeyHeader = "X-Gpt-Load-Key"

	proxyAuthHeadersContextField = "proxy_auth_consumed_headers"
)

type proxyCredentialCandidate struct {
	header string
	value  string
}

func ProxyAuth(gm proxyGroupLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Reject requests with no possible proxy credential before touching the
		// group store. A query key remains only a legacy candidate; whether it
		// may be consumed is decided after the group type is known.
		if len(proxyHeaderCredentialCandidates(c)) == 0 && c.Query("key") == "" {
			response.Error(c, app_errors.ErrUnauthorized)
			c.Abort()
			return
		}

		group, err := gm.GetGroupByName(c.Param("group_name"))
		if err != nil {
			// The caller is not authenticated until a group-specific proxy key is
			// verified. Keep lookup failures indistinguishable from an invalid key
			// so group names cannot be enumerated through 500/401 differences.
			response.Error(c, app_errors.ErrUnauthorized)
			c.Abort()
			return
		}

		// Generic HTTP is a transparent data plane. Its query string belongs to
		// the upstream application and must never be consumed or re-encoded as
		// console authentication. Legacy channels retain query-key support.
		valid := false
		consumedHeaders := make([]string, 0, 2)
		consumedSet := make(map[string]struct{}, 2)
		for _, candidate := range proxyHeaderCredentialCandidates(c) {
			if !isValidGroupProxyKey(group, candidate.value) {
				continue
			}
			valid = true
			canonical := candidate.header
			if _, exists := consumedSet[canonical]; !exists {
				consumedSet[canonical] = struct{}{}
				consumedHeaders = append(consumedHeaders, canonical)
			}
		}

		// Query authentication remains a legacy-only compatibility surface.
		// Remove it only when it actually authenticates this request.
		if group.ChannelType != channel.GenericHTTPChannelType {
			if queryKey := c.Query("key"); queryKey != "" && isValidGroupProxyKey(group, queryKey) {
				valid = true
				query := c.Request.URL.Query()
				query.Del("key")
				c.Request.URL.RawQuery = query.Encode()
			}
		}

		if valid {
			c.Set(proxyAuthHeadersContextField, consumedHeaders)
			c.Next()
			return
		}

		response.Error(c, app_errors.ErrUnauthorized)
		c.Abort()
	}
}

// ConsumedProxyCredentialHeaders returns only header names whose values
// authenticated the current proxy request. Values are intentionally never
// retained in context.
func ConsumedProxyCredentialHeaders(c *gin.Context) []string {
	if c == nil {
		return nil
	}
	value, exists := c.Get(proxyAuthHeadersContextField)
	if !exists {
		return nil
	}
	headers, ok := value.([]string)
	if !ok {
		return nil
	}
	return append([]string(nil), headers...)
}

func isValidGroupProxyKey(group *models.Group, key string) bool {
	if group == nil || key == "" {
		return false
	}
	_, existsInEffective := group.EffectiveConfig.ProxyKeysMap[key]
	_, existsInGroup := group.ProxyKeysMap[key]
	return existsInEffective || existsInGroup
}

func proxyHeaderCredentialCandidates(c *gin.Context) []proxyCredentialCandidate {
	if c == nil || c.Request == nil {
		return nil
	}
	candidates := make([]proxyCredentialCandidate, 0, 4)
	for _, value := range c.Request.Header.Values("Authorization") {
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(value, bearerPrefix) && len(value) > len(bearerPrefix) {
			candidates = append(candidates, proxyCredentialCandidate{header: "Authorization", value: value[len(bearerPrefix):]})
		}
	}
	for _, name := range []string{ProxyKeyHeader, "X-Api-Key", "X-Goog-Api-Key"} {
		for _, value := range c.Request.Header.Values(name) {
			if value != "" {
				candidates = append(candidates, proxyCredentialCandidate{header: name, value: value})
			}
		}
	}
	return candidates
}

// ProxyRouteDispatcher dispatches special routes before proxy authentication
func ProxyRouteDispatcher(serverHandler interface{ GetIntegrationInfo(*gin.Context) }, gm proxyGroupLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Param("path") != "/api/integration/info" {
			c.Next()
			return
		}

		// This endpoint predates generic HTTP and is part of the legacy LLM
		// integration surface. A generic upstream may legitimately expose the
		// same path, so it must continue through normal proxy authentication.
		group, err := gm.GetGroupByName(c.Param("group_name"))
		if err == nil && group.ChannelType != channel.GenericHTTPChannelType {
			serverHandler.GetIntegrationInfo(c)
			c.Abort()
			return
		}

		c.Next()
	}
}

// Recovery creates a recovery middleware with custom error handling
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logrus.Errorf("Panic recovered: %s", utils.SanitizeText(fmt.Sprint(recovered)))
		response.Error(c, app_errors.ErrInternalServer)
		c.Abort()
	})
}

// RateLimiter creates a simple rate limiting middleware
func RateLimiter(config types.PerformanceConfig) gin.HandlerFunc {
	// Simple semaphore-based rate limiting
	semaphore := make(chan struct{}, config.MaxConcurrentRequests)

	return func(c *gin.Context) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
			c.Next()
		default:
			response.Error(c, app_errors.NewAPIError(app_errors.ErrInternalServer, "Too many concurrent requests"))
			c.Abort()
		}
	}
}

// ErrorHandler creates an error handling middleware
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Handle any errors that occurred during request processing
		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err

			// Check if it's our custom error type
			if apiErr, ok := err.(*app_errors.APIError); ok {
				response.Error(c, apiErr)
				return
			}

			// Handle other errors
			logrus.Errorf("Unhandled error: %s", utils.SanitizeText(err.Error()))
			response.Error(c, app_errors.ErrInternalServer)
		}
	}
}

// isMonitoringEndpoint checks if the path is a monitoring endpoint
func isMonitoringEndpoint(path string) bool {
	monitoringPaths := []string{"/health"}
	for _, monitoringPath := range monitoringPaths {
		if path == monitoringPath {
			return true
		}
	}
	return false
}

// extractAuthKey extracts a auth key.
func extractAuthKey(c *gin.Context, allowQuery bool) string {
	// Query authentication is a legacy compatibility surface. Generic HTTP
	// callers need `key` to remain ordinary application data.
	if allowQuery {
		if key := c.Query("key"); key != "" {
			query := c.Request.URL.Query()
			query.Del("key")
			c.Request.URL.RawQuery = query.Encode()
			return key
		}
	}

	return extractHeaderAuthKey(c)
}

func extractHeaderAuthKey(c *gin.Context) string {
	// Bearer token
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(authHeader, bearerPrefix) {
			return authHeader[len(bearerPrefix):]
		}
	}

	// X-Api-Key
	if key := c.GetHeader("X-Api-Key"); key != "" {
		return key
	}

	// X-Goog-Api-Key
	if key := c.GetHeader("X-Goog-Api-Key"); key != "" {
		return key
	}

	return ""
}

// StaticCache creates a middleware for caching static resources
func StaticCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if isStaticResource(path) {
			c.Header("Cache-Control", "public, max-age=2592000, immutable")
			c.Header("Expires", time.Now().AddDate(1, 0, 0).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))
		}

		c.Next()
	}
}

// isStaticResource 判断是否为静态资源
func isStaticResource(path string) bool {
	staticPrefixes := []string{"/assets/"}
	staticSuffixes := []string{
		".js", ".css", ".ico", ".png", ".jpg", ".jpeg",
		".gif", ".svg", ".woff", ".woff2", ".ttf", ".eot",
		".webp", ".avif", ".map",
	}

	// 检查路径前缀
	for _, prefix := range staticPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	// 检查文件扩展名
	for _, suffix := range staticSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}

// SecurityHeaders creates a middleware to add security-related headers
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Next()
	}
}
