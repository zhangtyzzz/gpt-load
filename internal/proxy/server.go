// Package proxy provides high-performance OpenAI multi-key proxy server
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/errorpolicy"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/keypool"
	"gpt-load/internal/middleware"
	"gpt-load/internal/models"
	"gpt-load/internal/response"
	"gpt-load/internal/services"
	"gpt-load/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ProxyServer represents the proxy server
type ProxyServer struct {
	keyProvider       *keypool.KeyProvider
	groupManager      *services.GroupManager
	subGroupManager   *services.SubGroupManager
	settingsManager   *config.SystemSettingsManager
	channelFactory    *channel.Factory
	requestLogService *services.RequestLogService
	encryptionSvc     encryption.Service
	affinityManager   *keypool.AffinityManager
}

type preparedProxyAttempt struct {
	group        *models.Group
	handler      channel.ChannelProxy
	apiKey       *models.APIKey
	body         []byte
	isStream     bool
	affinityHash string
	affinityTTL  time.Duration
}

var errRequestBodyTooLarge = errors.New("request body exceeds channel limit")

// NewProxyServer creates a new proxy server
func NewProxyServer(
	keyProvider *keypool.KeyProvider,
	groupManager *services.GroupManager,
	subGroupManager *services.SubGroupManager,
	settingsManager *config.SystemSettingsManager,
	channelFactory *channel.Factory,
	requestLogService *services.RequestLogService,
	encryptionSvc encryption.Service,
	affinityManager *keypool.AffinityManager,
) (*ProxyServer, error) {
	return &ProxyServer{
		keyProvider:       keyProvider,
		groupManager:      groupManager,
		subGroupManager:   subGroupManager,
		settingsManager:   settingsManager,
		channelFactory:    channelFactory,
		requestLogService: requestLogService,
		encryptionSvc:     encryptionSvc,
		affinityManager:   affinityManager,
	}, nil
}

// HandleProxy is the main entry point for proxy requests, refactored based on the stable .bak logic.
func (ps *ProxyServer) HandleProxy(c *gin.Context) {
	startTime := time.Now()
	groupName := c.Param("group_name")

	originalGroup, err := ps.groupManager.GetGroupByName(groupName)
	if err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	// Authentication middleware has already consumed any proxy credential.
	// Strip fixed and Connection-declared hop-by-hop fields before proxy
	// processing inspects or forwards request headers.
	removeHopByHopHeaders(c.Request.Header)

	group, channelHandler, err := ps.resolveProxyTarget(originalGroup)
	if err != nil {
		if errors.Is(err, app_errors.ErrNoActiveKeys) {
			ps.logRequest(c, originalGroup, originalGroup, nil, startTime, http.StatusServiceUnavailable, errors.New("no available sub-groups"), false, "", nil, nil, models.RequestTypeFinal)
			response.Error(c, app_errors.NewAPIError(app_errors.ErrNoKeysAvailable, "No available sub-groups"))
			return
		}
		safeErr := utils.SanitizeText(err.Error())
		ps.logRequest(c, originalGroup, originalGroup, nil, startTime, http.StatusInternalServerError, errors.New("resolve_proxy_target_failed: "+safeErr), false, "", nil, nil, models.RequestTypeFinal)
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInternalServer, fmt.Sprintf("Failed to get channel for group '%s'", groupName)))
		return
	}
	readLimit, err := ps.requestBodyReadLimit(originalGroup, channelHandler)
	if err != nil {
		safeErr := utils.SanitizeText(err.Error())
		ps.logRequest(c, originalGroup, group, nil, startTime, http.StatusInternalServerError, errors.New("resolve_request_body_limit_failed: "+safeErr), false, "", channelHandler, nil, models.RequestTypeFinal)
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInternalServer, "Failed to resolve request body limit"))
		return
	}
	if readLimit > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, readLimit)
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			ps.logRequest(c, originalGroup, group, nil, startTime, http.StatusRequestEntityTooLarge, errRequestBodyTooLarge, false, "", channelHandler, nil, models.RequestTypeFinal)
			response.Error(c, app_errors.NewAPIErrorWithUpstream(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Request body exceeds channel limit"))
			return
		}
		safeErr := utils.SanitizeText(err.Error())
		logrus.WithField("error", safeErr).Error("Failed to read request body")
		ps.logRequest(c, originalGroup, group, nil, startTime, http.StatusBadRequest, errors.New("read_request_body_failed: "+safeErr), false, "", channelHandler, nil, models.RequestTypeFinal)
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, "Failed to read request body"))
		return
	}
	if closeErr := c.Request.Body.Close(); closeErr != nil {
		logrus.WithField("error", utils.SanitizeText(closeErr.Error())).Warn("Failed to close request body")
	}

	ps.executeRequestWithRetry(c, channelHandler, originalGroup, group, bodyBytes, startTime, 0)
}

func (ps *ProxyServer) requestBodyReadLimit(originalGroup *models.Group, preferredHandler channel.ChannelProxy) (int64, error) {
	limitFor := func(handler channel.ChannelProxy) int64 {
		if limiter, ok := handler.(channel.RequestLimitProvider); ok {
			return limiter.MaxRequestBodyBytes()
		}
		return 0
	}
	maximum := limitFor(preferredHandler)
	if originalGroup.GroupType != "aggregate" {
		return maximum, nil
	}
	for _, relation := range originalGroup.SubGroups {
		child, err := ps.groupManager.GetGroupByID(relation.SubGroupID)
		if err != nil {
			return 0, err
		}
		handler, err := ps.channelFactory.GetChannel(child)
		if err != nil {
			return 0, err
		}
		limit := limitFor(handler)
		if limit == 0 {
			// An unbounded candidate requires reading the original request once;
			// the final selected child is still checked during preparation.
			return 0, nil
		}
		if limit > maximum {
			maximum = limit
		}
	}
	return maximum, nil
}

func (ps *ProxyServer) resolveProxyTarget(originalGroup *models.Group) (*models.Group, channel.ChannelProxy, error) {
	if originalGroup.GroupType == "aggregate" && originalGroup.ChannelType == channel.GenericHTTPChannelType {
		if err := ps.validateGenericAggregateConfig(originalGroup); err != nil {
			return nil, nil, err
		}
	}
	group, err := ps.selectOrdinaryProxyGroup(originalGroup)
	if err != nil {
		return nil, nil, err
	}
	handler, err := ps.channelFactory.GetChannel(group)
	return group, handler, err
}

func (ps *ProxyServer) validateGenericAggregateConfig(originalGroup *models.Group) error {
	var reference []byte
	for _, relation := range originalGroup.SubGroups {
		child, err := ps.groupManager.GetGroupByID(relation.SubGroupID)
		if err != nil {
			return err
		}
		normalized, _, err := channel.NormalizeGenericHTTPConfig(child.ChannelConfig)
		if err != nil {
			return err
		}
		if reference == nil {
			reference = normalized
			continue
		}
		if !bytes.Equal(reference, normalized) {
			return fmt.Errorf("generic-http aggregate sub-groups have inconsistent channel_config")
		}
	}
	return nil
}

func (ps *ProxyServer) selectOrdinaryProxyGroup(originalGroup *models.Group) (*models.Group, error) {
	subGroupName, err := ps.subGroupManager.SelectSubGroup(originalGroup)
	if err != nil {
		logrus.WithFields(logrus.Fields{"aggregate_group": originalGroup.Name, "error": err}).Error("Failed to select sub-group from aggregate")
		return nil, fmt.Errorf("%w: no available sub-groups", app_errors.ErrNoActiveKeys)
	}
	if subGroupName == "" {
		return originalGroup, nil
	}
	return ps.groupManager.GetGroupByName(subGroupName)
}

func (ps *ProxyServer) prepareRequestAttempt(
	c *gin.Context,
	originalGroup, preferredGroup *models.Group,
	preferredHandler channel.ChannelProxy,
	rawBody []byte,
	retryCount int,
) (*preparedProxyAttempt, error) {
	if originalGroup.GroupType != "aggregate" {
		return ps.prepareGroupAttempt(c, preferredGroup, preferredHandler, rawBody, retryCount)
	}
	return ps.prepareAggregateAttempt(c, originalGroup, preferredGroup, preferredHandler, rawBody, retryCount)
}

// prepareAggregateAttempt chooses and prepares one complete child attempt. Each
// candidate derives body overrides, stream mode and affinity from the original
// request bytes before selecting its key; no state from a rejected child is
// reused by a sibling.
func (ps *ProxyServer) prepareAggregateAttempt(
	c *gin.Context,
	originalGroup, preferredGroup *models.Group,
	preferredHandler channel.ChannelProxy,
	rawBody []byte,
	retryCount int,
) (*preparedProxyAttempt, error) {
	type candidate struct {
		group   *models.Group
		handler channel.ChannelProxy
	}
	candidates := make([]candidate, 0, len(originalGroup.SubGroups)+1)
	seen := make(map[uint]struct{}, len(originalGroup.SubGroups)+1)
	appendCandidate := func(group *models.Group, handler channel.ChannelProxy) {
		if group == nil {
			return
		}
		if _, exists := seen[group.ID]; exists {
			return
		}
		seen[group.ID] = struct{}{}
		candidates = append(candidates, candidate{group: group, handler: handler})
	}
	appendCandidate(preferredGroup, preferredHandler)
	for _, relation := range originalGroup.SubGroups {
		group, err := ps.groupManager.GetGroupByID(relation.SubGroupID)
		if err != nil {
			return nil, err
		}
		appendCandidate(group, nil)
	}

	var noActiveKeys bool
	var bodyTooLarge bool
	for _, candidate := range candidates {
		handler := candidate.handler
		if handler == nil {
			var err error
			handler, err = ps.channelFactory.GetChannel(candidate.group)
			if err != nil {
				return nil, err
			}
		}
		attempt, err := ps.prepareGroupAttempt(c, candidate.group, handler, rawBody, retryCount)
		if err == nil {
			return attempt, nil
		}
		if errors.Is(err, errRequestBodyTooLarge) {
			bodyTooLarge = true
			continue
		}
		if errors.Is(err, app_errors.ErrNoActiveKeys) {
			noActiveKeys = true
			continue
		}
		return nil, err
	}
	if bodyTooLarge {
		return nil, errRequestBodyTooLarge
	}
	if noActiveKeys {
		return nil, app_errors.ErrNoActiveKeys
	}
	return nil, app_errors.ErrNoActiveKeys
}

func (ps *ProxyServer) prepareGroupAttempt(
	c *gin.Context,
	group *models.Group,
	handler channel.ChannelProxy,
	rawBody []byte,
	retryCount int,
) (*preparedProxyAttempt, error) {
	if group == nil || handler == nil {
		return nil, app_errors.ErrNoActiveKeys
	}
	body, err := ps.applyParamOverrides(rawBody, group)
	if err != nil {
		return nil, fmt.Errorf("apply parameter overrides: %w", err)
	}
	isStream := handler.IsStreamRequest(c, body)

	var affinityHash string
	var affinityTTL time.Duration
	if ps.affinityManager != nil && len(group.AffinityRuleList) > 0 {
		modelName := handler.ExtractModel(c, body)
		affinityResult := ps.affinityManager.ExtractValue(c, body, modelName, group.AffinityRuleList)
		if affinityResult.Hash != "" {
			affinityHash = affinityResult.Hash
			affinityTTL = keypool.GetEffectiveTTL(affinityResult.MatchedRule, group.EffectiveConfig.KeyAffinityDefaultTTL)
		}
	}

	var apiKey *models.APIKey
	if retryCount == 0 && affinityHash != "" {
		apiKey, err = ps.keyProvider.SelectKeyWithAffinity(group.ID, affinityHash)
	} else if retryCount > 0 {
		apiKey, err = ps.keyProvider.SelectKeyExcludeSet(group.ID, getAttemptedKeyIDs(c))
	} else {
		apiKey, err = ps.keyProvider.SelectKey(group.ID)
	}
	if err != nil {
		return nil, err
	}
	if limiter, ok := handler.(channel.RequestLimitProvider); ok {
		limit := limiter.MaxRequestBodyBytes()
		if limit > 0 && int64(len(rawBody)) > limit {
			// Key selection happens first so aggregate error priority is precise:
			// a child with no key contributes ErrNoActiveKeys, while an actually
			// serviceable child that rejects this body contributes 413.
			return nil, errRequestBodyTooLarge
		}
	}

	return &preparedProxyAttempt{
		group:        group,
		handler:      handler,
		apiKey:       apiKey,
		body:         body,
		isStream:     isStream,
		affinityHash: affinityHash,
		affinityTTL:  affinityTTL,
	}, nil
}

// executeRequestWithRetry is the core recursive function for handling requests and retries.
func (ps *ProxyServer) executeRequestWithRetry(
	c *gin.Context,
	preferredHandler channel.ChannelProxy,
	originalGroup *models.Group,
	preferredGroup *models.Group,
	rawBodyBytes []byte,
	startTime time.Time,
	retryCount int,
) {
	attempt, err := ps.prepareRequestAttempt(c, originalGroup, preferredGroup, preferredHandler, rawBodyBytes, retryCount)
	if err != nil {
		logGroup := preferredGroup
		if logGroup == nil {
			logGroup = originalGroup
		}
		safeErr := errors.New("request attempt preparation failed")
		statusCode := http.StatusInternalServerError
		apiErr := app_errors.NewAPIError(app_errors.ErrInternalServer, "Failed to prepare upstream request")
		switch {
		case errors.Is(err, errRequestBodyTooLarge):
			safeErr = errRequestBodyTooLarge
			statusCode = http.StatusRequestEntityTooLarge
			apiErr = app_errors.NewAPIErrorWithUpstream(statusCode, "REQUEST_TOO_LARGE", "Request body exceeds channel limit")
		case errors.Is(err, app_errors.ErrNoActiveKeys):
			safeErr = errors.New("no active keys")
			statusCode = http.StatusServiceUnavailable
			apiErr = app_errors.NewAPIError(app_errors.ErrNoKeysAvailable, "No available keys")
		}
		logrus.WithFields(logrus.Fields{
			"group":   logGroup.Name,
			"attempt": retryCount + 1,
			"error":   utils.SanitizeText(err.Error()),
		}).Error("Failed to prepare proxy attempt")
		response.Error(c, apiErr)
		ps.logRequest(c, originalGroup, logGroup, nil, startTime, statusCode, safeErr, false, "", preferredHandler, rawBodyBytes, models.RequestTypeFinal)
		return
	}

	group := attempt.group
	channelHandler := attempt.handler
	apiKey := attempt.apiKey
	bodyBytes := attempt.body
	isStream := attempt.isStream
	affinityHash := attempt.affinityHash
	affinityTTL := attempt.affinityTTL
	cfg := group.EffectiveConfig

	upstreamURL, err := channelHandler.BuildUpstreamURL(c.Request.URL, originalGroup.Name)
	if err != nil {
		safeErr := utils.SanitizeKnownSecrets(err.Error(), apiKey.KeyValue)
		ps.logRequest(c, originalGroup, group, apiKey, startTime, http.StatusInternalServerError, errors.New("build_upstream_url_failed: "+safeErr), isStream, "", channelHandler, bodyBytes, models.RequestTypeFinal)
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInternalServer, "Failed to build upstream URL"))
		return
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if isStream {
		ctx, cancel = context.WithCancel(c.Request.Context())
	} else {
		timeout := time.Duration(cfg.RequestTimeout) * time.Second
		ctx, cancel = context.WithTimeout(c.Request.Context(), timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, c.Request.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		safeErr := utils.SanitizeKnownSecrets(err.Error(), apiKey.KeyValue)
		logrus.WithField("error", safeErr).Error("Failed to create upstream request")
		ps.logRequest(c, originalGroup, group, apiKey, startTime, http.StatusInternalServerError, errors.New("create_upstream_request_failed: "+safeErr), isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
		response.Error(c, app_errors.ErrInternalServer)
		return
	}
	req.ContentLength = int64(len(bodyBytes))

	req.Header = c.Request.Header.Clone()

	removeProxyControlCredentials(c, group, req.Header)

	// Apply model redirection
	finalBodyBytes, err := channelHandler.ApplyModelRedirect(req, bodyBytes, group)
	if err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, err.Error()))
		ps.logRequest(c, originalGroup, group, apiKey, startTime, http.StatusBadRequest, err, isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
		return
	}

	// Update request body if it was modified by redirection
	if !bytes.Equal(finalBodyBytes, bodyBytes) {
		req.Body = io.NopCloser(bytes.NewReader(finalBodyBytes))
		req.ContentLength = int64(len(finalBodyBytes))
	}

	channelHandler.ModifyRequest(req, apiKey, group)

	// Apply custom header rules
	if len(group.HeaderRuleList) > 0 {
		headerCtx := utils.NewHeaderVariableContextFromGin(c, group, apiKey)
		utils.ApplyHeaderRules(req, group.HeaderRuleList, headerCtx)
	}
	if finalizer, ok := channelHandler.(channel.CredentialFinalizer); ok {
		finalizer.FinalizeCredentials(req, apiKey, group)
	}
	// The dedicated proxy carrier is never an end-to-end header. Enforce this
	// after both user header rules and channel finalizers so neither can
	// accidentally or maliciously reintroduce it.
	req.Header.Del(middleware.ProxyKeyHeader)
	removeHopByHopHeaders(req.Header)

	var client *http.Client
	if isStream {
		client = channelHandler.GetStreamClient()
		if group.ChannelType != channel.GenericHTTPChannelType {
			// Preserve the legacy request contract; Generic HTTP must not invent an
			// end-to-end upstream header that the caller did not provide.
			req.Header.Set("X-Accel-Buffering", "no")
		}
	} else {
		client = channelHandler.GetHTTPClient()
	}

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
		// Remove both fixed and Connection-declared hop-by-hop response fields
		// before forwarding anything downstream.
		removeHopByHopHeaders(resp.Header)
	}

	classification := channel.ResponseClassification{}
	classifier, hasClassifier := channelHandler.(channel.ResponseClassifier)
	handleAsFailure := err != nil || (resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300))
	if hasClassifier {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		classification = classifier.ClassifyResponse(c.Request.Method, statusCode, err)
		handleAsFailure = classification.HandleAsFailure
	}

	if handleAsFailure {
		keyIdentifier := utils.KeyFingerprint(ps.encryptionSvc.Hash(apiKey.KeyValue))
		if err != nil && app_errors.IsIgnorableError(err) {
			safeErr := utils.SanitizeKnownSecrets(err.Error(), apiKey.KeyValue)
			logrus.Debugf("Client-side ignorable error for key %s, aborting retries: %s", keyIdentifier, safeErr)
			ps.logRequest(c, originalGroup, group, apiKey, startTime, 499, errors.New(safeErr), isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
			return
		}

		var statusCode int
		var errorMessage string
		var parsedError string
		var bufferedErrorBody []byte

		if err != nil {
			statusCode = transportFailureStatus(hasClassifier)
			errorMessage = utils.SanitizeKnownSecrets(err.Error(), apiKey.KeyValue)
			parsedError = errorMessage
			logrus.Debugf("Request failed (attempt %d/%d) for key %s: %s", retryCount+1, cfg.MaxRetries, keyIdentifier, errorMessage)
		} else {
			statusCode = resp.StatusCode
			errorBodyLimit := int64(64 << 10)
			if limiter, ok := channelHandler.(channel.ErrorBodyLimitProvider); ok {
				errorBodyLimit = limiter.MaxErrorBodyBytes()
			}
			errorBody, readErr := readUpstreamErrorBodyBounded(resp, errorBodyLimit)
			if readErr != nil {
				safeReadErr := utils.SanitizeKnownSecrets(readErr.Error(), apiKey.KeyValue)
				if requestContextError(c) != nil {
					result := responseBodyResult{outcome: responseBodyClientCancelled, err: requestContextError(c)}
					logStatus, finalErr := responseBodyLogResult(result, apiKey.KeyValue)
					ps.logRequest(c, originalGroup, group, apiKey, startTime, logStatus, finalErr, isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
					return
				}
				logrus.Errorf("Failed to read error body: %s", safeReadErr)
				errorBody = []byte("Failed to read error body")
			}

			errorMessage, parsedError = sanitizeUpstreamError(errorBody, apiKey.KeyValue)
			bufferedErrorBody = []byte(errorMessage)
			if hasClassifier {
				resp.Header.Del("Content-Length")
				removeRepresentationIntegrityHeaders(resp.Header)
				resp.ContentLength = -1
			}
			logrus.Debugf("Request failed with status %d (attempt %d/%d) for key %s. Parsed Error: %s", statusCode, retryCount+1, cfg.MaxRetries, keyIdentifier, parsedError)
		}

		policy := group.ErrorPolicy
		if len(policy.Rules) == 0 && policy.Default == nil {
			policy = errorpolicy.DefaultPolicy()
		}
		decision := policy.Decide(statusCode)
		if !hasClassifier || classification.UseErrorPolicy {
			cooldown := cooldownDuration(decision, resp)
			if healthErr := ps.keyProvider.ApplyHealthAction(apiKey, group, decision.Health, cooldown); healthErr != nil {
				logrus.WithFields(logrus.Fields{
					"keyID":  apiKey.ID,
					"health": decision.Health,
					"error":  healthErr,
				}).Error("Failed to apply error policy health action")
			}
		}

		shouldRetry := false
		if hasClassifier {
			shouldRetry = shouldRetryClassifiedAttempt(classification, decision, retryCount, cfg.MaxRetries)
		} else {
			shouldRetry = decision.OnRequest == errorpolicy.RequestActionRetryOtherKey && retryCount < cfg.MaxRetries
		}
		if shouldRetry && !hasClassifier {
			if guard, ok := channelHandler.(channel.RetryGuard); ok && !guard.AllowRetry(c.Request.Method, statusCode, err) {
				shouldRetry = false
			}
		}
		if !shouldRetry {
			if hasClassifier && resp != nil {
				sanitizeUpstreamResponseHeaders(resp.Header, apiKey.KeyValue)
				copyResponseHeaders(c.Writer.Header(), resp.Header)
				c.Status(statusCode)
				deliveryResult := copyResponseBody(c, bytes.NewReader(bufferedErrorBody), nil)
				if !deliveryResult.completed() {
					logStatus, deliveryErr := responseBodyLogResult(deliveryResult, apiKey.KeyValue)
					ps.logRequest(c, originalGroup, group, apiKey, startTime, logStatus, deliveryErr, isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
					return
				}
				ps.logRequest(c, originalGroup, group, apiKey, startTime, statusCode, errors.New(parsedError), isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
				return
			}
			ps.logRequest(c, originalGroup, group, apiKey, startTime, statusCode, errors.New(parsedError), isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
			var errorJSON map[string]any
			if err := json.Unmarshal([]byte(errorMessage), &errorJSON); err == nil {
				c.JSON(statusCode, errorJSON)
			} else {
				response.Error(c, app_errors.NewAPIErrorWithUpstream(statusCode, "UPSTREAM_ERROR", errorMessage))
			}
			return
		}

		attemptedKeyIDs := getAttemptedKeyIDs(c)
		attemptedKeyIDs[apiKey.ID] = struct{}{}
		c.Set("attempted_key_ids", attemptedKeyIDs)

		retryGroup := group
		retryHandler := channelHandler
		if originalGroup.GroupType == "aggregate" {
			selectedGroup, selectErr := ps.selectOrdinaryProxyGroup(originalGroup)
			if selectErr == nil {
				retryGroup = selectedGroup
				retryHandler, selectErr = ps.channelFactory.GetChannel(selectedGroup)
			} else if errors.Is(selectErr, app_errors.ErrNoActiveKeys) {
				// The aggregate selector only sees its derived active list. Let the
				// attempt preparer inspect every child against request-local key
				// exclusions before declaring the aggregate unavailable.
				retryGroup = nil
				retryHandler = nil
				selectErr = nil
			}
			if selectErr != nil {
				safeSelectErr := utils.SanitizeKnownSecrets(selectErr.Error(), apiKey.KeyValue)
				logrus.WithFields(logrus.Fields{
					"aggregate_group": originalGroup.Name,
					"attempt":         retryCount + 2,
					"error":           safeSelectErr,
				}).Error("Failed to resolve aggregate retry target")
				ps.logRequest(c, originalGroup, group, apiKey, startTime, http.StatusInternalServerError, errors.New("aggregate_retry_target_failed: "+safeSelectErr), isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
				response.Error(c, app_errors.NewAPIError(app_errors.ErrInternalServer, "Failed to select aggregate retry target"))
				return
			}
		}
		ps.logRequest(c, originalGroup, group, apiKey, startTime, statusCode, errors.New(parsedError), isStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeRetry)
		ps.executeRequestWithRetry(c, retryHandler, originalGroup, retryGroup, rawBodyBytes, startTime, retryCount+1)
		return
	}

	var transparentErrorBody []byte
	if hasClassifier && resp.StatusCode >= 300 {
		limit := int64(64 << 10)
		if limiter, ok := channelHandler.(channel.ErrorBodyLimitProvider); ok {
			limit = limiter.MaxErrorBodyBytes()
		}
		body, readErr := readUpstreamErrorBodyBounded(resp, limit)
		if readErr != nil {
			safeReadErr := utils.SanitizeKnownSecrets(readErr.Error(), apiKey.KeyValue)
			if requestContextError(c) != nil {
				result := responseBodyResult{outcome: responseBodyClientCancelled, err: requestContextError(c)}
				statusCode, finalErr := responseBodyLogResult(result, apiKey.KeyValue)
				ps.logRequest(c, originalGroup, group, apiKey, startTime, statusCode, finalErr, actualStreamForLog(isStream, resp, channelHandler), upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
				return
			}
			finalErr := fmt.Errorf("upstream_error_body_read_failed: %s", safeReadErr)
			ps.logRequest(c, originalGroup, group, apiKey, startTime, http.StatusBadGateway, finalErr, actualStreamForLog(isStream, resp, channelHandler), upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
			response.Error(c, app_errors.NewAPIErrorWithUpstream(http.StatusBadGateway, "UPSTREAM_ERROR_BODY_TOO_LARGE", safeReadErr))
			return
		}
		transparentErrorBody = []byte(utils.SanitizeKnownSecrets(string(body), apiKey.KeyValue))
		resp.Header.Del("Content-Length")
		removeRepresentationIntegrityHeaders(resp.Header)
		resp.ContentLength = -1
	}

	if hasClassifier {
		sanitizeUpstreamResponseHeaders(resp.Header, apiKey.KeyValue)
	}

	actualIsStream := actualStreamForLog(isStream, resp, channelHandler)

	// Check if this is a model list request (needs special handling)
	interceptModelList := shouldInterceptModelList(c.Request.URL.Path, c.Request.Method)
	if policy, ok := channelHandler.(channel.ModelListPolicy); ok && !policy.ShouldTransformModelList(c.Request) {
		interceptModelList = false
	}
	if interceptModelList {
		statusCode, finalErr := ps.handleModelListResponse(c, resp, group, channelHandler, apiKey)
		if finalErr == nil && affinityHash != "" && statusCode >= 200 && statusCode < 300 {
			ps.keyProvider.UpdateAffinityMapping(group.ID, affinityHash, apiKey.ID, affinityTTL)
		}
		ps.logRequest(c, originalGroup, group, apiKey, startTime, statusCode, finalErr, actualIsStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
		return
	} else {
		copyResponseHeaders(c.Writer.Header(), resp.Header)
		c.Status(resp.StatusCode)
		if actualIsStream {
			c.Header("X-Accel-Buffering", "no")
		}

		var deliveryResult responseBodyResult
		if hasClassifier && resp.StatusCode >= 300 {
			deliveryResult = copyResponseBody(c, bytes.NewReader(transparentErrorBody), nil)
		} else if actualIsStream {
			if isEventStreamResponse(resp) {
				deliveryResult = ps.handleStreamingResponse(c, resp)
			} else {
				deliveryResult = ps.handleFlushedResponse(c, resp)
			}
		} else {
			deliveryResult = ps.handleNormalResponse(c, resp)
		}

		statusCode, finalErr := responseBodyLogResult(deliveryResult, apiKey.KeyValue)
		if deliveryResult.completed() {
			statusCode = resp.StatusCode
			if affinityHash != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// Only a fully delivered response can establish or update affinity.
				ps.keyProvider.UpdateAffinityMapping(group.ID, affinityHash, apiKey.ID, affinityTTL)
			}
			logrus.Debugf("Request for group %s completed with status %d on attempt %d with key %s", group.Name, resp.StatusCode, retryCount+1, utils.KeyFingerprint(ps.encryptionSvc.Hash(apiKey.KeyValue)))
		} else {
			safeDeliveryErr := ""
			if deliveryResult.err != nil {
				safeDeliveryErr = utils.SanitizeKnownSecrets(deliveryResult.err.Error(), apiKey.KeyValue)
			}
			logrus.WithFields(logrus.Fields{
				"group":   group.Name,
				"attempt": retryCount + 1,
				"outcome": deliveryResult.outcome,
				"error":   safeDeliveryErr,
			}).Warn("Proxy response did not complete")
		}
		ps.logRequest(c, originalGroup, group, apiKey, startTime, statusCode, finalErr, actualIsStream, upstreamURL, channelHandler, bodyBytes, models.RequestTypeFinal)
		return
	}
}

func actualStreamForLog(requestStream bool, resp *http.Response, handler channel.ChannelProxy) bool {
	// Legacy channels historically classify streaming from the request-side
	// decision. Only an explicit response policy may replace that contract.
	actualIsStream := requestStream
	if streamPolicy, ok := handler.(channel.StreamResponsePolicy); ok {
		actualIsStream = streamPolicy.ShouldFlushResponse(requestStream, resp)
	}
	return actualIsStream
}

func transportFailureStatus(hasClassifier bool) int {
	if hasClassifier {
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

func responseBodyLogResult(result responseBodyResult, selectedKey string) (int, error) {
	if result.completed() {
		return http.StatusOK, nil
	}
	safeDetail := ""
	if result.err != nil {
		safeDetail = utils.SanitizeKnownSecrets(result.err.Error(), selectedKey)
	}
	message := string(result.outcome)
	if safeDetail != "" {
		message += ": " + safeDetail
	}
	switch result.outcome {
	case responseBodyClientCancelled:
		return 499, errors.New(message)
	case responseBodyUpstreamTruncated:
		return http.StatusBadGateway, errors.New(message)
	case responseBodyDownstreamWriteFailed:
		return http.StatusInternalServerError, errors.New(message)
	default:
		return http.StatusInternalServerError, errors.New("unknown_response_outcome")
	}
}

func removeProxyControlCredentials(c *gin.Context, group *models.Group, headers http.Header) {
	if group != nil && group.ChannelType == channel.GenericHTTPChannelType {
		// The dedicated carrier is control-plane-only even when its value did not
		// authenticate the request. A caller may authenticate with a compatible
		// legacy carrier while also sending a stale or malicious dedicated header;
		// that value must never cross the upstream trust boundary.
		headers.Del(middleware.ProxyKeyHeader)
		for _, name := range middleware.ConsumedProxyCredentialHeaders(c) {
			headers.Del(name)
		}
		return
	}

	// Legacy LLM channels historically treat these names as control-plane
	// credentials. Remove all of them before the channel injects its selected
	// upstream key, including the new dedicated carrier.
	for _, name := range []string{"Authorization", middleware.ProxyKeyHeader, "X-Api-Key", "X-Goog-Api-Key"} {
		headers.Del(name)
	}
}

func sanitizeUpstreamError(errorBody []byte, apiKey string) (string, string) {
	safeBody := utils.SanitizeKnownSecrets(string(errorBody), apiKey)
	parsedError := utils.SanitizeKnownSecrets(
		app_errors.ParseUpstreamError([]byte(safeBody)),
		apiKey,
	)
	return safeBody, parsedError
}

func sanitizeUpstreamResponseHeaders(headers http.Header, apiKey string) {
	changed := false
	for name, values := range headers {
		for i, value := range values {
			safe := utils.SanitizeKnownSecrets(value, apiKey)
			if safe != value {
				values[i] = safe
				changed = true
			}
		}
		headers[name] = values
	}
	if changed {
		headers.Del("Content-Length")
	}
}

// logRequest is a helper function to create and record a request log.
func (ps *ProxyServer) logRequest(
	c *gin.Context,
	originalGroup *models.Group,
	group *models.Group,
	apiKey *models.APIKey,
	startTime time.Time,
	statusCode int,
	finalError error,
	isStream bool,
	upstreamAddr string,
	channelHandler channel.ChannelProxy,
	bodyBytes []byte,
	requestType string,
) {
	if ps.requestLogService == nil {
		return
	}

	var requestBodyToLog, userAgent string

	if group.EffectiveConfig.EnableRequestBodyLogging {
		requestBodyToLog = utils.TruncateString(utils.SanitizeText(string(bodyBytes)), 65000)
		userAgent = c.Request.UserAgent()
	}

	duration := time.Since(startTime).Milliseconds()

	logEntry := &models.RequestLog{
		GroupID:      group.ID,
		GroupName:    group.Name,
		IsSuccess:    finalError == nil && statusCode < 400,
		SourceIP:     c.ClientIP(),
		StatusCode:   statusCode,
		RequestPath:  utils.TruncateString(utils.SanitizeURLForLogging(c.Request.URL), 500),
		Duration:     duration,
		UserAgent:    userAgent,
		RequestType:  requestType,
		IsStream:     isStream,
		UpstreamAddr: utils.TruncateString(utils.SanitizeURLStringForLogging(upstreamAddr), 500),
		RequestBody:  requestBodyToLog,
	}

	// Set parent group
	if originalGroup != nil && originalGroup.GroupType == "aggregate" && originalGroup.ID != group.ID {
		logEntry.ParentGroupID = originalGroup.ID
		logEntry.ParentGroupName = originalGroup.Name
	}

	if channelHandler != nil && bodyBytes != nil {
		logEntry.Model = channelHandler.ExtractModel(c, bodyBytes)
	}

	if apiKey != nil {
		// Request logs retain only a one-way identifier. The upstream credential
		// must never enter the log cache, database, API response, or CSV export.
		logEntry.KeyHash = ps.encryptionSvc.Hash(apiKey.KeyValue)
		logEntry.KeyValue = utils.KeyFingerprint(logEntry.KeyHash)
	}

	if finalError != nil {
		errorMessage := finalError.Error()
		if apiKey != nil {
			errorMessage = utils.SanitizeKnownSecrets(errorMessage, apiKey.KeyValue)
		}
		logEntry.ErrorMessage = utils.SanitizeText(errorMessage)
	}

	if err := ps.requestLogService.Record(logEntry); err != nil {
		logrus.Errorf("Failed to record request log: %v", err)
	}
}
