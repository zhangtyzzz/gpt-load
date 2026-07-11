package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/httpclient"
	"gpt-load/internal/keypool"
	"gpt-load/internal/middleware"
	"gpt-load/internal/models"
	"gpt-load/internal/services"
	"gpt-load/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type responseOutcomeHarness struct {
	baseURL     string
	client      *http.Client
	proxyKey    string
	upstreamKey string
	store       *store.MemoryStore
}

func newResponseOutcomeHarness(t *testing.T) *responseOutcomeHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		groupName   = "response-outcome"
		proxyKey    = "response-outcome-proxy-key"
		upstreamKey = "response-outcome-upstream-key"
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Upstream-Key") != "Token "+upstreamKey {
			http.Error(w, "unexpected credential", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/normal":
			w.Header().Set("Content-Length", "8")
			_, _ = io.WriteString(w, "complete")
		case "/truncated":
			w.Header().Set("Content-Length", "64")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "short")
		case "/cancel":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "data: first\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		case "/error-limit":
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, strings.Repeat("limit-body-", 8))
		case "/error-truncated":
			w.Header().Set("Content-Length", "64")
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, "short-error")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "response-outcome.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.Group{}, &models.APIKey{}, &models.GroupSubGroup{}, &models.RequestLog{}); err != nil {
		t.Fatal(err)
	}

	preset := findChannelPreset(t, "tavily-mcp")
	preset.ChannelConfig.Auth.Name = "X-Upstream-Key"
	preset.ChannelConfig.Auth.Prefix = "Token "
	preset.ChannelConfig.StreamMode = channel.GenericStreamAuto
	preset.ChannelConfig.MaxErrorBodyBytes = 16
	channelConfig, err := json.Marshal(preset.ChannelConfig)
	if err != nil {
		t.Fatal(err)
	}
	upstreams, err := json.Marshal([]map[string]any{{"url": upstream.URL, "weight": 1}})
	if err != nil {
		t.Fatal(err)
	}
	group := models.Group{
		Name:          groupName,
		ProxyKeys:     proxyKey,
		GroupType:     "standard",
		ChannelType:   channel.GenericHTTPChannelType,
		ChannelConfig: channelConfig,
		Upstreams:     upstreams,
		TestModel:     "-",
		Config: datatypes.JSONMap{
			"request_timeout":         5,
			"connect_timeout":         5,
			"response_header_timeout": 5,
			"max_retries":             0,
			"blacklist_threshold":     100,
		},
	}
	if err := database.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatal(err)
	}
	key := models.APIKey{
		GroupID:  group.ID,
		KeyValue: upstreamKey,
		KeyHash:  encryptionSvc.Hash(upstreamKey),
		Status:   models.KeyStatusActive,
	}
	if err := database.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	memoryStore := store.NewMemoryStore()
	t.Cleanup(func() { _ = memoryStore.Close() })
	settingsManager := config.NewSystemSettingsManager()
	subGroupManager := services.NewSubGroupManager(memoryStore)
	groupManager := services.NewGroupManager(database, memoryStore, settingsManager, subGroupManager)
	if err := groupManager.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { groupManager.Stop(context.Background()) })
	affinityManager := keypool.NewAffinityManager(memoryStore)
	keyProvider := keypool.NewProvider(database, memoryStore, settingsManager, encryptionSvc, affinityManager)
	if err := keyProvider.LoadKeysFromDB(); err != nil {
		t.Fatal(err)
	}
	requestLogService := services.NewRequestLogService(database, memoryStore, settingsManager)
	proxyServer, err := NewProxyServer(
		keyProvider,
		groupManager,
		subGroupManager,
		settingsManager,
		channel.NewFactory(settingsManager, httpclient.NewHTTPClientManager()),
		requestLogService,
		encryptionSvc,
		affinityManager,
	)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Any("/proxy/:group_name/*path", middleware.ProxyAuth(groupManager), proxyServer.HandleProxy)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &responseOutcomeHarness{
		baseURL:     server.URL + "/proxy/" + groupName,
		client:      server.Client(),
		proxyKey:    proxyKey,
		upstreamKey: upstreamKey,
		store:       memoryStore,
	}
}

func (h *responseOutcomeHarness) request(t *testing.T, ctx context.Context, path, accept string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(middleware.ProxyKeyHeader, h.proxyKey)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (h *responseOutcomeHarness) waitForOnlyLog(t *testing.T, path string) models.RequestLog {
	t.Helper()
	wantPath := "/proxy/response-outcome" + path
	deadline := time.Now().Add(5 * time.Second)
	var matching []models.RequestLog
	for time.Now().Before(deadline) {
		keys, err := h.store.SPopN(services.PendingLogKeysSet, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			encoded, getErr := h.store.Get(key)
			if getErr != nil {
				t.Fatal(getErr)
			}
			var entry models.RequestLog
			if err := json.Unmarshal(encoded, &entry); err != nil {
				t.Fatal(err)
			}
			if entry.RequestPath == wantPath {
				matching = append(matching, entry)
			}
		}
		if len(matching) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(matching) != 1 {
		t.Fatalf("request logs for %s = %d, want exactly one final log", wantPath, len(matching))
	}
	entry := matching[0]
	if entry.RequestType != models.RequestTypeFinal {
		t.Fatalf("request type=%q, want final", entry.RequestType)
	}
	if strings.Contains(entry.ErrorMessage, h.upstreamKey) {
		t.Fatalf("request log leaked selected key: %q", entry.ErrorMessage)
	}
	return entry
}

func TestResponseOutcomeEndToEnd(t *testing.T) {
	t.Run("completed 200 remains successful", func(t *testing.T) {
		harness := newResponseOutcomeHarness(t)
		resp := harness.request(t, context.Background(), "/normal", "")
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || string(body) != "complete" {
			t.Fatalf("status=%d body=%q err=%v", resp.StatusCode, body, err)
		}
		entry := harness.waitForOnlyLog(t, "/normal")
		if !entry.IsSuccess || entry.StatusCode != http.StatusOK || entry.ErrorMessage != "" {
			t.Fatalf("completed log=%#v", entry)
		}
	})

	t.Run("truncated 200 is not logged successful", func(t *testing.T) {
		harness := newResponseOutcomeHarness(t)
		resp := harness.request(t, context.Background(), "/truncated", "")
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !errors.Is(readErr, io.ErrUnexpectedEOF) {
			t.Fatalf("client status=%d body=%q err=%v", resp.StatusCode, body, readErr)
		}
		if string(body) != "short" || strings.Contains(string(body), `"error"`) {
			t.Fatalf("partial upstream body was overwritten: %q", body)
		}
		entry := harness.waitForOnlyLog(t, "/truncated")
		if entry.IsSuccess || entry.StatusCode != http.StatusBadGateway || !strings.Contains(entry.ErrorMessage, string(responseBodyUpstreamTruncated)) {
			t.Fatalf("truncated log=%#v", entry)
		}
	})

	for _, path := range []string{"/error-limit", "/error-truncated"} {
		t.Run("generic error body 502 is logged "+path, func(t *testing.T) {
			harness := newResponseOutcomeHarness(t)
			resp := harness.request(t, context.Background(), path, "")
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil || resp.StatusCode != http.StatusBadGateway {
				t.Fatalf("status=%d body=%q err=%v", resp.StatusCode, body, readErr)
			}
			entry := harness.waitForOnlyLog(t, path)
			if entry.IsSuccess || entry.StatusCode != http.StatusBadGateway || !strings.Contains(entry.ErrorMessage, "upstream_error_body_read_failed") {
				t.Fatalf("error-body log=%#v", entry)
			}
		})
	}

	t.Run("client cancellation is not completed", func(t *testing.T) {
		harness := newResponseOutcomeHarness(t)
		requestCtx, cancel := context.WithCancel(context.Background())
		resp := harness.request(t, requestCtx, "/cancel", "text/event-stream")
		reader := bufio.NewReader(resp.Body)
		if firstLine, err := reader.ReadString('\n'); err != nil || firstLine != "data: first\n" {
			t.Fatalf("first stream line=%q err=%v", firstLine, err)
		}
		cancel()
		_ = resp.Body.Close()

		entry := harness.waitForOnlyLog(t, "/cancel")
		if entry.IsSuccess || entry.StatusCode != 499 || !strings.Contains(entry.ErrorMessage, string(responseBodyClientCancelled)) {
			t.Fatalf("cancelled log=%#v", entry)
		}
	})
}
