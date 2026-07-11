package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// TestProxyAffinityEndToEnd exercises the smallest stable production boundary
// that still proves the complete affinity path: a real HTTP request enters a
// Gin proxy route, passes proxy authentication, is assigned by the real
// KeyProvider backed by SQLite and MemoryStore, reaches a real HTTP upstream,
// and persists the successful affinity mapping in the Store.
func TestProxyAffinityEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		groupName = "affinity-e2e"
		proxyKey  = "proxy-e2e-secret"
		keyAlpha  = "upstream-key-alpha"
		keyBeta   = "upstream-key-beta"
	)

	var (
		upstreamMu     sync.Mutex
		upstreamCounts = map[string]int{}
	)
	upstreamSlotByKey := map[string]string{keyAlpha: "alpha", keyBeta: "beta"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selectedKey := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		selectedSlot, found := upstreamSlotByKey[selectedKey]
		if !found {
			http.Error(w, "unexpected upstream credential", http.StatusUnauthorized)
			return
		}

		upstreamMu.Lock()
		upstreamCounts[selectedKey]++
		upstreamMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"selected_slot": selectedSlot})
	}))
	t.Cleanup(upstream.Close)

	database, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "affinity-e2e.db")),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("access SQLite connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.Group{}, &models.APIKey{}, &models.GroupSubGroup{}); err != nil {
		t.Fatalf("migrate SQLite database: %v", err)
	}

	affinityRules, err := json.Marshal([]models.AffinityRule{
		{
			Name:  "session-header",
			Match: models.AffinityMatchRule{PathRegex: `^/proxy/affinity-e2e/v1/chat/completions$`},
			KeySource: models.AffinityKeySource{
				Type: "header",
				Key:  "X-Session-ID",
			},
			TTL: 300,
		},
	})
	if err != nil {
		t.Fatalf("marshal affinity rules: %v", err)
	}
	upstreams, err := json.Marshal([]map[string]any{{"url": upstream.URL, "weight": 1}})
	if err != nil {
		t.Fatalf("marshal upstreams: %v", err)
	}

	group := models.Group{
		Name:               groupName,
		DisplayName:        "Affinity E2E",
		ProxyKeys:          proxyKey,
		GroupType:          "standard",
		Upstreams:          datatypes.JSON(upstreams),
		ValidationEndpoint: "/v1/chat/completions",
		ChannelType:        "openai",
		TestModel:          "e2e-model",
		AffinityRules:      datatypes.JSON(affinityRules),
		Config: datatypes.JSONMap{
			"request_timeout":          5,
			"connect_timeout":          5,
			"response_header_timeout":  5,
			"max_retries":              0,
			"key_affinity_default_ttl": 300,
		},
	}
	if err := database.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	keys := []models.APIKey{
		{GroupID: group.ID, KeyValue: keyAlpha, KeyHash: encryptionSvc.Hash(keyAlpha), Status: models.KeyStatusActive},
		{GroupID: group.ID, KeyValue: keyBeta, KeyHash: encryptionSvc.Hash(keyBeta), Status: models.KeyStatusActive},
	}
	if err := database.Create(&keys).Error; err != nil {
		t.Fatalf("create API keys: %v", err)
	}
	keyIDBySlot := map[string]uint{"alpha": keys[0].ID, "beta": keys[1].ID}

	memoryStore := store.NewMemoryStore()
	t.Cleanup(func() { _ = memoryStore.Close() })
	affinityManager := keypool.NewAffinityManager(memoryStore)
	settingsManager := config.NewSystemSettingsManager()
	subGroupManager := services.NewSubGroupManager(memoryStore)
	groupManager := services.NewGroupManager(database, memoryStore, settingsManager, subGroupManager)
	if err := groupManager.Initialize(); err != nil {
		t.Fatalf("initialize group manager: %v", err)
	}
	t.Cleanup(func() { groupManager.Stop(context.Background()) })

	keyProvider := keypool.NewProvider(database, memoryStore, settingsManager, encryptionSvc, affinityManager)
	if err := keyProvider.LoadKeysFromDB(); err != nil {
		t.Fatalf("load keys into key pool: %v", err)
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
		t.Fatalf("create proxy server: %v", err)
	}

	router := gin.New()
	router.Any(
		"/proxy/:group_name/*path",
		middleware.ProxyAuth(groupManager),
		proxyServer.HandleProxy,
	)
	proxyHTTPServer := httptest.NewServer(router)
	t.Cleanup(proxyHTTPServer.Close)

	request := func(sessionID string) string {
		t.Helper()
		body := []byte(`{"model":"e2e-model","messages":[{"role":"user","content":"affinity check"}]}`)
		req, err := http.NewRequest(
			http.MethodPost,
			proxyHTTPServer.URL+"/proxy/"+groupName+"/v1/chat/completions",
			bytes.NewReader(body),
		)
		if err != nil {
			t.Fatalf("create proxy request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+proxyKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Session-ID", sessionID)

		resp, err := proxyHTTPServer.Client().Do(req)
		if err != nil {
			t.Fatalf("send proxy request for session %q: %v", sessionID, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("proxy status for session %q = %d, want %d", sessionID, resp.StatusCode, http.StatusOK)
		}

		var responseBody struct {
			SelectedSlot string `json:"selected_slot"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
			t.Fatalf("decode proxy response for session %q: %v", sessionID, err)
		}
		return responseBody.SelectedSlot
	}

	const (
		sessionA = "session-a"
		sessionB = "session-b"
	)
	selectedAFirst := request(sessionA)
	waitForAffinityMapping(t, affinityManager, group.ID, sessionA, keyIDBySlot[selectedAFirst])
	selectedASecond := request(sessionA)
	if selectedASecond != selectedAFirst {
		t.Fatalf("session A changed key: first = %q, second = %q", selectedAFirst, selectedASecond)
	}

	selectedBFirst := request(sessionB)
	if selectedBFirst == selectedAFirst {
		t.Fatalf("independent session did not advance round-robin: A = %q, B = %q", selectedAFirst, selectedBFirst)
	}
	waitForAffinityMapping(t, affinityManager, group.ID, sessionB, keyIDBySlot[selectedBFirst])
	selectedBSecond := request(sessionB)
	if selectedBSecond != selectedBFirst {
		t.Fatalf("session B changed key: first = %q, second = %q", selectedBFirst, selectedBSecond)
	}

	assertAffinityMapping(t, affinityManager, group.ID, sessionA, keyIDBySlot[selectedAFirst])
	assertAffinityMapping(t, affinityManager, group.ID, sessionB, keyIDBySlot[selectedBFirst])

	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	if upstreamCounts[keyAlpha] != 2 || upstreamCounts[keyBeta] != 2 {
		t.Fatalf("upstream request counts = %#v, want each selected key used twice", upstreamCounts)
	}
}

func waitForAffinityMapping(
	t *testing.T,
	affinityManager *keypool.AffinityManager,
	groupID uint,
	affinityValue string,
	wantKeyID uint,
) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mapped, err := affinityManager.GetMapping(groupID, keypool.ComputeAffinityHash(affinityValue))
		if err != nil {
			t.Fatalf("read affinity mapping for %q: %v", affinityValue, err)
		}
		if mapped == strconv.FormatUint(uint64(wantKeyID), 10) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	assertAffinityMapping(t, affinityManager, groupID, affinityValue, wantKeyID)
}

func assertAffinityMapping(
	t *testing.T,
	affinityManager *keypool.AffinityManager,
	groupID uint,
	affinityValue string,
	wantKeyID uint,
) {
	t.Helper()
	mapped, err := affinityManager.GetMapping(groupID, keypool.ComputeAffinityHash(affinityValue))
	if err != nil {
		t.Fatalf("read affinity mapping for %q: %v", affinityValue, err)
	}
	want := strconv.FormatUint(uint64(wantKeyID), 10)
	if mapped != want {
		t.Fatalf("affinity mapping for %q = %q, want key ID %q", affinityValue, mapped, want)
	}
}
