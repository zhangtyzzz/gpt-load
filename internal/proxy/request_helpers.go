package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"gpt-load/internal/errorpolicy"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const maxRetryAfterCooldown = 24 * time.Hour

func (ps *ProxyServer) applyParamOverrides(bodyBytes []byte, group *models.Group) ([]byte, error) {
	if len(group.ParamOverrides) == 0 || len(bodyBytes) == 0 {
		return bodyBytes, nil
	}

	var requestData map[string]any
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		logrus.Warnf("failed to unmarshal request body for param override, passing through: %v", err)
		return bodyBytes, nil
	}

	for key, value := range group.ParamOverrides {
		requestData[key] = value
	}

	return json.Marshal(requestData)
}

// logUpstreamError provides a centralized way to log errors from upstream interactions.
func logUpstreamError(context string, err error) {
	if err == nil {
		return
	}
	if app_errors.IsIgnorableError(err) {
		logrus.Debugf("Ignorable upstream error in %s: %v", context, err)
	} else {
		logrus.Errorf("Upstream error in %s: %v", context, err)
	}
}

// handleGzipCompression checks for gzip encoding and decompresses the body if necessary.
func handleGzipCompression(resp *http.Response, bodyBytes []byte) []byte {
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, gzipErr := gzip.NewReader(bytes.NewReader(bodyBytes))
		if gzipErr != nil {
			logrus.Warnf("Failed to create gzip reader for error body: %v", gzipErr)
			return bodyBytes
		}
		defer reader.Close()

		decompressedBody, readAllErr := io.ReadAll(reader)
		if readAllErr != nil {
			logrus.Warnf("Failed to decompress gzip error body: %v", readAllErr)
			return bodyBytes
		}
		return decompressedBody
	}
	return bodyBytes
}

func getAttemptedKeyIDs(c interface {
	Get(string) (any, bool)
}) map[uint]struct{} {
	if existing, ok := c.Get("attempted_key_ids"); ok {
		if attempted, ok := existing.(map[uint]struct{}); ok {
			return attempted
		}
	}
	return make(map[uint]struct{})
}

func cooldownDuration(decision errorpolicy.Decision, resp *http.Response) time.Duration {
	if decision.Health != errorpolicy.HealthActionCooldown {
		return 0
	}

	if resp != nil {
		if retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After")); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
				return minDuration(time.Duration(seconds)*time.Second, maxRetryAfterCooldown)
			}
			if retryAt, err := http.ParseTime(retryAfter); err == nil {
				if duration := time.Until(retryAt); duration > 0 {
					return minDuration(duration, maxRetryAfterCooldown)
				}
			}
		}
	}

	seconds := decision.Params.CooldownSeconds
	if seconds <= 0 {
		seconds = errorpolicy.DefaultCooldownSeconds
	}
	return time.Duration(seconds) * time.Second
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
