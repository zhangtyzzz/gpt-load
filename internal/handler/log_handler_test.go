package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/i18n"
	"gpt-load/internal/middleware"
	"gpt-load/internal/models"
	"gpt-load/internal/services"
	"gpt-load/internal/types"
	"gpt-load/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestGetLogsReturnsFingerprintForHistoricalCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	const secret = "sk-api-historical-secret"
	const requestPathSecret = "sk-historical-path-secret"
	const errorSecret = "sk-historical-error-secret"
	const requestBodySecret = "sk-historical-body-secret"
	upstreamSecrets := []string{
		"historical-upstream-user-secret",
		"historical-upstream-password-secret",
		"historical-upstream-key-secret",
		"historical-upstream-api-key-secret",
		"historical-upstream-token-secret",
		"historical-upstream-access-token-secret",
		"historical-upstream-authorization-secret",
	}
	const keyHash = "fedcba9876543210fedcba9876543210"
	if err := database.Create(&models.RequestLog{
		ID:           "api-log",
		KeyValue:     secret,
		KeyHash:      keyHash,
		RequestPath:  "/proxy/demo?model=gpt-4&api_key=" + requestPathSecret,
		ErrorMessage: "upstream rejected token=" + errorSecret,
		RequestBody:  `{"model":"gpt-4","client_secret":"` + requestBodySecret + `"}`,
		UpstreamAddr: "https://" + upstreamSecrets[0] + ":" + upstreamSecrets[1] + "@upstream.example/v1" +
			"?key=" + upstreamSecrets[2] +
			"&api_key=" + upstreamSecrets[3] +
			"&token=" + upstreamSecrets[4] +
			"&access_token=" + upstreamSecrets[5] +
			"&authorization=" + upstreamSecrets[6] +
			"&region=us-east-1",
	}).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	server := &Server{LogService: services.NewLogService(database, enc), EncryptionSvc: enc}
	router := gin.New()
	router.GET("/logs", server.GetLogs)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	historicalSecrets := []string{
		secret,
		requestPathSecret,
		errorSecret,
		requestBodySecret,
	}
	historicalSecrets = append(historicalSecrets, upstreamSecrets...)
	for _, historicalSecret := range historicalSecrets {
		if strings.Contains(recorder.Body.String(), historicalSecret) {
			t.Fatalf("log API leaked historical credential %q: %s", historicalSecret, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), "region=us-east-1") {
		t.Fatalf("log API removed non-sensitive upstream query context: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"key_hash"`) || strings.Contains(recorder.Body.String(), keyHash) {
		t.Fatalf("log API leaked internal key hash: %s", recorder.Body.String())
	}
	if fingerprint := utils.KeyFingerprint(keyHash); !strings.Contains(recorder.Body.String(), fingerprint) {
		t.Fatalf("log API omitted fingerprint %q: %s", fingerprint, recorder.Body.String())
	}
}

func TestSearchLogsAcceptsCompleteKeyOnlyInJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	const secret = "sk-post-only-search-secret"
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	keyHash := enc.Hash(secret)
	if err := database.Create(&models.RequestLog{ID: "post-search", KeyHash: keyHash}).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	server := &Server{LogService: services.NewLogService(database, enc)}
	router := gin.New()
	router.POST("/logs/search", server.SearchLogs)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/logs/search", strings.NewReader(`{"key_value":"`+secret+`"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) || strings.Contains(recorder.Body.String(), keyHash) {
		t.Fatalf("POST log search leaked key material: %s", recorder.Body.String())
	}
	if fingerprint := utils.KeyFingerprint(keyHash); !strings.Contains(recorder.Body.String(), fingerprint) {
		t.Fatalf("POST log search omitted fingerprint %q: %s", fingerprint, recorder.Body.String())
	}
}

func TestGetLogsRejectsCompleteKeyWithoutLoggingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}
	const secret = "sk-never-in-get-filter"

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	var logs bytes.Buffer
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(previousOutput) })

	server := &Server{LogService: services.NewLogService(database, enc)}
	router := gin.New()
	router.Use(middleware.Logger(types.LogConfig{}))
	router.GET("/logs", server.GetLogs)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs?key_value="+url.QueryEscape(secret), nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) || strings.Contains(logs.String(), secret) {
		t.Fatalf("GET key filter leaked credential; response=%s logs=%s", recorder.Body.String(), logs.String())
	}
}

func TestGetLogsCompatibilityEndpointAcceptsFingerprint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	const keyHash = "abcdef0123459999999999999999999999999999999999999999999999999999"
	if err := database.Create(&models.RequestLog{ID: "fingerprint-search", KeyHash: keyHash}).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	server := &Server{LogService: services.NewLogService(database, enc)}
	router := gin.New()
	router.GET("/logs", server.GetLogs)
	recorder := httptest.NewRecorder()
	fingerprint := utils.KeyFingerprint(keyHash)
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs?key_value="+url.QueryEscape(fingerprint), nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), fingerprint) {
		t.Fatalf("GET fingerprint filter did not return matching log: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), keyHash) || strings.Contains(recorder.Body.String(), `"key_hash"`) {
		t.Fatalf("GET fingerprint response leaked key hash: %s", recorder.Body.String())
	}
}
