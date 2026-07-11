package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"gpt-load/internal/utils"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newRequestLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}, &models.GroupHourlyStat{}); err != nil {
		t.Fatalf("migrate request logs: %v", err)
	}
	return database
}

func TestRecordReplacesSuppliedCredentialWithFingerprint(t *testing.T) {
	database := newRequestLogTestDB(t)
	memoryStore := store.NewMemoryStore()
	t.Cleanup(func() { _ = memoryStore.Close() })
	service := NewRequestLogService(database, memoryStore, config.NewSystemSettingsManager())

	const secret = "sk-record-secret"
	const keyHash = "abcdef0123456789abcdef0123456789"
	upstreamSecrets := []string{
		"record-userinfo-secret",
		"record-password-secret",
		"record-query-key-secret",
		"record-query-api-key-secret",
		"record-query-token-secret",
		"record-query-access-token-secret",
		"record-query-authorization-secret",
	}
	logEntry := &models.RequestLog{
		KeyValue: secret,
		KeyHash:  keyHash,
		UpstreamAddr: "https://" + upstreamSecrets[0] + ":" + upstreamSecrets[1] + "@upstream.example.test/v1" +
			"?key=" + upstreamSecrets[2] +
			"&api_key=" + upstreamSecrets[3] +
			"&token=" + upstreamSecrets[4] +
			"&access_token=" + upstreamSecrets[5] +
			"&authorization=" + upstreamSecrets[6] +
			"&model=gpt-4",
	}
	if err := service.Record(logEntry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	storedJSON, err := memoryStore.Get(RequestLogCachePrefix + logEntry.ID)
	if err != nil {
		t.Fatalf("load cached request log: %v", err)
	}
	if strings.Contains(string(storedJSON), secret) {
		t.Fatalf("cached request log leaked credential: %s", storedJSON)
	}
	for _, upstreamSecret := range upstreamSecrets {
		if strings.Contains(string(storedJSON), upstreamSecret) {
			t.Fatalf("cached request log leaked upstream URL credential %q: %s", upstreamSecret, storedJSON)
		}
	}
	var cached models.RequestLog
	if err := json.Unmarshal(storedJSON, &cached); err != nil {
		t.Fatalf("decode cached request log: %v", err)
	}
	if got, want := cached.KeyValue, utils.KeyFingerprint(keyHash); got != want {
		t.Fatalf("cached identifier = %q, want %q", got, want)
	}
	parsedUpstream, err := url.Parse(cached.UpstreamAddr)
	if err != nil {
		t.Fatalf("parse cached upstream address: %v", err)
	}
	if parsedUpstream.User != nil {
		t.Fatalf("cached upstream address retained userinfo: %q", cached.UpstreamAddr)
	}
	if got := parsedUpstream.Query().Get("model"); got != "gpt-4" {
		t.Fatalf("cached upstream safe query = %q, want gpt-4", got)
	}
}

func TestWriteLogsToDBSanitizesLegacyPendingEntry(t *testing.T) {
	database := newRequestLogTestDB(t)
	service := &RequestLogService{db: database}
	const keySecret = "legacy-pending-key"
	const querySecret = "legacy-query-secret"
	const bodySecret = "legacy-body-secret"
	upstreamSecrets := []string{
		"legacy-upstream-user-secret",
		"legacy-upstream-password-secret",
		"legacy-upstream-key-secret",
		"legacy-upstream-api-key-secret",
		"legacy-upstream-token-secret",
		"legacy-upstream-access-token-secret",
		"legacy-upstream-authorization-secret",
	}
	const keyHash = "1234567890abcdef1234567890abcdef"
	logEntry := &models.RequestLog{
		ID:           "legacy-pending",
		KeyValue:     keySecret,
		KeyHash:      keyHash,
		RequestPath:  "/proxy/test?api_key=" + querySecret + "&model=gpt-4",
		ErrorMessage: "upstream failed: token=" + querySecret,
		RequestBody:  `{"client_secret":"` + bodySecret + `","model":"gpt-4"}`,
		UpstreamAddr: "https://" + upstreamSecrets[0] + ":" + upstreamSecrets[1] + "@upstream.example.test/v1" +
			"?key=" + upstreamSecrets[2] +
			"&api_key=" + upstreamSecrets[3] +
			"&token=" + upstreamSecrets[4] +
			"&access_token=" + upstreamSecrets[5] +
			"&authorization=" + upstreamSecrets[6] +
			"&region=us-east-1",
	}
	if err := service.writeLogsToDB([]*models.RequestLog{logEntry}); err != nil {
		t.Fatalf("writeLogsToDB: %v", err)
	}

	var stored models.RequestLog
	if err := database.First(&stored, "id = ?", logEntry.ID).Error; err != nil {
		t.Fatalf("load stored request log: %v", err)
	}
	rendered, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal stored request log: %v", err)
	}
	for _, secret := range append([]string{keySecret, querySecret, bodySecret}, upstreamSecrets...) {
		if strings.Contains(string(rendered), secret) {
			t.Fatalf("stored request log leaked %q: %s", secret, rendered)
		}
	}
	if got, want := stored.KeyValue, utils.KeyFingerprint(keyHash); got != want {
		t.Fatalf("stored key identifier = %q, want %q", got, want)
	}
	if !strings.Contains(stored.RequestPath, "model=gpt-4") {
		t.Fatalf("sanitization removed non-sensitive request context: %q", stored.RequestPath)
	}
	parsedUpstream, err := url.Parse(stored.UpstreamAddr)
	if err != nil {
		t.Fatalf("parse stored upstream address: %v", err)
	}
	if parsedUpstream.User != nil {
		t.Fatalf("stored upstream address retained userinfo: %q", stored.UpstreamAddr)
	}
	if got := parsedUpstream.Query().Get("region"); got != "us-east-1" {
		t.Fatalf("stored upstream safe query = %q, want us-east-1", got)
	}
}

func TestPurgeHistoricalKeyValuesRemovesReversibleCredentials(t *testing.T) {
	database := newRequestLogTestDB(t)
	const secret = "sk-historical-plaintext-secret"
	logEntry := models.RequestLog{ID: "historical", KeyValue: secret, KeyHash: strings.Repeat("a", 64)}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("insert historical log: %v", err)
	}

	service := &RequestLogService{db: database, cleanupBatchDelay: -1}
	if _, err := service.purgeHistoricalKeyValues(context.Background()); err != nil {
		t.Fatalf("purgeHistoricalKeyValues: %v", err)
	}

	var stored models.RequestLog
	if err := database.First(&stored, "id = ?", logEntry.ID).Error; err != nil {
		t.Fatalf("load purged log: %v", err)
	}
	if stored.KeyValue != "" {
		t.Fatalf("historical key value still persisted: %q", stored.KeyValue)
	}
	if stored.KeyHash != logEntry.KeyHash {
		t.Fatalf("key hash changed: %q", stored.KeyHash)
	}
}

func TestStreamLogKeysToCSVExportsFingerprintOnly(t *testing.T) {
	database := newRequestLogTestDB(t)
	const secret = "sk-csv-secret"
	upstreamSecrets := []string{
		"csv-upstream-user-secret",
		"csv-upstream-password-secret",
		"csv-upstream-key-secret",
		"csv-upstream-api-key-secret",
		"csv-upstream-token-secret",
		"csv-upstream-access-token-secret",
		"csv-upstream-authorization-secret",
	}
	const keyHash = "0123456789abcdef0123456789abcdef"
	logEntry := models.RequestLog{
		ID:         "csv-log",
		KeyValue:   secret,
		KeyHash:    keyHash,
		GroupName:  "primary",
		StatusCode: 200,
		UpstreamAddr: "https://" + upstreamSecrets[0] + ":" + upstreamSecrets[1] + "@upstream.example.test/v1" +
			"?key=" + upstreamSecrets[2] +
			"&api_key=" + upstreamSecrets[3] +
			"&token=" + upstreamSecrets[4] +
			"&access_token=" + upstreamSecrets[5] +
			"&authorization=" + upstreamSecrets[6],
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	service := NewLogService(database, enc)
	var output bytes.Buffer
	if err := service.StreamLogKeysToCSV(LogFilter{}, &output); err != nil {
		t.Fatalf("StreamLogKeysToCSV: %v", err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("CSV leaked upstream key: %s", output.String())
	}
	for _, upstreamSecret := range upstreamSecrets {
		if strings.Contains(output.String(), upstreamSecret) {
			t.Fatalf("CSV leaked upstream URL credential %q: %s", upstreamSecret, output.String())
		}
	}

	records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV rows = %d, want 2: %q", len(records), output.String())
	}
	if got, want := records[0][0], "key_identifier"; got != want {
		t.Fatalf("CSV header = %q, want %q", got, want)
	}
	if got, want := records[1][0], utils.KeyFingerprint(keyHash); got != want {
		t.Fatalf("CSV identifier = %q, want %q", got, want)
	}
}

func TestLogFilterMatchesFullKeyOrFingerprint(t *testing.T) {
	database := newRequestLogTestDB(t)
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	const fullKey = "sk-searchable-secret"
	keyHash := enc.Hash(fullKey)
	if err := database.Create(&models.RequestLog{ID: "search-log", KeyHash: keyHash}).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}
	service := NewLogService(database, enc)

	for _, searchValue := range []string{fullKey, utils.KeyFingerprint(keyHash)} {
		var count int64
		if err := service.GetLogsQuery(LogFilter{KeyValue: searchValue}).Count(&count).Error; err != nil {
			t.Fatalf("count logs for %q: %v", searchValue, err)
		}
		if count != 1 {
			t.Fatalf("log count for %q = %d, want 1", searchValue, count)
		}
	}
}
