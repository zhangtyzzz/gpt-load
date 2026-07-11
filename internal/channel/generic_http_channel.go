package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Register(GenericHTTPChannelType, newGenericHTTPChannel)
}

type GenericHTTPChannel struct {
	*BaseChannel
	config    GenericHTTPConfig
	rawConfig []byte
}

func newGenericHTTPChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	normalized, cfg, err := NormalizeGenericHTTPConfig(group.ChannelConfig)
	if err != nil {
		return nil, err
	}
	for _, rule := range group.HeaderRuleList {
		if IsReservedProxyHeader(rule.Key) ||
			(cfg.Auth.Placement == GenericAuthHeader && strings.EqualFold(rule.Key, cfg.Auth.Name)) {
			return nil, fmt.Errorf("header rule %q conflicts with proxy-managed headers", rule.Key)
		}
	}
	base, err := f.newBaseChannel(GenericHTTPChannelType, group)
	if err != nil {
		return nil, err
	}

	// Generic credentials can use arbitrary header names. Refusing all
	// redirects is the only reliable way to prevent them leaking through a
	// Location hop without expanding the shared client cache with secret names.
	base.HTTPClient = cloneClientWithoutRedirects(base.HTTPClient)
	base.StreamClient = cloneClientWithoutRedirects(base.StreamClient)
	if len(base.Upstreams) == 0 {
		return nil, fmt.Errorf("generic HTTP requires at least one active upstream target")
	}
	for i := range base.Upstreams {
		if base.Upstreams[i].URL == nil {
			return nil, fmt.Errorf("generic HTTP upstream target is nil")
		}
		if _, strictErr := ParseAbsoluteHTTPURL(base.Upstreams[i].URL.String()); strictErr != nil {
			return nil, fmt.Errorf("invalid generic HTTP upstream target: %w", strictErr)
		}
	}

	return &GenericHTTPChannel{
		BaseChannel: base,
		config:      cfg,
		rawConfig:   normalized,
	}, nil
}

func cloneClientWithoutRedirects(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func (ch *GenericHTTPChannel) IsConfigStale(group *models.Group) bool {
	if ch.BaseChannel.IsConfigStale(group) {
		return true
	}
	normalized, _, err := NormalizeGenericHTTPConfig(group.ChannelConfig)
	return err != nil || !bytes.Equal(ch.rawConfig, normalized)
}

func (ch *GenericHTTPChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, _ *models.Group) {
	// net/http synthesizes "User-Agent: Go-http-client/1.1" when the header is
	// absent. An explicit empty value suppresses that default while preserving
	// any caller- or rule-provided value.
	if _, exists := req.Header["User-Agent"]; !exists {
		req.Header["User-Agent"] = []string{""}
	}
	credential := ch.config.Auth.Prefix + apiKey.KeyValue
	req.Header.Set(ch.config.Auth.Name, credential)
}

func (ch *GenericHTTPChannel) FinalizeCredentials(req *http.Request, apiKey *models.APIKey, group *models.Group) {
	ch.ModifyRequest(req, apiKey, group)
}

func (ch *GenericHTTPChannel) IsStreamRequest(c *gin.Context, _ []byte) bool {
	switch ch.config.StreamMode {
	case GenericStreamAlways:
		return true
	case GenericStreamNever:
		return false
	}
	return strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream")
}

func (ch *GenericHTTPChannel) ExtractModel(_ *gin.Context, _ []byte) string {
	return ""
}

func (ch *GenericHTTPChannel) ApplyModelRedirect(_ *http.Request, bodyBytes []byte, _ *models.Group) ([]byte, error) {
	return bodyBytes, nil
}

func (ch *GenericHTTPChannel) TransformModelList(_ *http.Request, _ []byte, _ *models.Group) (map[string]any, error) {
	return nil, fmt.Errorf("model list transformation is unsupported for generic HTTP")
}

func (ch *GenericHTTPChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, _ *models.Group) (bool, error) {
	validation := ch.config.Validation
	if !validation.Enabled {
		return false, fmt.Errorf("%w: validation is disabled", ErrValidationInconclusive)
	}

	baseURL := validation.BaseURL
	if baseURL == "" {
		upstream := ch.getUpstreamURL()
		if upstream == nil {
			return false, fmt.Errorf("no upstream URL configured for channel %s", ch.Name)
		}
		baseURL = upstream.String()
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return false, fmt.Errorf("invalid validation base URL: %w", err)
	}
	endpoint, err := url.Parse(validation.Path)
	if err != nil {
		return false, fmt.Errorf("invalid validation path: %w", err)
	}
	finalURL := *parsedBase
	finalURL.Path = strings.TrimRight(finalURL.Path, "/") + endpoint.Path
	finalURL.RawQuery = endpoint.RawQuery

	var body io.Reader
	if validation.Body != nil {
		encoded, marshalErr := json.Marshal(validation.Body)
		if marshalErr != nil {
			return false, fmt.Errorf("marshal validation body: %w", marshalErr)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, validation.Method, finalURL.String(), body)
	if err != nil {
		return false, fmt.Errorf("create validation request: %w", err)
	}
	for name, value := range validation.Headers {
		req.Header.Set(name, value)
	}
	if validation.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	ch.ModifyRequest(req, apiKey, nil)

	resp, err := ch.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: validation request failed: %s", ErrValidationInconclusive, utils.SanitizeKnownSecrets(err.Error(), apiKey.KeyValue))
	}
	defer resp.Body.Close()

	if containsStatus(validation.ValidStatuses, resp.StatusCode) {
		return true, nil
	}
	contentEncoding := strings.Join(resp.Header.Values("Content-Encoding"), ",")
	errorBody, readErr := utils.ReadCompressedBodyBounded(
		resp.Body,
		contentEncoding,
		defaultValidationResponseLimit,
		defaultValidationResponseLimit,
	)
	if readErr != nil {
		safeError := utils.SanitizeKnownSecrets(readErr.Error(), apiKey.KeyValue)
		return false, fmt.Errorf("%w: read validation response: %s", ErrValidationInconclusive, safeError)
	}
	message := utils.SanitizeKnownSecrets(app_errors.ParseUpstreamError(errorBody), apiKey.KeyValue)
	if containsStatus(validation.InvalidStatuses, resp.StatusCode) {
		return false, fmt.Errorf("[status %d] %s", resp.StatusCode, message)
	}
	return false, fmt.Errorf("%w: [status %d] %s", ErrValidationInconclusive, resp.StatusCode, message)
}

func (ch *GenericHTTPChannel) AllowRetry(method string, statusCode int, transportErr error) bool {
	safe := false
	for _, safeMethod := range ch.config.Retry.SafeMethods {
		if method == safeMethod {
			safe = true
			break
		}
	}
	if !safe {
		return false
	}
	if transportErr != nil {
		return true
	}
	return containsStatus(ch.config.Retry.FailoverStatuses, statusCode)
}

func (ch *GenericHTTPChannel) ClassifyResponse(method string, statusCode int, transportErr error) ResponseClassification {
	if transportErr != nil {
		return ResponseClassification{
			HandleAsFailure: true,
			UseErrorPolicy:  false,
			AllowRetry:      ch.AllowRetry(method, statusCode, transportErr),
		}
	}
	if containsStatus(ch.config.Retry.FailoverStatuses, statusCode) {
		return ResponseClassification{
			HandleAsFailure: true,
			UseErrorPolicy:  true,
			AllowRetry:      ch.AllowRetry(method, statusCode, nil),
		}
	}
	return ResponseClassification{}
}

func (ch *GenericHTTPChannel) ShouldTransformModelList(_ *http.Request) bool {
	return false
}

func (ch *GenericHTTPChannel) MaxRequestBodyBytes() int64 {
	return ch.config.MaxRequestBodyBytes
}

func (ch *GenericHTTPChannel) MaxErrorBodyBytes() int64 {
	return ch.config.MaxErrorBodyBytes
}

func (ch *GenericHTTPChannel) ShouldFlushResponse(_ bool, resp *http.Response) bool {
	switch ch.config.StreamMode {
	case GenericStreamAlways:
		return true
	case GenericStreamNever:
		return false
	default:
		return resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	}
}
