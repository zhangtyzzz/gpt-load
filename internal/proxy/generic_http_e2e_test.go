package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

type integrationInfoTrap struct {
	calls atomic.Int64
}

func (h *integrationInfoTrap) GetIntegrationInfo(c *gin.Context) {
	h.calls.Add(1)
	c.JSON(http.StatusTeapot, gin.H{"source": "legacy-interceptor"})
}

func TestGenericHTTPTransparentProxyAndLegacyAffinityEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		groupName = "generic-http-e2e"
		proxyKey  = "proxy-secret"
		keyAlpha  = "vendor-alpha"
		keyBeta   = "vendor-beta"
	)
	var dropCount atomic.Int64
	var failoverMu sync.Mutex
	var failoverKeys []string
	var postFailoverCount atomic.Int64

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selectedKey := strings.TrimPrefix(r.Header.Get("X-Upstream-Key"), "Token ")
		if selectedKey != keyAlpha && selectedKey != keyBeta {
			http.Error(w, "unexpected credential", http.StatusUnauthorized)
			return
		}
		slot := "alpha"
		if selectedKey == keyBeta {
			slot = "beta"
		}

		switch r.URL.Path {
		case "/drop":
			dropCount.Add(1)
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
				return
			}
			conn, _, _ := hijacker.Hijack()
			_ = conn.Close()
			return
		case "/api/integration/info":
			_ = json.NewEncoder(w).Encode(map[string]string{"source": "upstream", "query": r.URL.RawQuery})
			return
		case "/failover":
			failoverMu.Lock()
			failoverKeys = append(failoverKeys, selectedKey)
			attempt := len(failoverKeys)
			failoverMu.Unlock()
			if attempt == 1 {
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, "recovered")
			return
		case "/post-failover":
			postFailoverCount.Add(1)
			http.Error(w, "ambiguous POST failure", http.StatusInternalServerError)
			return
		case "/affinity":
			w.Header().Set("X-Upstream-Slot", slot)
			_, _ = io.WriteString(w, "affinity-ok")
			return
		case "/echo":
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Key-Echo", "selected="+selectedKey)
			w.Header().Add("X-Multi", "one")
			w.Header().Add("X-Multi", "two")
			w.Header().Set("Connection", "X-Hop-Response")
			w.Header().Set("X-Hop-Response", "must-not-forward")
			w.Header().Set("Mcp-Session-Id", "upstream-session")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"method":          r.Method,
				"path":            r.URL.Path,
				"query":           r.URL.RawQuery,
				"body":            string(body),
				"header":          r.Header.Values("X-Multi-Request"),
				"connection":      r.Header.Get("Connection"),
				"hop":             r.Header.Get("X-Hop-Request"),
				"session":         r.Header.Get("Mcp-Session-Id"),
				"authorization":   r.Header.Get("Authorization"),
				"proxy_control":   r.Header.Get(middleware.ProxyKeyHeader),
				"accel_buffering": r.Header.Get("X-Accel-Buffering"),
			})
			return
		}

		if rawStatus := r.URL.Query().Get("status"); rawStatus != "" {
			status, _ := strconv.Atoi(rawStatus)
			w.Header().Add("X-Multi", "one")
			w.Header().Add("X-Multi", "two")
			if status == http.StatusFound {
				w.Header().Set("Location", "/redirect-target")
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":"business response"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "generic-http.db")), &gorm.Config{})
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
	preset.ChannelConfig.Auth.Name = "X-Upstream-Key"
	preset.ChannelConfig.Auth.Prefix = "Token "
	preset.ChannelConfig.Retry.FailoverStatuses = append(preset.ChannelConfig.Retry.FailoverStatuses, http.StatusInternalServerError)
	configJSON, err := json.Marshal(preset.ChannelConfig)
	if err != nil {
		t.Fatal(err)
	}
	upstreams, _ := json.Marshal([]map[string]any{{"url": upstream.URL, "weight": 1}})
	headerRules, _ := json.Marshal([]models.HeaderRule{{Key: "X-Trace", Value: "generic-test", Action: "set"}})
	affinityRules, _ := json.Marshal([]models.AffinityRule{{
		Name:      "generic legacy key affinity",
		Match:     models.AffinityMatchRule{PathRegex: `^/proxy/[^/]+/affinity$`},
		KeySource: models.AffinityKeySource{Type: "header", Key: "X-Legacy-Session"},
		TTL:       3600,
	}})
	group := models.Group{
		Name:          groupName,
		ProxyKeys:     proxyKey,
		GroupType:     "standard",
		Upstreams:     datatypes.JSON(upstreams),
		ChannelType:   channel.GenericHTTPChannelType,
		ChannelConfig: datatypes.JSON(configJSON),
		HeaderRules:   datatypes.JSON(headerRules),
		AffinityRules: datatypes.JSON(affinityRules),
		TestModel:     "-",
		Config: datatypes.JSONMap{
			"request_timeout":         5,
			"connect_timeout":         5,
			"response_header_timeout": 5,
			"max_retries":             2,
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
	keys := []models.APIKey{
		{GroupID: group.ID, KeyValue: keyAlpha, KeyHash: encryptionSvc.Hash(keyAlpha), Status: models.KeyStatusActive},
		{GroupID: group.ID, KeyValue: keyBeta, KeyHash: encryptionSvc.Hash(keyBeta), Status: models.KeyStatusActive},
	}
	if err := database.Create(&keys).Error; err != nil {
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
	integrationTrap := &integrationInfoTrap{}
	router.Any(
		"/proxy/:group_name/*path",
		middleware.ProxyRouteDispatcher(integrationTrap, groupManager),
		middleware.ProxyAuth(groupManager),
		proxyServer.HandleProxy,
	)
	proxyHTTPServer := httptest.NewServer(router)
	t.Cleanup(proxyHTTPServer.Close)
	transparentClient := *proxyHTTPServer.Client()
	transparentClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	baseURL := proxyHTTPServer.URL + "/proxy/" + groupName

	echoReq, _ := http.NewRequest(http.MethodPut, baseURL+"/echo?key=business%2Fvalue&x=1&x=2", strings.NewReader("opaque-body"))
	echoReq.Header.Set("Authorization", "Bearer upstream-business-credential")
	echoReq.Header.Set(middleware.ProxyKeyHeader, proxyKey)
	echoReq.Header.Set("Accept", "text/event-stream")
	echoReq.Header.Add("X-Multi-Request", "alpha")
	echoReq.Header.Add("X-Multi-Request", "beta")
	echoReq.Header.Set("Mcp-Session-Id", "client-session")
	echoReq.Header.Set("Connection", "X-Hop-Request, Keep-Alive")
	echoReq.Header.Set("X-Hop-Request", "must-not-forward")
	echoReq.Header.Set("Keep-Alive", "timeout=10")
	echoResp, err := transparentClient.Do(echoReq)
	if err != nil {
		t.Fatal(err)
	}
	var echo map[string]any
	if err := json.NewDecoder(echoResp.Body).Decode(&echo); err != nil {
		t.Fatal(err)
	}
	echoResp.Body.Close()
	if echo["method"] != http.MethodPut || echo["path"] != "/echo" || echo["query"] != "key=business%2Fvalue&x=1&x=2" || echo["body"] != "opaque-body" {
		t.Fatalf("request was not transparent: %#v", echo)
	}
	if echo["session"] != "client-session" || echoResp.Header.Get("Mcp-Session-Id") != "upstream-session" {
		t.Fatalf("MCP session header was not transparent: request=%v response=%q", echo["session"], echoResp.Header.Get("Mcp-Session-Id"))
	}
	if echo["connection"] != "" || echo["hop"] != "" {
		t.Fatalf("hop-by-hop request fields reached upstream: %#v", echo)
	}
	if echo["authorization"] != "Bearer upstream-business-credential" {
		t.Fatalf("end-to-end Authorization was not preserved: %#v", echo)
	}
	if echo["proxy_control"] != "" {
		t.Fatalf("consumed proxy control header reached upstream: %#v", echo)
	}
	if echo["accel_buffering"] != "" {
		t.Fatalf("Generic proxy synthesized X-Accel-Buffering upstream: %#v", echo)
	}

	staleControlReq, _ := http.NewRequest(http.MethodPut, baseURL+"/echo", strings.NewReader("stale-control"))
	staleControlReq.Header.Set("Authorization", "Bearer "+proxyKey)
	staleControlReq.Header.Set(middleware.ProxyKeyHeader, "stale-control-value")
	staleControlResp, err := transparentClient.Do(staleControlReq)
	if err != nil {
		t.Fatal(err)
	}
	var staleControlEcho map[string]any
	if err := json.NewDecoder(staleControlResp.Body).Decode(&staleControlEcho); err != nil {
		t.Fatal(err)
	}
	staleControlResp.Body.Close()
	if staleControlResp.StatusCode != http.StatusOK || staleControlEcho["proxy_control"] != "" {
		t.Fatalf("unmatched dedicated control header reached upstream: status=%d payload=%#v", staleControlResp.StatusCode, staleControlEcho)
	}
	if got := echoResp.Header.Values("X-Multi"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("multi-value response headers = %#v", got)
	}
	if got := echoResp.Header.Get("X-Key-Echo"); strings.Contains(got, keyAlpha) || strings.Contains(got, keyBeta) || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("successful response header leaked selected key: %q", got)
	}
	if got := echoResp.Header.Get("X-Hop-Response"); got != "" {
		t.Fatalf("dynamic hop-by-hop response header was forwarded: %q", got)
	}

	integrationResp := sendGenericRequest(t, &transparentClient, http.MethodGet, baseURL+"/api/integration/info?key=business", proxyKey, nil, "")
	var integrationPayload map[string]string
	if err := json.NewDecoder(integrationResp.Body).Decode(&integrationPayload); err != nil {
		t.Fatal(err)
	}
	integrationResp.Body.Close()
	if integrationResp.StatusCode != http.StatusOK || integrationPayload["source"] != "upstream" || integrationPayload["query"] != "key=business" || integrationTrap.calls.Load() != 0 {
		t.Fatalf("generic integration path was intercepted: status=%d payload=%#v trap=%d", integrationResp.StatusCode, integrationPayload, integrationTrap.calls.Load())
	}

	for _, status := range []int{http.StatusNotModified, http.StatusFound, http.StatusConflict, http.StatusUnprocessableEntity} {
		resp := sendGenericRequest(t, &transparentClient, http.MethodGet, baseURL+"/status?status="+strconv.Itoa(status), proxyKey, nil, "")
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("transparent status %d became %d", status, resp.StatusCode)
		}
		if got := resp.Header.Values("X-Multi"); len(got) != 2 {
			t.Fatalf("status %d multi headers = %#v", status, got)
		}
		if status == http.StatusFound && resp.Header.Get("Location") != "/redirect-target" {
			t.Fatalf("302 Location = %q", resp.Header.Get("Location"))
		}
	}
	var persistedKeys []models.APIKey
	if err := database.Order("id").Find(&persistedKeys).Error; err != nil {
		t.Fatal(err)
	}
	for _, persisted := range persistedKeys {
		if persisted.Status != models.KeyStatusActive || persisted.FailureCount != 0 {
			t.Fatalf("transparent business statuses punished key %d: status=%s failures=%d", persisted.ID, persisted.Status, persisted.FailureCount)
		}
	}

	dropResp := sendGenericRequest(t, &transparentClient, http.MethodPost, baseURL+"/drop", proxyKey, nil, "unsafe")
	dropResp.Body.Close()
	if dropResp.StatusCode != http.StatusBadGateway || dropCount.Load() != 1 {
		t.Fatalf("unsafe POST transport result status=%d attempts=%d", dropResp.StatusCode, dropCount.Load())
	}

	failoverResp := sendGenericRequest(t, &transparentClient, http.MethodGet, baseURL+"/failover", proxyKey, nil, "")
	failoverResp.Body.Close()
	failoverMu.Lock()
	gotFailoverKeys := append([]string(nil), failoverKeys...)
	failoverMu.Unlock()
	if failoverResp.StatusCode != http.StatusOK || len(gotFailoverKeys) != 2 || gotFailoverKeys[0] == gotFailoverKeys[1] {
		t.Fatalf("standard group failover status=%d keys=%#v", failoverResp.StatusCode, gotFailoverKeys)
	}
	postFailover := sendGenericRequest(t, &transparentClient, http.MethodPost, baseURL+"/post-failover", proxyKey, nil, "opaque")
	postFailover.Body.Close()
	if postFailover.StatusCode != http.StatusInternalServerError || postFailoverCount.Load() != 1 {
		t.Fatalf("unsafe POST failover status=%d attempts=%d", postFailover.StatusCode, postFailoverCount.Load())
	}

	affinityHeaders := http.Header{"X-Legacy-Session": []string{"stable-user"}}
	firstAffinity := sendGenericRequest(t, &transparentClient, http.MethodGet, baseURL+"/affinity", proxyKey, affinityHeaders, "")
	firstSlot := firstAffinity.Header.Get("X-Upstream-Slot")
	firstAffinity.Body.Close()
	secondAffinity := sendGenericRequest(t, &transparentClient, http.MethodGet, baseURL+"/affinity", proxyKey, affinityHeaders, "")
	secondSlot := secondAffinity.Header.Get("X-Upstream-Slot")
	secondAffinity.Body.Close()
	if firstAffinity.StatusCode != http.StatusOK || secondAffinity.StatusCode != http.StatusOK || firstSlot == "" || secondSlot != firstSlot {
		t.Fatalf("legacy affinity on generic HTTP did not retain key: first=%d/%q second=%d/%q", firstAffinity.StatusCode, firstSlot, secondAffinity.StatusCode, secondSlot)
	}
	mappedKeyID, err := affinityManager.GetMapping(group.ID, keypool.ComputeAffinityHash("stable-user"))
	if err != nil || mappedKeyID == "" {
		t.Fatalf("legacy affinity mapping was not stored: key=%q err=%v", mappedKeyID, err)
	}
}

func sendGenericRequest(t *testing.T, client *http.Client, method, requestURL, proxyKey string, headers http.Header, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, requestURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer upstream-business-credential")
	req.Header.Set(middleware.ProxyKeyHeader, proxyKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func findChannelPreset(t *testing.T, id string) channel.ChannelCatalogEntry {
	t.Helper()
	for _, preset := range channel.GetChannelCatalog() {
		if preset.ID == id {
			return preset
		}
	}
	t.Fatalf("channel preset %q not found", id)
	return channel.ChannelCatalogEntry{}
}
