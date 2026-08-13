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

// newKeyIdentifierTestDB builds a database with both tables, which is what the
// mask resolution path needs. newRequestLogTestDB deliberately omits api_keys so
// the older tests continue to exercise the unresolvable fallback.
func newKeyIdentifierTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}, &models.GroupHourlyStat{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate key identifier tables: %v", err)
	}
	return database
}

// seedKeyWithLogs stores a key the way KeyService does — encrypted value plus
// one-way hash — and a matching request-log row.
func seedKeyWithLogs(
	t *testing.T,
	database *gorm.DB,
	enc encryption.Service,
	plaintextKey string,
	logID string,
	statusCode int,
) string {
	t.Helper()

	encrypted, err := enc.Encrypt(plaintextKey)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	keyHash := enc.Hash(plaintextKey)
	if err := database.Create(&models.APIKey{
		KeyValue: encrypted,
		KeyHash:  keyHash,
		GroupID:  1,
		Status:   models.KeyStatusActive,
	}).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	if err := database.Create(&models.RequestLog{
		ID:         logID,
		KeyHash:    keyHash,
		KeyValue:   utils.KeyFingerprint(keyHash),
		GroupName:  "primary",
		StatusCode: statusCode,
		IsSuccess:  statusCode < 400,
	}).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}
	return keyHash
}

func TestResolveKeyIdentifiersMatchKeyManagementMask(t *testing.T) {
	database := newKeyIdentifierTestDB(t)
	enc, err := encryption.NewService("unit-test-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	const keyAlpha = "sk-alpha-abcdefghijklmnop"
	const keyBeta = "sk-beta-qrstuvwxyz012345"
	alphaHash := seedKeyWithLogs(t, database, enc, keyAlpha, "log-alpha", 200)
	betaHash := seedKeyWithLogs(t, database, enc, keyBeta, "log-beta", 400)

	service := NewLogService(database, enc)
	identifiers := service.ResolveKeyIdentifiers([]string{alphaHash, betaHash})

	// The identifier must begin with exactly what the key management screen
	// renders — the mask of the decrypted key — so the two can be matched by eye.
	// The remainder is the discriminator that keeps colliding masks apart.
	for name, expected := range map[string]struct {
		hash string
		key  string
	}{
		"alpha": {alphaHash, keyAlpha},
		"beta":  {betaHash, keyBeta},
	} {
		mask := utils.MaskKeyIdentifier(expected.key)
		identifier := identifiers[expected.hash]
		if !strings.HasPrefix(identifier, mask) {
			t.Errorf("identifier for %s = %q, want it to begin with the key management mask %q",
				name, identifier, mask)
		}
		if got, want := identifier, utils.KeyIdentifier(expected.key, expected.hash); got != want {
			t.Errorf("identifier for %s = %q, want %q", name, got, want)
		}
	}

	if identifiers[alphaHash] == identifiers[betaHash] {
		t.Errorf("distinct keys resolved to the same identifier %q", identifiers[alphaHash])
	}
	for hash, identifier := range identifiers {
		if strings.Contains(identifier, "alpha-abcdefghij") || strings.Contains(identifier, "beta-qrstuvwxyz") {
			t.Errorf("identifier for %s leaked the middle of the key: %q", hash, identifier)
		}
	}
}

func TestResolveKeyIdentifiersOmitUnresolvableHashes(t *testing.T) {
	database := newKeyIdentifierTestDB(t)
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	liveHash := seedKeyWithLogs(t, database, enc, "sk-live-abcdefghijklmnop", "log-live", 200)

	// A historical row whose key has been deleted from key management, and an
	// empty hash from a row logged without a selected key.
	const deletedKeyHash = "deadbeefdeadbeefdeadbeefdeadbeef"

	service := NewLogService(database, enc)
	masks := service.ResolveKeyIdentifiers([]string{liveHash, deletedKeyHash, ""})

	if _, ok := masks[liveHash]; !ok {
		t.Errorf("live key was not resolved")
	}
	if _, ok := masks[deletedKeyHash]; ok {
		t.Errorf("deleted key resolved to a mask %q; it must fall back to a fingerprint", masks[deletedKeyHash])
	}
	if _, ok := masks[""]; ok {
		t.Errorf("empty key hash resolved to a mask")
	}
}

func TestResolveKeyIdentifiersFallBackWhenAPIKeysUnavailable(t *testing.T) {
	// newRequestLogTestDB has no api_keys table, standing in for a database where
	// the table is unavailable. Log listing must degrade to fingerprints, not fail.
	database := newRequestLogTestDB(t)
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	service := NewLogService(database, enc)

	masks := service.ResolveKeyIdentifiers([]string{enc.Hash("sk-any-key-value-here")})
	if len(masks) != 0 {
		t.Fatalf("expected no masks when api_keys is unavailable, got %v", masks)
	}
}

func TestLogFilterMatchesMaskedIdentifier(t *testing.T) {
	// The reporter's workflow: copy the identifier column, paste it into search.
	database := newKeyIdentifierTestDB(t)
	enc, err := encryption.NewService("unit-test-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	const keyAlpha = "sk-alpha-abcdefghijklmnop"
	const keyBeta = "sk-beta-qrstuvwxyz012345"
	alphaHash := seedKeyWithLogs(t, database, enc, keyAlpha, "log-alpha", 400)
	seedKeyWithLogs(t, database, enc, keyBeta, "log-beta", 200)

	service := NewLogService(database, enc)

	var rows []models.RequestLog
	if err := service.GetLogsQuery(LogFilter{KeyValue: utils.MaskKeyIdentifier(keyAlpha)}).
		Find(&rows).Error; err != nil {
		t.Fatalf("search by mask: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("mask search returned %d rows, want 1", len(rows))
	}
	if rows[0].KeyHash != alphaHash {
		t.Fatalf("mask search matched the wrong key hash")
	}

	// The full key and the fingerprint must keep working alongside the mask.
	for name, searchValue := range map[string]string{
		"full key":    keyAlpha,
		"fingerprint": utils.KeyFingerprint(alphaHash),
	} {
		var count int64
		if err := service.GetLogsQuery(LogFilter{KeyValue: searchValue}).Count(&count).Error; err != nil {
			t.Fatalf("count logs by %s: %v", name, err)
		}
		if count != 1 {
			t.Errorf("search by %s returned %d rows, want 1", name, count)
		}
	}
}

func TestLogFilterMaskWithNoMatchingKeyReturnsNothing(t *testing.T) {
	database := newKeyIdentifierTestDB(t)
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	seedKeyWithLogs(t, database, enc, "sk-alpha-abcdefghijklmnop", "log-alpha", 200)

	service := NewLogService(database, enc)
	var count int64
	if err := service.GetLogsQuery(LogFilter{KeyValue: "zzzz****zzzz"}).Count(&count).Error; err != nil {
		t.Fatalf("count logs for unmatched mask: %v", err)
	}
	if count != 0 {
		t.Fatalf("unmatched mask returned %d rows, want 0", count)
	}
}

func TestLogFilterMaskMatchesEveryCollidingKey(t *testing.T) {
	// Masks keep only eight characters, so two keys can share one. Both must be
	// returned rather than the search silently picking one of them.
	database := newKeyIdentifierTestDB(t)
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	const keyOne = "sk-pAAAAAAAAAAAAAAA1234"
	const keyTwo = "sk-pBBBBBBBBBBBBBBB1234"
	if utils.MaskKeyIdentifier(keyOne) != utils.MaskKeyIdentifier(keyTwo) {
		t.Fatalf("test fixture no longer produces colliding masks")
	}
	seedKeyWithLogs(t, database, enc, keyOne, "log-one", 400)
	seedKeyWithLogs(t, database, enc, keyTwo, "log-two", 400)

	service := NewLogService(database, enc)
	var count int64
	if err := service.GetLogsQuery(LogFilter{KeyValue: utils.MaskKeyIdentifier(keyOne)}).
		Count(&count).Error; err != nil {
		t.Fatalf("count logs for colliding mask: %v", err)
	}
	if count != 2 {
		t.Fatalf("colliding mask returned %d rows, want 2", count)
	}
}

func TestStreamLogKeysToCSVExportsMaskAndFingerprint(t *testing.T) {
	database := newKeyIdentifierTestDB(t)
	enc, err := encryption.NewService("unit-test-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	const liveKey = "sk-live-abcdefghijklmnop"
	liveHash := seedKeyWithLogs(t, database, enc, liveKey, "csv-live", 400)

	// A historical row with no surviving key, which must export a fingerprint.
	const orphanHash = "0123456789abcdef0123456789abcdef"
	if err := database.Create(&models.RequestLog{
		ID:         "csv-orphan",
		KeyHash:    orphanHash,
		GroupName:  "primary",
		StatusCode: 500,
	}).Error; err != nil {
		t.Fatalf("insert orphan request log: %v", err)
	}

	service := NewLogService(database, enc)
	var output bytes.Buffer
	if err := service.StreamLogKeysToCSV(LogFilter{}, &output); err != nil {
		t.Fatalf("StreamLogKeysToCSV: %v", err)
	}
	if strings.Contains(output.String(), liveKey) {
		t.Fatalf("CSV leaked the complete key: %s", output.String())
	}

	records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	wantHeader := []string{"key_identifier", "key_fingerprint", "group_name", "status_code"}
	if len(records) == 0 || len(records[0]) != len(wantHeader) {
		t.Fatalf("unexpected CSV header %v, want %v", records[0], wantHeader)
	}
	for i, column := range wantHeader {
		if records[0][i] != column {
			t.Fatalf("CSV header[%d] = %q, want %q", i, records[0][i], column)
		}
	}

	rowsByFingerprint := make(map[string][]string)
	for _, record := range records[1:] {
		rowsByFingerprint[record[1]] = record
	}

	liveRow, ok := rowsByFingerprint[utils.KeyFingerprint(liveHash)]
	if !ok {
		t.Fatalf("CSV omitted the live key row: %s", output.String())
	}
	if got, want := liveRow[0], utils.KeyIdentifier(liveKey, liveHash); got != want {
		t.Errorf("CSV identifier for live key = %q, want %q", got, want)
	}
	if mask := utils.MaskKeyIdentifier(liveKey); !strings.HasPrefix(liveRow[0], mask) {
		t.Errorf("CSV identifier %q does not begin with the key management mask %q", liveRow[0], mask)
	}

	orphanRow, ok := rowsByFingerprint[utils.KeyFingerprint(orphanHash)]
	if !ok {
		t.Fatalf("CSV omitted the historical row: %s", output.String())
	}
	if got, want := orphanRow[0], utils.KeyFingerprint(orphanHash); got != want {
		t.Errorf("CSV identifier for historical row = %q, want fingerprint %q", got, want)
	}
}
