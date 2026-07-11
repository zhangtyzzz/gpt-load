package channel

import (
	"context"
	"errors"
	"gpt-load/internal/models"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// ErrValidationInconclusive indicates that a key could not be classified as
// valid or invalid (for example because the upstream was rate limited). Callers
// must not change key health when this error is returned.
var ErrValidationInconclusive = errors.New("key validation inconclusive")

// ChannelProxy defines the interface for different API channel proxies.
type ChannelProxy interface {
	// BuildUpstreamURL constructs the target URL for the upstream service.
	BuildUpstreamURL(originalURL *url.URL, groupName string) (string, error)

	// IsConfigStale checks if the channel's configuration is stale compared to the provided group.
	IsConfigStale(group *models.Group) bool

	// GetHTTPClient returns the client for standard requests.
	GetHTTPClient() *http.Client

	// GetStreamClient returns the client for streaming requests.
	GetStreamClient() *http.Client

	// ModifyRequest allows the channel to add specific headers or modify the request
	ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group)

	// IsStreamRequest checks if the request is for a streaming response,
	IsStreamRequest(c *gin.Context, bodyBytes []byte) bool

	// ExtractModel extracts the model name from the request.
	ExtractModel(c *gin.Context, bodyBytes []byte) string

	// ValidateKey checks if the given API key is valid.
	ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error)

	// ApplyModelRedirect applies model redirection based on the group's redirect rules.
	ApplyModelRedirect(req *http.Request, bodyBytes []byte, group *models.Group) ([]byte, error)

	// TransformModelList transforms the model list response based on redirect rules.
	TransformModelList(req *http.Request, bodyBytes []byte, group *models.Group) (map[string]any, error)
}

// RetryGuard is an optional channel capability. The proxy evaluates the group
// error policy first and then asks this guard whether replaying the request is
// safe. Channels that do not implement it retain the historical behaviour.
type RetryGuard interface {
	AllowRetry(method string, statusCode int, transportErr error) bool
}

// ModelListPolicy is an optional channel capability used to opt out of the
// OpenAI/Gemini model-list interceptor.
type ModelListPolicy interface {
	ShouldTransformModelList(req *http.Request) bool
}

// RequestLimitProvider lets a channel bound buffered request bodies without
// changing the compatibility contract of existing channels.
type RequestLimitProvider interface {
	MaxRequestBodyBytes() int64
}

// ErrorBodyLimitProvider bounds error responses that the retry path buffers.
type ErrorBodyLimitProvider interface {
	MaxErrorBodyBytes() int64
}

// ResponseClassification lets a channel decide whether a transport outcome is
// transparent or should enter the shared retry/health policy. Channels that do
// not implement ResponseClassifier keep the historical non-2xx behaviour.
type ResponseClassification struct {
	HandleAsFailure bool
	UseErrorPolicy  bool
	AllowRetry      bool
}

type ResponseClassifier interface {
	ClassifyResponse(method string, statusCode int, transportErr error) ResponseClassification
}

// StreamResponsePolicy makes request-side client selection and response-side
// flushing explicit. It is optional to preserve existing channel behaviour.
type StreamResponsePolicy interface {
	ShouldFlushResponse(requestUsedStreamClient bool, resp *http.Response) bool
}

// CredentialFinalizer lets a channel re-assert structured credentials after
// generic header rules have run, preventing an accidental override.
type CredentialFinalizer interface {
	FinalizeCredentials(req *http.Request, apiKey *models.APIKey, group *models.Group)
}
