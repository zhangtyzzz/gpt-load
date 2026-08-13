package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gpt-load/internal/encryption"
	"gpt-load/internal/i18n"
	"gpt-load/internal/models"
	"gpt-load/internal/services"
	"gpt-load/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// statementCounter is a gorm logger that records every SQL statement actually
// executed, so the cost of a log page can be measured instead of estimated.
type statementCounter struct {
	mu         sync.Mutex
	statements []string
}

func (c *statementCounter) LogMode(gormlogger.LogLevel) gormlogger.Interface { return c }
func (c *statementCounter) Info(context.Context, string, ...any)             {}
func (c *statementCounter) Warn(context.Context, string, ...any)             {}
func (c *statementCounter) Error(context.Context, string, ...any)            {}

func (c *statementCounter) Trace(
	_ context.Context,
	_ time.Time,
	fc func() (string, int64),
	_ error,
) {
	sql, _ := fc()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, sql)
}

func (c *statementCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = nil
}

func (c *statementCounter) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.statements...)
}

// countMatching reports how many recorded statements touch the given table.
func (c *statementCounter) countMatching(fragment string) int {
	count := 0
	for _, sql := range c.all() {
		if strings.Contains(sql, fragment) {
			count++
		}
	}
	return count
}

// countingEncryption counts Decrypt calls so per-row versus per-key decryption
// can be told apart.
type countingEncryption struct {
	inner    encryption.Service
	mu       sync.Mutex
	decrypts int
}

func (e *countingEncryption) Encrypt(plaintext string) (string, error) {
	return e.inner.Encrypt(plaintext)
}

func (e *countingEncryption) Decrypt(ciphertext string) (string, error) {
	e.mu.Lock()
	e.decrypts++
	e.mu.Unlock()
	return e.inner.Decrypt(ciphertext)
}

func (e *countingEncryption) Hash(plaintext string) string { return e.inner.Hash(plaintext) }

func (e *countingEncryption) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.decrypts = 0
}

func (e *countingEncryption) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.decrypts
}

// TestLogPageResolutionCostIsBounded measures the real cost of rendering a log
// page through the HTTP endpoint. The read-time resolution design moves work
// from write to read, so the claim that it is batched rather than per-row has to
// be measured, not asserted.
func TestLogPageResolutionCostIsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}

	counter := &statementCounter{}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: counter})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	base, err := encryption.NewService("cost-measurement-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	enc := &countingEncryption{inner: base}

	// 120 distinct keys, three log rows each: enough that a page of any tested
	// size is full and repeated keys appear within a page.
	const distinctKeys = 120
	const rowsPerKey = 3
	timestamp := time.Now()
	for keyIndex := range distinctKeys {
		plaintext := fmt.Sprintf("sk-cost-%04d-abcdefghijklmnop", keyIndex)
		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt key: %v", err)
		}
		keyHash := enc.Hash(plaintext)
		if err := database.Create(&models.APIKey{
			KeyValue: ciphertext,
			KeyHash:  keyHash,
			GroupID:  1,
			Status:   models.KeyStatusActive,
		}).Error; err != nil {
			t.Fatalf("insert api key: %v", err)
		}
		for rowIndex := range rowsPerKey {
			timestamp = timestamp.Add(time.Second)
			if err := database.Create(&models.RequestLog{
				ID:         fmt.Sprintf("cost-%04d-%d", keyIndex, rowIndex),
				Timestamp:  timestamp,
				KeyHash:    keyHash,
				GroupName:  "primary",
				StatusCode: 200,
			}).Error; err != nil {
				t.Fatalf("insert request log: %v", err)
			}
		}
	}

	server := &Server{
		LogService:    services.NewLogService(database, enc),
		EncryptionSvc: enc,
	}
	router := gin.New()
	router.GET("/logs", server.GetLogs)

	type measurement struct {
		pageSize            int
		apiKeyStatements    int
		requestLogStatement int
		totalStatements     int
		decrypts            int
		distinctKeysOnPage  int
	}

	// 15 is the frontend default (web/src/components/logs/LogTable.vue pageSize).
	pageSizes := []int{15, 30, 60}
	measurements := make([]measurement, 0, len(pageSizes))

	for _, pageSize := range pageSizes {
		counter.reset()
		enc.reset()

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodGet,
			fmt.Sprintf("/logs?page=1&page_size=%d", pageSize),
			nil,
		)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("page_size=%d: status = %d, body = %s", pageSize, recorder.Code, recorder.Body.String())
		}

		// Rows are ordered by timestamp desc and each key owns rowsPerKey
		// consecutive rows, so a page of pageSize rows covers this many keys.
		distinctOnPage := (pageSize + rowsPerKey - 1) / rowsPerKey

		measurements = append(measurements, measurement{
			pageSize:            pageSize,
			apiKeyStatements:    counter.countMatching("`api_keys`"),
			requestLogStatement: counter.countMatching("`request_logs`"),
			totalStatements:     len(counter.all()),
			decrypts:            enc.count(),
			distinctKeysOnPage:  distinctOnPage,
		})
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println(" Cost of rendering one log page (measured through GET /logs)")
	fmt.Println("================================================================================")
	fmt.Printf("%-10s  %-12s  %-14s  %-11s  %-9s  %s\n",
		"PAGE SIZE", "SQL TOTAL", "request_logs", "api_keys", "DECRYPTS", "DISTINCT KEYS ON PAGE")
	fmt.Println(strings.Repeat("-", 88))
	for _, m := range measurements {
		fmt.Printf("%-10d  %-12d  %-14d  %-11d  %-9d  %d\n",
			m.pageSize, m.totalStatements, m.requestLogStatement,
			m.apiKeyStatements, m.decrypts, m.distinctKeysOnPage)
	}

	for _, m := range measurements {
		// One batched IN query for the whole page. Anything that scales with
		// pageSize here is an N+1.
		if m.apiKeyStatements != 1 {
			t.Errorf("page_size=%d issued %d api_keys statements, want exactly 1 (N+1 regression)",
				m.pageSize, m.apiKeyStatements)
		}
		// A count query plus a find query, unchanged by this feature.
		if m.requestLogStatement != 2 {
			t.Errorf("page_size=%d issued %d request_logs statements, want 2",
				m.pageSize, m.requestLogStatement)
		}
		if m.totalStatements != 3 {
			t.Errorf("page_size=%d issued %d statements in total, want 3",
				m.pageSize, m.totalStatements)
		}
		// Decryption is per distinct key on the page, never per row.
		if m.decrypts != m.distinctKeysOnPage {
			t.Errorf("page_size=%d performed %d decrypts for %d distinct keys; want one per distinct key",
				m.pageSize, m.decrypts, m.distinctKeysOnPage)
		}
		if m.decrypts >= m.pageSize {
			t.Errorf("page_size=%d performed %d decrypts, which is per-row rather than per-key",
				m.pageSize, m.decrypts)
		}
	}

	fmt.Println()
	fmt.Println("SQL statement count is CONSTANT in page size (3: count, find, one batched IN).")
	fmt.Println("Decryption is LINEAR in distinct keys on the page, not in rows -- repeated")
	fmt.Println("keys are deduplicated before the lookup.")

	// Show the actual statements for the default page size so the shape of the
	// added query is on the record rather than described.
	counter.reset()
	enc.reset()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs?page=1&page_size=15", nil))
	fmt.Println()
	fmt.Println("Statements executed for the default page size (15):")
	for i, sql := range counter.all() {
		trimmed := sql
		if len(trimmed) > 150 {
			trimmed = trimmed[:150] + " ...[truncated]"
		}
		fmt.Printf("  %d. %s\n", i+1, trimmed)
	}
	fmt.Println()
}

// TestLogPageDeduplicatesRepeatedKeyBeforeLookup pins the deduplication that
// keeps decryption per-key: a page where every row uses one key must decrypt
// exactly once.
func TestLogPageDeduplicatesRepeatedKeyBeforeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}

	counter := &statementCounter{}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: counter})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	base, err := encryption.NewService("dedup-measurement-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	enc := &countingEncryption{inner: base}

	const plaintext = "sk-single-key-abcdefghijklmnop"
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	keyHash := enc.Hash(plaintext)
	if err := database.Create(&models.APIKey{
		KeyValue: ciphertext,
		KeyHash:  keyHash,
		GroupID:  1,
		Status:   models.KeyStatusActive,
	}).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	const rows = 50
	timestamp := time.Now()
	for i := range rows {
		timestamp = timestamp.Add(time.Second)
		if err := database.Create(&models.RequestLog{
			ID:         fmt.Sprintf("dedup-%03d", i),
			Timestamp:  timestamp,
			KeyHash:    keyHash,
			StatusCode: 400,
		}).Error; err != nil {
			t.Fatalf("insert request log: %v", err)
		}
	}

	server := &Server{LogService: services.NewLogService(database, enc), EncryptionSvc: enc}
	router := gin.New()
	router.GET("/logs", server.GetLogs)

	counter.reset()
	enc.reset()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs?page=1&page_size=50", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	fmt.Printf("\n50 rows sharing one key: %d SQL statements, %d decrypts (want 3 and 1)\n\n",
		len(counter.all()), enc.count())

	if got := enc.count(); got != 1 {
		t.Errorf("decrypts = %d for 50 rows of one key, want 1", got)
	}
	if got := counter.countMatching("`api_keys`"); got != 1 {
		t.Errorf("api_keys statements = %d, want 1", got)
	}
}

// TestKeySearchCostByInputKind measures each search input. The masked-key branch
// has to scan and decrypt api_keys to compare plaintext, which is the one
// genuinely expensive path this feature adds; the discriminator in the displayed
// identifier turns it back into an indexed lookup.
func TestKeySearchCostByInputKind(t *testing.T) {
	counter := &statementCounter{}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: counter})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	base, err := encryption.NewService("search-cost-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	enc := &countingEncryption{inner: base}

	const totalKeys = 400
	var targetKey, targetHash string
	for keyIndex := range totalKeys {
		plaintext := fmt.Sprintf("sk-search-%04d-abcdefghijkl", keyIndex)
		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt key: %v", err)
		}
		keyHash := enc.Hash(plaintext)
		if err := database.Create(&models.APIKey{
			KeyValue: ciphertext,
			KeyHash:  keyHash,
			GroupID:  1,
			Status:   models.KeyStatusActive,
		}).Error; err != nil {
			t.Fatalf("insert api key: %v", err)
		}
		if err := database.Create(&models.RequestLog{
			ID:         fmt.Sprintf("search-%04d", keyIndex),
			KeyHash:    keyHash,
			StatusCode: 400,
		}).Error; err != nil {
			t.Fatalf("insert request log: %v", err)
		}
		if keyIndex == totalKeys/2 {
			targetKey, targetHash = plaintext, keyHash
		}
	}

	service := services.NewLogService(database, enc)

	// Every generated key shares the head "sk-s" and the tail "ijkl", so all 400
	// have the same bare mask. That is not a contrived fixture: keys minted by one
	// provider share a prefix, and a fixed-format suffix is common too. It is the
	// clearest statement of why a bare mask cannot be the displayed identifier.
	cases := []struct {
		input    string
		kind     string
		wantRows int64
	}{
		{utils.KeyIdentifier(targetKey, targetHash), "displayed identifier", 1},
		{utils.MaskKeyIdentifier(targetKey), "bare mask (legacy form)", totalKeys},
		{utils.KeyFingerprint(targetHash), "fingerprint", 1},
		{targetKey, "complete key", 1},
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Printf(" Search cost by input kind (%d keys, all sharing one bare mask)\n", totalKeys)
	fmt.Println("================================================================================")
	fmt.Printf("%-26s  %-12s  %-10s  %s\n", "KIND", "SQL", "DECRYPTS", "ROWS MATCHED")
	fmt.Println(strings.Repeat("-", 66))

	costs := make(map[string]int, len(cases))
	for _, searchCase := range cases {
		counter.reset()
		enc.reset()

		var rows int64
		if err := service.GetLogsQuery(services.LogFilter{KeyValue: searchCase.input}).
			Count(&rows).Error; err != nil {
			t.Fatalf("search %q: %v", searchCase.input, err)
		}
		costs[searchCase.kind] = enc.count()
		fmt.Printf("%-26s  %-12d  %-10d  %d\n",
			searchCase.kind, len(counter.all()), enc.count(), rows)

		if rows != searchCase.wantRows {
			t.Errorf("search by %s matched %d rows, want %d",
				searchCase.kind, rows, searchCase.wantRows)
		}
	}

	// The fingerprint and complete-key branches never touch api_keys at all.
	if costs["fingerprint"] != 0 || costs["complete key"] != 0 {
		t.Errorf("fingerprint/complete-key search decrypted keys: %d/%d",
			costs["fingerprint"], costs["complete key"])
	}
	// The bare mask has to compare plaintext, so it decrypts the table.
	if costs["bare mask (legacy form)"] < totalKeys/2 {
		t.Errorf("bare mask search decrypted only %d of %d keys; expected a scan",
			costs["bare mask (legacy form)"], totalKeys)
	}
	// The displayed identifier narrows by an indexed hash prefix first, so it
	// decrypts a tiny fraction. This is the value of the discriminator beyond
	// disambiguation.
	if costs["displayed identifier"] >= costs["bare mask (legacy form)"]/10 {
		t.Errorf("displayed-identifier search decrypted %d keys, want far fewer than the %d a bare mask needs",
			costs["displayed identifier"], costs["bare mask (legacy form)"])
	}

	fmt.Println()
	fmt.Printf("The bare mask matched all %d rows: with a shared prefix and suffix every key\n", totalKeys)
	fmt.Printf("masks the same. The discriminator narrows by an indexed key_hash prefix,\n")
	fmt.Printf("selecting exactly one key and cutting decryptions from %d to %d.\n",
		costs["bare mask (legacy form)"], costs["displayed identifier"])
	fmt.Println()
}
