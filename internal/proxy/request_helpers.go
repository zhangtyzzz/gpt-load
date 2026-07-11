package proxy

import (
	"encoding/json"
	"gpt-load/internal/channel"
	"gpt-load/internal/errorpolicy"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const maxRetryAfterCooldown = 24 * time.Hour

func shouldRetryClassifiedAttempt(
	classification channel.ResponseClassification,
	decision errorpolicy.Decision,
	retryCount int,
	maxRetries int,
) bool {
	return classification.AllowRetry &&
		decision.OnRequest == errorpolicy.RequestActionRetryOtherKey &&
		retryCount < maxRetries
}

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
	safeError := utils.SanitizeText(err.Error())
	if app_errors.IsIgnorableError(err) {
		logrus.Debugf("Ignorable upstream error in %s: %s", context, safeError)
	} else {
		logrus.Errorf("Upstream error in %s: %s", context, safeError)
	}
}

// readUpstreamErrorBodyBounded limits both the encoded representation and all
// decoded layers. A decoding failure returns no body bytes.
func readUpstreamErrorBodyBounded(resp *http.Response, limit int64) ([]byte, error) {
	return readResponseBodyBounded(resp, limit, limit)
}

func readResponseBodyBounded(resp *http.Response, encodedLimit, decodedLimit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	contentEncodingValues := resp.Header.Values("Content-Encoding")
	contentEncoding := strings.Join(contentEncodingValues, ",")
	body, err := utils.ReadCompressedBodyBounded(resp.Body, contentEncoding, encodedLimit, decodedLimit)
	if len(contentEncodingValues) > 0 {
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		removeRepresentationIntegrityHeaders(resp.Header)
		resp.ContentLength = -1
	}
	if err != nil {
		return nil, err
	}
	return body, nil
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
