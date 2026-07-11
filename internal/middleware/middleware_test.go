package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gpt-load/internal/channel"
	"gpt-load/internal/models"
	"gpt-load/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type proxyGroupLookupStub struct {
	group *models.Group
	err   error
	calls int
}

func (s *proxyGroupLookupStub) GetGroupByName(string) (*models.Group, error) {
	s.calls++
	return s.group, s.err
}

func TestLoggerRedactsCredentialQueriesWithoutMutatingRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	oldFormatter := logrus.StandardLogger().Formatter
	oldLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(oldOutput)
		logrus.SetFormatter(oldFormatter)
		logrus.SetLevel(oldLevel)
	})

	const keySecret = "query-key-secret"
	const tokenSecret = "access-token-secret"
	requestQuerySeenByHandler := ""

	router := gin.New()
	router.Use(Logger(types.LogConfig{}))
	router.GET("/api/integration/info", func(c *gin.Context) {
		requestQuerySeenByHandler = c.Request.URL.RawQuery
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/integration/info?key="+keySecret+"&access_token="+tokenSecret+"&model=gpt-4&page=2", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	output := logs.String()
	for _, secret := range []string{keySecret, tokenSecret} {
		if strings.Contains(output, secret) {
			t.Fatalf("access log leaked %q: %s", secret, output)
		}
		if !strings.Contains(requestQuerySeenByHandler, secret) {
			t.Fatalf("logger unexpectedly mutated request query; handler did not receive %q", secret)
		}
	}
	for _, safe := range []string{"model=gpt-4", "page=2"} {
		if !strings.Contains(output, safe) {
			t.Errorf("access log lost non-sensitive query %q: %s", safe, output)
		}
	}
}

func TestExtractAuthKeyPreservesGenericQueryAndKeepsLegacyCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)

	genericCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	genericCtx.Request = httptest.NewRequest(http.MethodGet, "/proxy/generic/search?key=business%2Fvalue&x=1&x=2", nil)
	genericCtx.Request.Header.Set("Authorization", "Bearer proxy-key")
	originalQuery := genericCtx.Request.URL.RawQuery
	if got := extractAuthKey(genericCtx, false); got != "proxy-key" {
		t.Fatalf("generic header auth = %q", got)
	}
	if got := genericCtx.Request.URL.RawQuery; got != originalQuery {
		t.Fatalf("generic auth mutated query: got %q want %q", got, originalQuery)
	}

	legacyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	legacyCtx.Request = httptest.NewRequest(http.MethodGet, "/proxy/legacy/search?key=legacy-proxy&x=1", nil)
	if got := extractAuthKey(legacyCtx, true); got != "legacy-proxy" {
		t.Fatalf("legacy query auth = %q", got)
	}
	if got := legacyCtx.Request.URL.Query().Get("key"); got != "" {
		t.Fatalf("legacy proxy key remained in query: %q", got)
	}
}

func TestProxyAuthRejectsBeforeLookupAndDoesNotExposeLookupFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("missing credential avoids lookup", func(t *testing.T) {
		lookup := &proxyGroupLookupStub{err: errors.New("must not be called")}
		router := gin.New()
		router.GET("/proxy/:group_name/*path", ProxyAuth(lookup), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/proxy/unknown/path", nil))
		if recorder.Code != http.StatusUnauthorized || lookup.calls != 0 {
			t.Fatalf("status=%d lookup calls=%d", recorder.Code, lookup.calls)
		}
	})

	t.Run("lookup failure is still unauthorized", func(t *testing.T) {
		lookup := &proxyGroupLookupStub{err: errors.New("group missing")}
		router := gin.New()
		router.GET("/proxy/:group_name/*path", ProxyAuth(lookup), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/proxy/unknown/path", nil)
		req.Header.Set("Authorization", "Bearer attacker")
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized || lookup.calls != 1 {
			t.Fatalf("status=%d lookup calls=%d body=%q", recorder.Code, lookup.calls, recorder.Body.String())
		}
	})
}

func TestProxyAuthChecksEveryHeaderCandidateAndRecordsOnlyConsumedNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const proxyKey = "valid-proxy-key"
	group := &models.Group{
		ChannelType: channel.GenericHTTPChannelType,
		ProxyKeysMap: map[string]struct{}{
			proxyKey: {},
		},
	}
	lookup := &proxyGroupLookupStub{group: group}

	tests := []struct {
		name           string
		headers        http.Header
		wantStatus     int
		wantConsumed   []string
		wantAuthHeader string
	}{
		{
			name: "upstream authorization does not shadow dedicated proxy carrier",
			headers: http.Header{
				"Authorization":   []string{"Bearer upstream-business-credential"},
				ProxyKeyHeader:    []string{proxyKey},
				"X-Unrelated-Key": []string{"keep"},
			},
			wantStatus:     http.StatusNoContent,
			wantConsumed:   []string{ProxyKeyHeader},
			wantAuthHeader: "Bearer upstream-business-credential",
		},
		{
			name: "invalid first candidate falls through to later x api key",
			headers: http.Header{
				"Authorization": []string{"Bearer invalid-first"},
				"X-Api-Key":     []string{proxyKey},
			},
			wantStatus:     http.StatusNoContent,
			wantConsumed:   []string{"X-Api-Key"},
			wantAuthHeader: "Bearer invalid-first",
		},
		{
			name: "all matching carriers are recorded by name",
			headers: http.Header{
				"Authorization": []string{"Bearer " + proxyKey},
				ProxyKeyHeader:  []string{proxyKey},
			},
			wantStatus:     http.StatusNoContent,
			wantConsumed:   []string{"Authorization", ProxyKeyHeader},
			wantAuthHeader: "Bearer " + proxyKey,
		},
		{
			name: "invalid dedicated carrier does not shadow valid authorization",
			headers: http.Header{
				"Authorization": []string{"Bearer " + proxyKey},
				ProxyKeyHeader:  []string{"stale-control-value"},
			},
			wantStatus:     http.StatusNoContent,
			wantConsumed:   []string{"Authorization"},
			wantAuthHeader: "Bearer " + proxyKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var consumed []string
			var authHeader string
			router := gin.New()
			router.GET("/proxy/:group_name/*path", ProxyAuth(lookup), func(c *gin.Context) {
				consumed = ConsumedProxyCredentialHeaders(c)
				authHeader = c.Request.Header.Get("Authorization")
				c.Status(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/proxy/generic/path", nil)
			req.Header = tc.headers.Clone()
			router.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
			}
			if strings.Join(consumed, ",") != strings.Join(tc.wantConsumed, ",") {
				t.Fatalf("consumed headers=%#v, want %#v", consumed, tc.wantConsumed)
			}
			if authHeader != tc.wantAuthHeader {
				t.Fatalf("middleware mutated end-to-end Authorization: %q", authHeader)
			}
		})
	}
}
