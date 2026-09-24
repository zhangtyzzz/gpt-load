package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

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

// retryBudgetHarness builds one aggregate group whose two children fail every
// request with a retryable 500. Child A carries an overwhelming selection
// weight, so within a single request the failover chain always exhausts A's
// keys before falling through to B. That makes the per-child upstream call
// counts deterministic for budget assertions.
type retryBudgetHarness struct {
	proxyURL string
	client   *http.Client
	aCalls   atomic.Int64
	bCalls   atomic.Int64
}

type retryBudgetChild struct {
	name   string
	budget int
	keys   int
}

func newRetryBudgetHarness(t *testing.T, childA, childB retryBudgetChild) *retryBudgetHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const proxyKey = "retry-budget-proxy-key"
	harness := &retryBudgetHarness{}

	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		harness.aCalls.Add(1)
		http.Error(w, "A failed", http.StatusInternalServerError)
	}))
	t.Cleanup(upstreamA.Close)
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		harness.bCalls.Add(1)
		http.Error(w, "B failed", http.StatusInternalServerError)
	}))
	t.Cleanup(upstreamB.Close)

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "retry-budget.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.Group{}, &models.APIKey{}, &models.GroupSubGroup{}); err != nil {
		t.Fatal(err)
	}

	preset := findChannelPreset(t, "tavily-mcp")
	preset.ChannelConfig.StreamMode = channel.GenericStreamNever
	preset.ChannelConfig.Retry.FailoverStatuses = []int{http.StatusInternalServerError}
	channelConfig, err := json.Marshal(preset.ChannelConfig)
	if err != nil {
		t.Fatal(err)
	}
	upstreams := func(rawURL string) datatypes.JSON {
		encoded, marshalErr := json.Marshal([]map[string]any{{"url": rawURL, "weight": 1}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return encoded
	}
	groupConfig := func(maxRetries int) datatypes.JSONMap {
		return datatypes.JSONMap{
			"request_timeout":         5,
			"connect_timeout":         5,
			"response_header_timeout": 5,
			"max_retries":             maxRetries,
			"blacklist_threshold":     100,
		}
	}

	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatal(err)
	}

	childSpecs := []struct {
		spec     retryBudgetChild
		upstream string
		weight   int
	}{
		{spec: childA, upstream: upstreamA.URL, weight: 1000},
		{spec: childB, upstream: upstreamB.URL, weight: 1},
	}
	children := make([]models.Group, 0, len(childSpecs))
	var keys []models.APIKey
	for _, spec := range childSpecs {
		child := models.Group{
			Name:          "budget-child-" + spec.spec.name,
			GroupType:     "standard",
			ChannelType:   channel.GenericHTTPChannelType,
			ChannelConfig: channelConfig,
			Upstreams:     upstreams(spec.upstream),
			TestModel:     "-",
			Config:        groupConfig(spec.spec.budget),
		}
		children = append(children, child)
	}
	if err := database.Create(&children).Error; err != nil {
		t.Fatal(err)
	}
	for index, spec := range childSpecs {
		for i := 0; i < spec.spec.keys; i++ {
			keyValue := fmt.Sprintf("budget-key-%s-%d", spec.spec.name, i)
			keys = append(keys, models.APIKey{
				GroupID:  children[index].ID,
				KeyValue: keyValue,
				KeyHash:  encryptionSvc.Hash(keyValue),
				Status:   models.KeyStatusActive,
			})
		}
	}
	if err := database.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}

	parent := models.Group{
		Name:          "budget-entry",
		ProxyKeys:     proxyKey,
		GroupType:     "aggregate",
		ChannelType:   channel.GenericHTTPChannelType,
		ChannelConfig: datatypes.JSON(`{}`),
		Upstreams:     datatypes.JSON(`[]`),
		TestModel:     "-",
	}
	if err := database.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	relations := []models.GroupSubGroup{
		{GroupID: parent.ID, SubGroupID: children[0].ID, Weight: 1000},
		{GroupID: parent.ID, SubGroupID: children[1].ID, Weight: 1},
	}
	if err := database.Create(&relations).Error; err != nil {
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
	proxyServer, err := NewProxyServer(
		keyProvider,
		groupManager,
		subGroupManager,
		settingsManager,
		channel.NewFactory(settingsManager, httpclient.NewHTTPClientManager()),
		nil,
		encryptionSvc,
		affinityManager,
	)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Any("/proxy/:group_name/*path", middleware.ProxyAuth(groupManager), proxyServer.HandleProxy)
	proxyHTTPServer := httptest.NewServer(router)
	t.Cleanup(proxyHTTPServer.Close)

	harness.proxyURL = proxyHTTPServer.URL + "/proxy/" + parent.Name + "/work"
	harness.client = proxyHTTPServer.Client()
	return harness
}

func (h *retryBudgetHarness) request(t *testing.T) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.proxyURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer retry-budget-proxy-key")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// TestAggregateRetryBudgetFrozenFromFirstGroup pins the per-request retry
// budget to the group that serves the first attempt. A group reached later in
// the failover chain must neither extend nor truncate the frozen budget,
// regardless of its own max_retries override.
func TestAggregateRetryBudgetFrozenFromFirstGroup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		childA     retryBudgetChild
		childB     retryBudgetChild
		wantACalls int64
		wantBCalls int64
		wantStatus int
		wantBody   string
	}{
		{
			name:       "later group cannot increase the request budget",
			childA:     retryBudgetChild{name: "a", budget: 2, keys: 2},
			childB:     retryBudgetChild{name: "b", budget: 5, keys: 4},
			wantACalls: 2,
			wantBCalls: 1,
			wantStatus: http.StatusInternalServerError,
			wantBody:   "B failed",
		},
		{
			name:       "later group cannot reduce the request budget",
			childA:     retryBudgetChild{name: "a", budget: 5, keys: 1},
			childB:     retryBudgetChild{name: "b", budget: 0, keys: 4},
			wantACalls: 1,
			wantBCalls: 4,
			wantStatus: http.StatusServiceUnavailable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := newRetryBudgetHarness(t, tc.childA, tc.childB)

			status, body := harness.request(t)

			if harness.aCalls.Load() != tc.wantACalls || harness.bCalls.Load() != tc.wantBCalls {
				t.Fatalf("upstream calls A=%d B=%d, want A=%d B=%d (budget must stay frozen from the first group)",
					harness.aCalls.Load(), harness.bCalls.Load(), tc.wantACalls, tc.wantBCalls)
			}
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", status, tc.wantStatus, body)
			}
			if tc.wantBody != "" && !strings.Contains(body, tc.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", body, tc.wantBody)
			}
		})
	}
}
