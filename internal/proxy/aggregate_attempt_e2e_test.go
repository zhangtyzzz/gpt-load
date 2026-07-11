package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/errorpolicy"
	app_errors "gpt-load/internal/errors"
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

type legacyAggregateObservation struct {
	mu              sync.Mutex
	calls           int
	credential      string
	streamBuffering string
	body            map[string]any
}

func (o *legacyAggregateObservation) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	decoded := make(map[string]any)
	_ = json.Unmarshal(body, &decoded)

	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	o.credential = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	o.streamBuffering = r.Header.Get("X-Accel-Buffering")
	o.body = decoded
}

func (o *legacyAggregateObservation) snapshot() (int, string, string, map[string]any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	body := make(map[string]any, len(o.body))
	for key, value := range o.body {
		body[key] = value
	}
	return o.calls, o.credential, o.streamBuffering, body
}

type legacyAggregateHarness struct {
	proxyURL       string
	client         *http.Client
	proxyKey       string
	proxyServer    *ProxyServer
	groupManager   *services.GroupManager
	channelFactory *channel.Factory
	keyProvider    *keypool.KeyProvider
	affinity       *keypool.AffinityManager
	parent         *models.Group
	childA         *models.Group
	childB         *models.Group
	keyA           models.APIKey
	keyB           models.APIKey
	keyB2          models.APIKey
	observationA   *legacyAggregateObservation
	observationB   *legacyAggregateObservation
}

func newLegacyAggregateHarness(t *testing.T) *legacyAggregateHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const (
		proxyKey = "legacy-aggregate-proxy-key"
		keyA     = "legacy-upstream-a"
		keyB     = "legacy-upstream-b"
		keyB2    = "legacy-upstream-b-2"
	)
	observationA := &legacyAggregateObservation{}
	observationB := &legacyAggregateObservation{}
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observationA.record(r)
		_, credential, _, _ := observationA.snapshot()
		if credential != keyA {
			http.Error(w, "unexpected A credential", http.StatusUnauthorized)
			return
		}
		http.Error(w, "retry on sibling", http.StatusInternalServerError)
	}))
	t.Cleanup(upstreamA.Close)
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observationB.record(r)
		_, credential, _, _ := observationB.snapshot()
		if credential != keyB && credential != keyB2 {
			http.Error(w, "unexpected B credential", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"served_by":"b"}`)
	}))
	t.Cleanup(upstreamB.Close)

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-aggregate.db")), &gorm.Config{})
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

	upstreams := func(rawURL string) datatypes.JSON {
		encoded, marshalErr := json.Marshal([]map[string]any{{"url": rawURL, "weight": 1}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return encoded
	}
	affinityRules, err := json.Marshal([]models.AffinityRule{{
		Name:      "attempt-local-body-affinity",
		Match:     models.AffinityMatchRule{PathRegex: `^/proxy/legacy-aggregate/v1/chat/completions$`},
		KeySource: models.AffinityKeySource{Type: "body_json", Path: "affinity_id"},
		TTL:       3600,
	}})
	if err != nil {
		t.Fatal(err)
	}
	groupConfig := datatypes.JSONMap{
		"request_timeout":          5,
		"connect_timeout":          5,
		"response_header_timeout":  5,
		"max_retries":              1,
		"blacklist_threshold":      100,
		"key_affinity_default_ttl": 3600,
	}
	children := []models.Group{
		{
			Name:          "legacy-aggregate-a",
			GroupType:     "standard",
			ChannelType:   "openai",
			Upstreams:     upstreams(upstreamA.URL),
			TestModel:     "raw-model",
			Config:        groupConfig,
			AffinityRules: affinityRules,
			ParamOverrides: datatypes.JSONMap{
				"route":       "a",
				"only_a":      "from-a",
				"stream":      true,
				"affinity_id": "affinity-a",
			},
			ModelRedirectRules: datatypes.JSONMap{"raw-model": "a-model"},
		},
		{
			Name:          "legacy-aggregate-b",
			GroupType:     "standard",
			ChannelType:   "openai",
			Upstreams:     upstreams(upstreamB.URL),
			TestModel:     "raw-model",
			Config:        groupConfig,
			AffinityRules: affinityRules,
			ParamOverrides: datatypes.JSONMap{
				"route":       "b",
				"only_b":      "from-b",
				"stream":      false,
				"affinity_id": "affinity-b",
			},
			ModelRedirectRules: datatypes.JSONMap{"raw-model": "b-model"},
		},
	}
	if err := database.Create(&children).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.Group{
		Name:        "legacy-aggregate",
		ProxyKeys:   proxyKey,
		GroupType:   "aggregate",
		ChannelType: "openai",
		Upstreams:   datatypes.JSON(`[]`),
		TestModel:   "raw-model",
	}
	if err := database.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&[]models.GroupSubGroup{
		{GroupID: parent.ID, SubGroupID: children[0].ID, Weight: 100},
		{GroupID: parent.ID, SubGroupID: children[1].ID, Weight: 1},
	}).Error; err != nil {
		t.Fatal(err)
	}

	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatal(err)
	}
	keys := []models.APIKey{
		{GroupID: children[0].ID, KeyValue: keyA, KeyHash: encryptionSvc.Hash(keyA), Status: models.KeyStatusActive},
		{GroupID: children[1].ID, KeyValue: keyB, KeyHash: encryptionSvc.Hash(keyB), Status: models.KeyStatusActive},
		{GroupID: children[1].ID, KeyValue: keyB2, KeyHash: encryptionSvc.Hash(keyB2), Status: models.KeyStatusActive},
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
	channelFactory := channel.NewFactory(settingsManager, httpclient.NewHTTPClientManager())
	proxyServer, err := NewProxyServer(
		keyProvider,
		groupManager,
		subGroupManager,
		settingsManager,
		channelFactory,
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

	loadedParent, err := groupManager.GetGroupByID(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	loadedA, err := groupManager.GetGroupByID(children[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	loadedB, err := groupManager.GetGroupByID(children[1].ID)
	if err != nil {
		t.Fatal(err)
	}

	return &legacyAggregateHarness{
		proxyURL:       proxyHTTPServer.URL + "/proxy/" + parent.Name,
		client:         proxyHTTPServer.Client(),
		proxyKey:       proxyKey,
		proxyServer:    proxyServer,
		groupManager:   groupManager,
		channelFactory: channelFactory,
		keyProvider:    keyProvider,
		affinity:       affinityManager,
		parent:         loadedParent,
		childA:         loadedA,
		childB:         loadedB,
		keyA:           keys[0],
		keyB:           keys[1],
		keyB2:          keys[2],
		observationA:   observationA,
		observationB:   observationB,
	}
}

func legacyAggregateRawBody() []byte {
	return []byte(`{"model":"raw-model","route":"raw","original":"keep","stream":false,"affinity_id":"raw-affinity"}`)
}

func TestLegacyAggregateRetryRebuildsCompleteAttemptFromRawBody(t *testing.T) {
	harness := newLegacyAggregateHarness(t)
	req, err := http.NewRequest(http.MethodPost, harness.proxyURL+"/v1/chat/completions", bytes.NewReader(legacyAggregateRawBody()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer upstream-business-credential")
	req.Header.Set(middleware.ProxyKeyHeader, harness.proxyKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := harness.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(responseBody), `"served_by":"b"`) {
		t.Fatalf("status=%d body=%q", resp.StatusCode, responseBody)
	}

	aCalls, aCredential, aStream, aBody := harness.observationA.snapshot()
	bCalls, bCredential, bStream, bBody := harness.observationB.snapshot()
	if aCalls != 1 || bCalls != 1 {
		t.Fatalf("attempt counts A=%d B=%d", aCalls, bCalls)
	}
	if aCredential != harness.keyA.KeyValue {
		t.Fatalf("A credential=%q", aCredential)
	}
	if bCredential != harness.keyB.KeyValue && bCredential != harness.keyB2.KeyValue {
		t.Fatalf("B credential=%q", bCredential)
	}
	if aStream != "no" || bStream != "" {
		t.Fatalf("stream policy leaked across attempts: A=%q B=%q", aStream, bStream)
	}
	if aBody["route"] != "a" || aBody["only_a"] != "from-a" || aBody["model"] != "a-model" || aBody["stream"] != true {
		t.Fatalf("A attempt was not prepared from A config: %#v", aBody)
	}
	if bBody["route"] != "b" || bBody["only_b"] != "from-b" || bBody["model"] != "b-model" || bBody["stream"] != false || bBody["original"] != "keep" {
		t.Fatalf("B attempt was not rebuilt from raw body: %#v", bBody)
	}
	if _, leaked := bBody["only_a"]; leaked {
		t.Fatalf("A override leaked into B body: %#v", bBody)
	}

	selectedBKeyID := harness.keyB.ID
	if bCredential == harness.keyB2.KeyValue {
		selectedBKeyID = harness.keyB2.ID
	}
	waitForAffinityMapping(t, harness.affinity, harness.childB.ID, "affinity-b", selectedBKeyID)
	wrongMapping, err := harness.affinity.GetMapping(harness.childB.ID, keypool.ComputeAffinityHash("affinity-a"))
	if err != nil {
		t.Fatal(err)
	}
	if wrongMapping != "" {
		t.Fatalf("retry reused A affinity metadata for B: %q", wrongMapping)
	}
}

func TestLegacyAggregateInitialFallbackUsesFallbackAffinity(t *testing.T) {
	harness := newLegacyAggregateHarness(t)
	if err := harness.keyProvider.ApplyHealthAction(&harness.keyA, harness.childA, errorpolicy.HealthActionCooldown, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := harness.affinity.SetMapping(harness.childB.ID, keypool.ComputeAffinityHash("affinity-b"), harness.keyB2.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/proxy/legacy-aggregate/v1/chat/completions", bytes.NewReader(legacyAggregateRawBody()))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handlerA, err := harness.channelFactory.GetChannel(harness.childA)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := harness.proxyServer.prepareAggregateAttempt(
		ctx,
		harness.parent,
		harness.childA,
		handlerA,
		legacyAggregateRawBody(),
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.group.ID != harness.childB.ID || attempt.apiKey.ID != harness.keyB2.ID {
		t.Fatalf("fallback tuple group=%d key=%d, want group=%d mapped key=%d", attempt.group.ID, attempt.apiKey.ID, harness.childB.ID, harness.keyB2.ID)
	}
	if attempt.isStream || attempt.affinityHash != keypool.ComputeAffinityHash("affinity-b") {
		t.Fatalf("fallback inherited preferred stream/affinity: stream=%v hash=%q", attempt.isStream, attempt.affinityHash)
	}
	var prepared map[string]any
	if err := json.Unmarshal(attempt.body, &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared["route"] != "b" || prepared["only_b"] != "from-b" {
		t.Fatalf("fallback body was not prepared with B config: %#v", prepared)
	}
	if _, leaked := prepared["only_a"]; leaked {
		t.Fatalf("preferred child override leaked into fallback: %#v", prepared)
	}
}

type requestLimitedChannel struct {
	channel.ChannelProxy
	limit int64
}

func (c requestLimitedChannel) MaxRequestBodyBytes() int64 { return c.limit }

func TestPrepareGroupAttemptDistinguishesNoKeyFromBodyLimit(t *testing.T) {
	t.Run("available child enforces its final limit", func(t *testing.T) {
		harness := newLegacyAggregateHarness(t)
		handler, err := harness.channelFactory.GetChannel(harness.childA)
		if err != nil {
			t.Fatal(err)
		}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/proxy/legacy-aggregate/v1/chat/completions", nil)
		_, err = harness.proxyServer.prepareGroupAttempt(ctx, harness.childA, requestLimitedChannel{ChannelProxy: handler, limit: 4}, legacyAggregateRawBody(), 0)
		if !errors.Is(err, errRequestBodyTooLarge) {
			t.Fatalf("error=%v, want request body too large", err)
		}
	})

	t.Run("child without a key reports unavailable before size", func(t *testing.T) {
		harness := newLegacyAggregateHarness(t)
		if err := harness.keyProvider.ApplyHealthAction(&harness.keyA, harness.childA, errorpolicy.HealthActionCooldown, time.Minute); err != nil {
			t.Fatal(err)
		}
		handler, err := harness.channelFactory.GetChannel(harness.childA)
		if err != nil {
			t.Fatal(err)
		}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/proxy/legacy-aggregate/v1/chat/completions", nil)
		_, err = harness.proxyServer.prepareGroupAttempt(ctx, harness.childA, requestLimitedChannel{ChannelProxy: handler, limit: 4}, legacyAggregateRawBody(), 0)
		if !errors.Is(err, app_errors.ErrNoActiveKeys) {
			t.Fatalf("error=%v, want no active keys", err)
		}
	})
}
