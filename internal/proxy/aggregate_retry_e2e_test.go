package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/errorpolicy"
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

type aggregateRetryHarness struct {
	proxyURL    string
	client      *http.Client
	proxyKey    string
	childA      models.Group
	keyA        models.APIKey
	keyProvider *keypool.KeyProvider
	database    *gorm.DB
	encryption  encryption.Service
	affinity    *keypool.AffinityManager
	aCalls      atomic.Int64
	bCalls      atomic.Int64
	aCredential atomic.Value
	bCredential atomic.Value
}

func newAggregateRetryHarness(t *testing.T) *aggregateRetryHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const (
		proxyKey = "aggregate-proxy-key"
		keyA     = "aggregate-upstream-a"
		keyA2    = "aggregate-upstream-a-2"
		keyB     = "aggregate-upstream-b"
	)
	harness := &aggregateRetryHarness{proxyKey: proxyKey}

	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		harness.aCalls.Add(1)
		credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		harness.aCredential.Store(credential)
		if credential != keyA && credential != keyA2 {
			http.Error(w, "wrong A credential", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/affinity" {
			_, _ = io.WriteString(w, "served-by-a")
			return
		}
		http.Error(w, "A failed", http.StatusInternalServerError)
	}))
	t.Cleanup(upstreamA.Close)
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		harness.bCalls.Add(1)
		credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		harness.bCredential.Store(credential)
		if credential != keyB {
			http.Error(w, "wrong B credential", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "served-by-b")
	}))
	t.Cleanup(upstreamB.Close)

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "aggregate-retry.db")), &gorm.Config{})
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
	groupConfig := datatypes.JSONMap{
		"request_timeout":         5,
		"connect_timeout":         5,
		"response_header_timeout": 5,
		"max_retries":             1,
		"blacklist_threshold":     100,
	}
	affinityRules, err := json.Marshal([]models.AffinityRule{{
		Name:      "aggregate legacy key affinity",
		Match:     models.AffinityMatchRule{PathRegex: `^/proxy/[^/]+/affinity$`},
		KeySource: models.AffinityKeySource{Type: "header", Key: "X-Legacy-Session"},
		TTL:       3600,
	}})
	if err != nil {
		t.Fatal(err)
	}
	children := []models.Group{
		{Name: "aggregate-child-a", GroupType: "standard", ChannelType: channel.GenericHTTPChannelType, ChannelConfig: channelConfig, Upstreams: upstreams(upstreamA.URL), TestModel: "-", Config: groupConfig, AffinityRules: affinityRules},
		{Name: "aggregate-child-b", GroupType: "standard", ChannelType: channel.GenericHTTPChannelType, ChannelConfig: channelConfig, Upstreams: upstreams(upstreamB.URL), TestModel: "-", Config: groupConfig, AffinityRules: affinityRules},
	}
	if err := database.Create(&children).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.Group{
		Name:          "aggregate-entry",
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

	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatal(err)
	}
	keys := []models.APIKey{
		{GroupID: children[0].ID, KeyValue: keyA, KeyHash: encryptionSvc.Hash(keyA), Status: models.KeyStatusActive},
		{GroupID: children[1].ID, KeyValue: keyB, KeyHash: encryptionSvc.Hash(keyB), Status: models.KeyStatusActive},
	}
	if err := database.Create(&keys).Error; err != nil {
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

	harness.proxyURL = proxyHTTPServer.URL + "/proxy/" + parent.Name
	harness.client = proxyHTTPServer.Client()
	harness.childA = children[0]
	harness.keyA = keys[0]
	harness.keyProvider = keyProvider
	harness.database = database
	harness.encryption = encryptionSvc
	harness.affinity = affinityManager
	return harness
}

func (h *aggregateRetryHarness) request(t *testing.T) (int, string) {
	return h.requestPath(t, "/work", nil)
}

func (h *aggregateRetryHarness) requestPath(t *testing.T, path string, headers http.Header) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.proxyURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.proxyKey)
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
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

func TestAggregatePreservesLegacyKeyAffinity(t *testing.T) {
	harness := newAggregateRetryHarness(t)
	const affinityKey = "aggregate-upstream-a-2"
	secondA := models.APIKey{
		GroupID:  harness.childA.ID,
		KeyValue: affinityKey,
		KeyHash:  harness.encryption.Hash(affinityKey),
		Status:   models.KeyStatusActive,
	}
	if err := harness.database.Create(&secondA).Error; err != nil {
		t.Fatal(err)
	}
	if err := harness.keyProvider.LoadKeysFromDB(); err != nil {
		t.Fatal(err)
	}
	if err := harness.affinity.SetMapping(harness.childA.ID, keypool.ComputeAffinityHash("stable-user"), secondA.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	status, body := harness.requestPath(t, "/affinity", http.Header{"X-Legacy-Session": []string{"stable-user"}})
	if status != http.StatusOK || !strings.Contains(body, "served-by-a") || harness.aCalls.Load() != 1 || harness.bCalls.Load() != 0 {
		t.Fatalf("legacy aggregate affinity status=%d body=%q A=%d B=%d", status, body, harness.aCalls.Load(), harness.bCalls.Load())
	}
	if credential, _ := harness.aCredential.Load().(string); credential != affinityKey {
		t.Fatalf("aggregate affinity selected credential %q, want mapped key", credential)
	}
}

func TestAggregateRetrySelectsCompleteUsableTuple(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cooldownA  bool
		wantACalls int64
	}{
		{name: "retry excludes attempted key and crosses sibling", wantACalls: 1},
		{name: "initial selection skips cooling child and crosses sibling", cooldownA: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := newAggregateRetryHarness(t)
			if tc.cooldownA {
				if err := harness.keyProvider.ApplyHealthAction(&harness.keyA, &harness.childA, errorpolicy.HealthActionCooldown, time.Minute); err != nil {
					t.Fatal(err)
				}
			}

			status, body := harness.request(t)
			if status != http.StatusOK || harness.aCalls.Load() != tc.wantACalls || harness.bCalls.Load() != 1 {
				t.Fatalf("status=%d body=%q A=%d B=%d; want status=200 A=%d B=1", status, body, harness.aCalls.Load(), harness.bCalls.Load(), tc.wantACalls)
			}
			if !strings.Contains(body, "served-by-b") {
				t.Fatalf("body=%q, want sibling response", body)
			}
			if credential, _ := harness.bCredential.Load().(string); credential != "aggregate-upstream-b" {
				t.Fatalf("B received credential %q", credential)
			}
		})
	}
}
