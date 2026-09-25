package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
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

func TestSelectedKeyUsesFingerprintInForwardProxyAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		groupName     = "forward-affinity"
		proxyKey      = "local-proxy-key"
		upstreamKey   = "sk-private-upstream-secret"
		proxyPassword = "private-proxy-password"
	)

	var proxyAuth string
	forwardProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodConnect {
			t.Errorf("proxy method = %q, want CONNECT", req.Method)
		}
		proxyAuth = req.Header.Get("Proxy-Authorization")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer forwardProxy.Close()

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "forward-affinity.db")), &gorm.Config{})
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

	upstreams, _ := json.Marshal([]map[string]any{{"url": "https://upstream.example.test", "weight": 1}})
	group := models.Group{
		Name:        groupName,
		ProxyKeys:   proxyKey,
		GroupType:   "standard",
		Upstreams:   datatypes.JSON(upstreams),
		ChannelType: "openai",
		TestModel:   "test-model",
		Config: datatypes.JSONMap{
			"proxy_url":               "http://Default.${API_KEY_FINGERPRINT}:" + proxyPassword + "@" + strings.TrimPrefix(forwardProxy.URL, "http://"),
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
	key := models.APIKey{GroupID: group.ID, KeyValue: upstreamKey, KeyHash: encryptionSvc.Hash(upstreamKey), Status: models.KeyStatusActive}
	if err := database.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	memoryStore := store.NewMemoryStore()
	t.Cleanup(func() { _ = memoryStore.Close() })
	affinityManager := keypool.NewAffinityManager(memoryStore)
	settingsManager := config.NewSystemSettingsManager()
	subGroupManager := services.NewSubGroupManager(memoryStore)
	groupManager := services.NewGroupManager(database, memoryStore, settingsManager, subGroupManager)
	if err := groupManager.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { groupManager.Stop(context.Background()) })
	keyProvider := keypool.NewProvider(database, memoryStore, settingsManager, encryptionSvc, affinityManager)
	if err := keyProvider.LoadKeysFromDB(); err != nil {
		t.Fatal(err)
	}
	proxyServer, err := NewProxyServer(keyProvider, groupManager, subGroupManager, settingsManager, channel.NewFactory(settingsManager, httpclient.NewHTTPClientManager()), nil, encryptionSvc, affinityManager)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Any("/proxy/:group_name/*path", middleware.ProxyAuth(groupManager), proxyServer.HandleProxy)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/proxy/"+groupName+"/v1/chat/completions", strings.NewReader(`{"model":"test-model","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(middleware.ProxyKeyHeader, proxyKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), upstreamKey) || strings.Contains(string(body), proxyPassword) {
		t.Fatal("credential leaked in proxy error response")
	}

	encoded := strings.TrimPrefix(proxyAuth, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	want := "Default.key-id-" + strconv.FormatUint(uint64(key.ID), 10) + ":" + proxyPassword
	if string(decoded) != want {
		t.Fatal("proxy authentication did not use the selected key fingerprint")
	}
	if strings.Contains(string(decoded), upstreamKey) {
		t.Fatal("upstream key leaked in proxy authentication")
	}
}
