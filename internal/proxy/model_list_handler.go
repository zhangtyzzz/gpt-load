package proxy

import (
	"fmt"
	"gpt-load/internal/channel"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const maxModelListResponseBytes = int64(16 << 20)

// shouldInterceptModelList checks if this is a model list request that should be intercepted
func shouldInterceptModelList(path string, method string) bool {
	if method != "GET" {
		return false
	}

	// Check various model list endpoints
	return strings.HasSuffix(path, "/v1/models") ||
		strings.HasSuffix(path, "/v1beta/models") ||
		strings.Contains(path, "/v1beta/openai/v1/models")
}

// handleModelListResponse processes the model list response and applies filtering based on redirect rules
func (ps *ProxyServer) handleModelListResponse(c *gin.Context, resp *http.Response, group *models.Group, channelHandler channel.ChannelProxy, apiKey *models.APIKey) (int, error) {
	// Model-list responses are transformed in memory, so both compressed input
	// and decoded output must be bounded and fully decoded before parsing.
	bodyBytes, err := readResponseBodyBounded(resp, maxModelListResponseBytes, maxModelListResponseBytes)
	if err != nil {
		safeError := utils.SanitizeText(err.Error())
		if apiKey != nil {
			safeError = utils.SanitizeKnownSecrets(safeError, apiKey.KeyValue)
		}
		logrus.WithField("error", safeError).Error("Failed to read bounded model list response body")
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to read upstream model list response"})
		return http.StatusBadGateway, fmt.Errorf("model_list_upstream_read_failed: %s", safeError)
	}

	// Transform model list (returns map[string]any directly, no marshaling)
	response, err := channelHandler.TransformModelList(c.Request, bodyBytes, group)
	if err != nil {
		safeError := utils.SanitizeText(err.Error())
		if apiKey != nil {
			safeError = utils.SanitizeKnownSecrets(safeError, apiKey.KeyValue)
		}
		logrus.WithField("error", safeError).Error("Failed to transform model list")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process response"})
		return http.StatusInternalServerError, fmt.Errorf("model_list_transform_failed: %s", safeError)
	}

	c.JSON(http.StatusOK, response)
	return http.StatusOK, nil
}
