package channel

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"gpt-load/internal/config"
	"gpt-load/internal/httpclient"
	"gpt-load/internal/models"
)

type genericRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn genericRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func testGenericConfig(t *testing.T, placement, name, prefix string) GenericHTTPConfig {
	t.Helper()
	raw, err := json.Marshal(GenericHTTPConfig{
		Version:    1,
		Auth:       GenericHTTPAuthConfig{Placement: placement, Name: name, Prefix: prefix},
		StreamMode: GenericStreamNever,
		Validation: GenericHTTPValidationConfig{
			Enabled:         true,
			Method:          http.MethodGet,
			Path:            "/usage",
			Headers:         map[string]string{"Accept": "application/json"},
			ValidStatuses:   []int{200},
			InvalidStatuses: []int{401},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseGenericHTTPConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGenericHTTPConfigValidation(t *testing.T) {
	cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
	if cfg.MaxRequestBodyBytes != defaultGenericRequestBodyLimit {
		t.Fatalf("unexpected request limit: %d", cfg.MaxRequestBodyBytes)
	}
	if got := cfg.Retry.SafeMethods; len(got) != 2 || got[0] != http.MethodGet || got[1] != http.MethodHead {
		t.Fatalf("unexpected safe methods: %#v", got)
	}

	invalid := []string{
		`{"version":2,"protocol":"http","auth":{"placement":"header","name":"Authorization","prefix":"Bearer "}}`,
		`{"version":1,"protocol":"http","auth":{"placement":"header","name":"Host","prefix":""}}`,
		`{"version":1,"protocol":"http","auth":{"placement":"header","name":"Authorization","prefix":"bad\nvalue"}}`,
		`{"version":1,"auth":{"placement":"query","name":"api_key","prefix":""}}`,
	}
	for _, raw := range invalid {
		if _, err := ParseGenericHTTPConfig([]byte(raw)); err == nil {
			t.Fatalf("expected invalid config: %s", raw)
		}
	}
}

func TestGenericHTTPChannelInjectsStructuredCredentials(t *testing.T) {
	key := &models.APIKey{KeyValue: "secret-value"}
	ch := &GenericHTTPChannel{config: testGenericConfig(t, GenericAuthHeader, "x-api-key", "token-")}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/search?keep=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	ch.ModifyRequest(req, key, nil)
	if got := req.Header.Get("x-api-key"); got != "token-secret-value" {
		t.Fatalf("header credential mismatch: %q", got)
	}
	if got := req.URL.RawQuery; got != "keep=1" {
		t.Fatalf("credential injection changed query: %q", got)
	}
	if values, exists := req.Header["User-Agent"]; !exists || len(values) != 1 || values[0] != "" {
		t.Fatalf("absent User-Agent was not explicitly suppressed: %#v", values)
	}
	callerUA, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	callerUA.Header.Set("User-Agent", "caller-agent")
	ch.ModifyRequest(callerUA, key, nil)
	if got := callerUA.Header.Get("User-Agent"); got != "caller-agent" {
		t.Fatalf("caller User-Agent was overwritten: %q", got)
	}
}

func TestGenericHTTPFinalCredentialsAndRetryGuard(t *testing.T) {
	cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
	cfg.Retry.FailoverStatuses = []int{http.StatusTooManyRequests}
	ch := &GenericHTTPChannel{config: cfg}
	req, _ := http.NewRequest(http.MethodPost, "https://example.test/search", nil)
	req.Header.Set("Authorization", "Bearer attacker")
	ch.FinalizeCredentials(req, &models.APIKey{KeyValue: "selected-key"}, nil)
	if got := req.Header.Get("Authorization"); got != "Bearer selected-key" {
		t.Fatalf("final credential = %q", got)
	}
	if ch.AllowRetry(http.MethodPost, http.StatusInternalServerError, nil) {
		t.Fatal("unsafe POST 500 was replayable")
	}
	if ch.AllowRetry(http.MethodPost, 0, errors.New("connection reset")) {
		t.Fatal("unsafe POST transport error was replayable")
	}
	if ch.AllowRetry(http.MethodPost, http.StatusTooManyRequests, nil) {
		t.Fatal("configured POST failure was replayable despite ambiguous outcome")
	}
	if !ch.AllowRetry(http.MethodGet, http.StatusTooManyRequests, nil) {
		t.Fatal("configured GET failure was not replayable")
	}
	if ch.AllowRetry(http.MethodGet, http.StatusInternalServerError, nil) {
		t.Fatal("unconfigured GET status was replayable")
	}
	if !ch.AllowRetry(http.MethodGet, 0, errors.New("connection reset")) {
		t.Fatal("safe GET transport error was not replayable")
	}
	if ch.AllowRetry("get", http.StatusTooManyRequests, nil) {
		t.Fatal("non-canonical lowercase get status was replayable")
	}
	if ch.AllowRetry("get", 0, errors.New("connection reset")) {
		t.Fatal("non-canonical lowercase get transport error was replayable")
	}
	if ch.ShouldTransformModelList(req) {
		t.Fatal("generic channel opted into model-list rewriting")
	}
	if transformed, err := ch.TransformModelList(req, []byte(`{"data":[{"id":"secret-shape"}]}`), nil); err == nil || transformed != nil {
		t.Fatalf("generic model-list transform = %#v, %v; want explicit unsupported error", transformed, err)
	}
}

func TestGenericHTTPResponseClassificationIsOptIn(t *testing.T) {
	cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
	cfg.Retry.FailoverStatuses = []int{http.StatusUnauthorized, http.StatusTooManyRequests}
	ch := &GenericHTTPChannel{config: cfg}

	for _, status := range []int{http.StatusNotModified, http.StatusFound, http.StatusConflict, http.StatusUnprocessableEntity} {
		if got := ch.ClassifyResponse(http.MethodPost, status, nil); got.HandleAsFailure || got.UseErrorPolicy || got.AllowRetry {
			t.Fatalf("status %d classification = %#v; want transparent", status, got)
		}
	}
	if got := ch.ClassifyResponse(http.MethodPost, http.StatusUnauthorized, nil); !got.HandleAsFailure || !got.UseErrorPolicy || got.AllowRetry {
		t.Fatalf("configured failover classification = %#v", got)
	}
	if got := ch.ClassifyResponse(http.MethodGet, http.StatusInternalServerError, nil); got.HandleAsFailure {
		t.Fatalf("unconfigured GET 500 classification = %#v", got)
	}
	cfg.Retry.FailoverStatuses = append(cfg.Retry.FailoverStatuses, http.StatusInternalServerError)
	ch.config = cfg
	if got := ch.ClassifyResponse(http.MethodPost, http.StatusInternalServerError, nil); !got.HandleAsFailure || !got.UseErrorPolicy || got.AllowRetry {
		t.Fatalf("configured POST 500 classification = %#v", got)
	}
	if got := ch.ClassifyResponse(http.MethodGet, http.StatusInternalServerError, nil); !got.HandleAsFailure || !got.UseErrorPolicy || !got.AllowRetry {
		t.Fatalf("configured GET 500 classification = %#v", got)
	}
	if got := ch.ClassifyResponse(http.MethodPost, 0, errors.New("reset")); !got.HandleAsFailure || got.UseErrorPolicy || got.AllowRetry {
		t.Fatalf("unsafe transport classification = %#v", got)
	}
	if got := ch.ClassifyResponse(http.MethodGet, 0, errors.New("reset")); !got.HandleAsFailure || got.UseErrorPolicy || !got.AllowRetry {
		t.Fatalf("safe transport classification = %#v", got)
	}
	if got := ch.ClassifyResponse("get", http.StatusInternalServerError, nil); !got.HandleAsFailure || !got.UseErrorPolicy || got.AllowRetry {
		t.Fatalf("lowercase status classification = %#v", got)
	}
	if got := ch.ClassifyResponse("get", 0, errors.New("reset")); !got.HandleAsFailure || got.UseErrorPolicy || got.AllowRetry {
		t.Fatalf("lowercase transport classification = %#v", got)
	}
}

func TestGenericHTTPConfigNormalizationAndProtectedHeaders(t *testing.T) {
	base := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
	base.PresetID = "  custom-preset  "
	base.Auth.Name = "authorization"
	base.Validation.ValidStatuses = []int{204, 200, 204}
	base.Validation.InvalidStatuses = []int{403, 401, 403}
	raw, _ := json.Marshal(base)
	cfg, err := ParseGenericHTTPConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PresetID != "custom-preset" || cfg.Auth.Name != "Authorization" {
		t.Fatalf("preset/auth normalization = %q/%q", cfg.PresetID, cfg.Auth.Name)
	}
	if got := cfg.Validation.ValidStatuses; len(got) != 2 || got[0] != 200 || got[1] != 204 {
		t.Fatalf("valid statuses = %#v", got)
	}
	if got := cfg.Validation.InvalidStatuses; len(got) != 2 || got[0] != 401 || got[1] != 403 {
		t.Fatalf("invalid statuses = %#v", got)
	}
	for _, protected := range []string{"Host", "Content-Length", "Connection", "Last-Event-ID", "X-Gpt-Load-Key"} {
		if !IsReservedProxyHeader(protected) {
			t.Fatalf("header %q was not protected", protected)
		}
	}
	reservedControlHeader := base
	reservedControlHeader.Auth.Name = "X-Gpt-Load-Key"
	reservedRaw, _ := json.Marshal(reservedControlHeader)
	if _, err := ParseGenericHTTPConfig(reservedRaw); err == nil {
		t.Fatal("dedicated proxy credential header was accepted as an upstream auth header")
	}
	removedFieldName := "route_" + "affinity"
	withRemovedField := append(raw[:len(raw)-1], []byte(`,"`+removedFieldName+`":{"enabled":false}}`)...)
	if _, err := ParseGenericHTTPConfig(withRemovedField); err == nil {
		t.Fatal("removed affinity routing field was silently accepted")
	}
}

func TestParseAbsoluteHTTPURL(t *testing.T) {
	valid := []string{"http://example.test", "https://example.test/base", " HTTPS://Example.test/path "}
	for _, raw := range valid {
		if _, err := ParseAbsoluteHTTPURL(raw); err != nil {
			t.Errorf("ParseAbsoluteHTTPURL(%q) error = %v", raw, err)
		}
	}
	invalid := []string{"/relative", "ftp://example.test", "https:///missing-host", "https://user:pass@example.test", "https://example.test/path#fragment", "https://example.test/path?region=us", "https://example.test/path?api_key=secret"}
	for _, raw := range invalid {
		if _, err := ParseAbsoluteHTTPURL(raw); err == nil {
			t.Errorf("ParseAbsoluteHTTPURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestParseGenericHTTPConfigRejectsCaseInsensitiveDuplicateValidationHeaders(t *testing.T) {
	cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
	cfg.Validation.Headers = map[string]string{"X-Trace": "one", "x-trace": "two"}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGenericHTTPConfig(raw); err == nil {
		t.Fatal("case-insensitive duplicate validation headers were accepted")
	}
}

func TestBaseChannelBuildUpstreamURLRotatesTargets(t *testing.T) {
	one, _ := url.Parse("https://one.example/base")
	two, _ := url.Parse("https://two.example")
	base := &BaseChannel{Name: GenericHTTPChannelType, Upstreams: []UpstreamInfo{{URL: one, Weight: 1}, {URL: two, Weight: 1}}}
	requestURL, _ := url.Parse("https://proxy.test/proxy/group/v1/search?q=term")
	first, err := base.BuildUpstreamURL(requestURL, "group")
	if err != nil {
		t.Fatal(err)
	}
	second, err := base.BuildUpstreamURL(requestURL, "group")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("weighted targets did not rotate: %q", first)
	}
	for _, built := range []string{first, second} {
		parsed, _ := url.Parse(built)
		if parsed.Host == "" || !strings.HasSuffix(parsed.Path, "/v1/search") || parsed.Query().Get("q") != "term" {
			t.Fatalf("rotated target URL = %q", built)
		}
	}
}

func TestBuildUpstreamURLPreservesEscapedPathAndQuery(t *testing.T) {
	base, err := url.Parse("https://upstream.example/root%2Fbase")
	if err != nil {
		t.Fatal(err)
	}
	requestURL, err := url.Parse("https://proxy.example/proxy/%67roup/files/a%2Fb/%2525/%25?q=a%2Fb&percent=%2525")
	if err != nil {
		t.Fatal(err)
	}

	built := buildUpstreamURLFromBase(base, requestURL, "group")
	const want = "https://upstream.example/root%2Fbase/files/a%2Fb/%2525/%25?q=a%2Fb&percent=%2525"
	if built != want {
		t.Fatalf("built URL = %q, want %q", built, want)
	}

	parsed, err := url.Parse(built)
	if err != nil {
		t.Fatal(err)
	}
	if got, wantPath := parsed.Path, "/root/base/files/a/b/%25/%"; got != wantPath {
		t.Fatalf("decoded path = %q, want %q", got, wantPath)
	}
	if got, wantEscaped := parsed.EscapedPath(), "/root%2Fbase/files/a%2Fb/%2525/%25"; got != wantEscaped {
		t.Fatalf("escaped path = %q, want %q", got, wantEscaped)
	}
	if got := parsed.RawQuery; got != "q=a%2Fb&percent=%2525" {
		t.Fatalf("raw query = %q", got)
	}
}

func TestGenericHTTPStreamModeControlsClientHintAndResponseFlush(t *testing.T) {
	for _, tc := range []struct {
		mode             string
		requestAccept    string
		requestURL       string
		requestBody      string
		responseType     string
		wantStreamClient bool
		wantFlush        bool
	}{
		{mode: GenericStreamNever, requestAccept: "text/event-stream", requestURL: "/", responseType: "text/event-stream", wantStreamClient: false, wantFlush: false},
		{mode: GenericStreamAuto, requestAccept: "application/json, text/event-stream", requestURL: "/", responseType: "application/json", wantStreamClient: true, wantFlush: false},
		{mode: GenericStreamAuto, requestAccept: "application/json", requestURL: "/?stream=true", requestBody: `{"stream":true}`, responseType: "text/event-stream", wantStreamClient: false, wantFlush: true},
		{mode: GenericStreamAlways, requestAccept: "application/json", requestURL: "/", responseType: "application/json", wantStreamClient: true, wantFlush: true},
	} {
		t.Run(tc.mode+tc.requestAccept+tc.responseType, func(t *testing.T) {
			cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
			cfg.StreamMode = tc.mode
			ch := &GenericHTTPChannel{config: cfg}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, tc.requestURL, strings.NewReader(tc.requestBody))
			ctx.Request.Header.Set("Accept", tc.requestAccept)
			if got := ch.IsStreamRequest(ctx, []byte(tc.requestBody)); got != tc.wantStreamClient {
				t.Fatalf("IsStreamRequest() = %v, want %v", got, tc.wantStreamClient)
			}
			resp := &http.Response{Header: http.Header{"Content-Type": []string{tc.responseType}}}
			if got := ch.ShouldFlushResponse(tc.wantStreamClient, resp); got != tc.wantFlush {
				t.Fatalf("ShouldFlushResponse() = %v, want %v", got, tc.wantFlush)
			}
		})
	}
}

func TestGenericHTTPValidationIsTriStateAndRedactsSecret(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		wantValid   bool
		wantUnknown bool
	}{
		{name: "valid", status: 200, wantValid: true},
		{name: "invalid", status: 401},
		{name: "inconclusive", status: 429, wantUnknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
			client := &http.Client{Transport: genericRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("Authorization"); got != "Bearer secret-value" {
					t.Fatalf("credential was not injected: %q", got)
				}
				return &http.Response{
					StatusCode: tc.status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"secret-value"}`)),
					Request:    req,
				}, nil
			})}
			baseURL, _ := url.Parse("https://provider.test")
			ch := &GenericHTTPChannel{
				BaseChannel: &BaseChannel{HTTPClient: client, Upstreams: []UpstreamInfo{{URL: baseURL, Weight: 1}}},
				config:      cfg,
			}
			valid, err := ch.ValidateKey(context.Background(), &models.APIKey{KeyValue: "secret-value"}, nil)
			if valid != tc.wantValid {
				t.Fatalf("valid=%v, want %v", valid, tc.wantValid)
			}
			if tc.wantValid && err != nil {
				t.Fatal(err)
			}
			if !tc.wantValid && err == nil {
				t.Fatal("expected validation error")
			}
			if tc.wantUnknown != errors.Is(err, ErrValidationInconclusive) {
				t.Fatalf("inconclusive=%v, error=%v", errors.Is(err, ErrValidationInconclusive), err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("validation error leaked secret: %v", err)
			}
		})
	}
}

func TestGenericHTTPValidationUsesBoundedContentDecoding(t *testing.T) {
	const secret = "validation-secret-content-encoding"
	payload := []byte(`{"error":"` + secret + `"}`)
	gzipPayload := encodeGenericValidationBody(t, "gzip", payload)

	for _, tc := range []struct {
		name             string
		encoding         string
		body             []byte
		wantInconclusive bool
	}{
		{name: "stacked gzip and brotli", encoding: "gzip, br", body: encodeGenericValidationBody(t, "br", gzipPayload)},
		{name: "malformed gzip", encoding: "gzip", body: []byte("not-gzip-" + secret), wantInconclusive: true},
		{name: "unsupported secret token", encoding: secret, body: payload, wantInconclusive: true},
		{name: "encoded input over limit", body: bytes.Repeat([]byte("x"), int(defaultValidationResponseLimit)+1), wantInconclusive: true},
		{name: "decoded output over limit", encoding: "gzip", body: encodeGenericValidationBody(t, "gzip", bytes.Repeat([]byte("x"), int(defaultValidationResponseLimit)+1)), wantInconclusive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: genericRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				if tc.encoding != "" {
					header.Set("Content-Encoding", tc.encoding)
				}
				return &http.Response{StatusCode: http.StatusUnauthorized, Header: header, Body: io.NopCloser(bytes.NewReader(tc.body)), Request: req}, nil
			})}
			baseURL, _ := url.Parse("https://provider.test")
			ch := &GenericHTTPChannel{
				BaseChannel: &BaseChannel{HTTPClient: client, Upstreams: []UpstreamInfo{{URL: baseURL, Weight: 1}}},
				config:      testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer "),
			}
			valid, err := ch.ValidateKey(context.Background(), &models.APIKey{KeyValue: secret}, nil)
			if valid || err == nil {
				t.Fatalf("ValidateKey() = %v, %v", valid, err)
			}
			if got := errors.Is(err, ErrValidationInconclusive); got != tc.wantInconclusive {
				t.Fatalf("inconclusive=%v want=%v error=%v", got, tc.wantInconclusive, err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("validation decoding error leaked secret: %v", err)
			}
		})
	}
}

func TestGenericHTTPUsesRepresentationTransparentTransport(t *testing.T) {
	cfg := testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer ")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	group := &models.Group{
		ID:            1001,
		ChannelType:   GenericHTTPChannelType,
		Upstreams:     []byte(`[{"url":"https://example.test","weight":1}]`),
		ChannelConfig: raw,
		TestModel:     "-",
	}
	factory := NewFactory(config.NewSystemSettingsManager(), httpclient.NewHTTPClientManager())
	proxy, err := factory.GetChannel(group)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := proxy.GetHTTPClient().Transport.(*http.Transport)
	if !ok || !transport.DisableCompression {
		t.Fatalf("generic transport=%T DisableCompression=%v", proxy.GetHTTPClient().Transport, ok && transport.DisableCompression)
	}

	legacyBase, err := factory.newBaseChannel("openai", &models.Group{Upstreams: group.Upstreams, TestModel: "-"})
	if err != nil {
		t.Fatal(err)
	}
	legacyTransport, ok := legacyBase.GetHTTPClient().Transport.(*http.Transport)
	if !ok || legacyTransport.DisableCompression {
		t.Fatalf("legacy transport=%T DisableCompression=%v", legacyBase.GetHTTPClient().Transport, ok && legacyTransport.DisableCompression)
	}
}

func encodeGenericValidationBody(t *testing.T, encoding string, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	switch encoding {
	case "gzip":
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	case "br":
		writer := brotli.NewWriter(&buffer)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported test encoding %q", encoding)
	}
	return buffer.Bytes()
}

func TestGenericHTTPDisablesRedirects(t *testing.T) {
	client := cloneClientWithoutRedirects(&http.Client{})
	req, _ := http.NewRequest(http.MethodGet, "https://other.test", nil)
	if err := client.CheckRedirect(req, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
}

func TestGenericHTTPChannelConfigParticipatesInCacheStaleness(t *testing.T) {
	raw, err := json.Marshal(testGenericConfig(t, GenericAuthHeader, "Authorization", "Bearer "))
	if err != nil {
		t.Fatal(err)
	}
	normalized, _, err := NormalizeGenericHTTPConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	group := &models.Group{
		ChannelType:   GenericHTTPChannelType,
		TestModel:     "-",
		Upstreams:     []byte(`[{"url":"https://example.test","weight":1}]`),
		ChannelConfig: normalized,
	}
	ch := &GenericHTTPChannel{
		BaseChannel: &BaseChannel{
			channelType:     group.ChannelType,
			TestModel:       group.TestModel,
			groupUpstreams:  group.Upstreams,
			effectiveConfig: &group.EffectiveConfig,
		},
		rawConfig: normalized,
	}
	if ch.IsConfigStale(group) {
		t.Fatal("unchanged channel_config was reported stale")
	}

	changed := *group
	changedConfig := testGenericConfig(t, GenericAuthHeader, "Authorization", "Token ")
	changed.ChannelConfig, err = json.Marshal(changedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !ch.IsConfigStale(&changed) {
		t.Fatal("changed channel_config did not invalidate cached channel")
	}
}

func TestChannelCatalogConfigsAreValid(t *testing.T) {
	catalog := GetChannelCatalog()
	if len(catalog) != 7 {
		t.Fatalf("catalog size = %d", len(catalog))
	}
	seen := make(map[string]struct{}, len(catalog))
	for _, preset := range catalog {
		if _, duplicate := seen[preset.ID]; duplicate {
			t.Fatalf("duplicate preset %q", preset.ID)
		}
		seen[preset.ID] = struct{}{}
		raw, err := json.Marshal(preset.ChannelConfig)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseGenericHTTPConfig(raw); err != nil {
			t.Fatalf("preset %q is invalid: %v", preset.ID, err)
		}
		if strings.Contains(string(raw), "protocol") || strings.Contains(string(raw), "mcp_session") {
			t.Fatalf("preset %q leaked protocol-specific runtime config: %s", preset.ID, raw)
		}
	}
}
