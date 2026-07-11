package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/i18n"
	"gpt-load/internal/models"
	"gpt-load/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func newKeySearchTestServer(t *testing.T, secret string) (*Server, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.Group{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	group := models.Group{
		Name:        "secure-search",
		DisplayName: "Secure search",
		ChannelType: "openai",
		TestModel:   "test-model",
		Upstreams:   datatypes.JSON([]byte("[]")),
	}
	if err := database.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	key := models.APIKey{
		GroupID:  group.ID,
		KeyValue: secret,
		KeyHash:  encryptionSvc.Hash(secret),
		Status:   models.KeyStatusActive,
	}
	if err := database.Create(&key).Error; err != nil {
		t.Fatalf("create API key: %v", err)
	}

	return &Server{
		DB:            database,
		KeyService:    services.NewKeyService(database, nil, nil, encryptionSvc),
		EncryptionSvc: encryptionSvc,
	}, group.ID
}

func TestSearchKeysInGroupKeepsFullKeyOutOfURL(t *testing.T) {
	const secret = "sk-upstream-search-secret"
	server, groupID := newKeySearchTestServer(t, secret)
	router := gin.New()
	router.POST("/keys/search", server.SearchKeysInGroup)

	body, err := json.Marshal(SearchKeysRequest{
		GroupID:  groupID,
		Status:   models.KeyStatusActive,
		KeyValue: secret,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/keys/search?page=1&page_size=12", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	if strings.Contains(request.URL.String(), secret) {
		t.Fatalf("search credential leaked into URL: %s", request.URL.String())
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("exact search did not return matching key: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"key_hash"`) {
		t.Fatalf("exact search exposed internal key hash: %s", recorder.Body.String())
	}
}

func TestListKeysInGroupRejectsFullKeyQuery(t *testing.T) {
	const secret = "sk-upstream-query-secret"
	server, groupID := newKeySearchTestServer(t, secret)
	router := gin.New()
	router.GET("/keys", server.ListKeysInGroup)

	path := "/keys?group_id=" + url.QueryEscape(strconv.FormatUint(uint64(groupID), 10)) +
		"&key_value=" + url.QueryEscape(secret)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("rejection response leaked credential: %s", recorder.Body.String())
	}
}

func TestListKeysInGroupStillSupportsNonSensitiveFilters(t *testing.T) {
	const secret = "sk-upstream-list-secret"
	server, groupID := newKeySearchTestServer(t, secret)
	router := gin.New()
	router.GET("/keys", server.ListKeysInGroup)

	path := "/keys?group_id=" + strconv.FormatUint(uint64(groupID), 10) +
		"&status=active&page=1&page_size=12"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if strings.Contains(request.URL.String(), secret) {
		t.Fatalf("credential leaked into list URL: %s", request.URL.String())
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("ordinary filtered list omitted key: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"key_hash"`) {
		t.Fatalf("ordinary key list exposed internal key hash: %s", recorder.Body.String())
	}
}
