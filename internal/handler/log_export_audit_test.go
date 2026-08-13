package handler

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/i18n"
	"gpt-load/internal/models"
	"gpt-load/internal/services"
	"gpt-load/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestExportBytesMatchPageDisplay generates the real export content and compares
// it, byte for byte in the identifier column, against what GET /logs puts on the
// page for the same rows. An export that disagrees with the screen is a trap, so
// the agreement is asserted rather than assumed, and the raw bytes are printed.
func TestExportBytesMatchPageDisplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("initialize i18n: %v", err)
	}
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	enc, err := encryption.NewService("export-audit-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	// A realistic mix: an ordinary key, two keys whose masks collide, and a
	// historical row whose key is gone.
	keys := []string{
		"sk-ordinary-abcdefghijkl",
		"sk-proj-AAAAAAAAAAAA9z7q",
		"sk-proj-BBBBBBBBBBBB9z7q",
	}
	plaintextByHash := make(map[string]string, len(keys))
	for index, plaintext := range keys {
		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt key: %v", err)
		}
		keyHash := enc.Hash(plaintext)
		plaintextByHash[keyHash] = plaintext
		if err := database.Create(&models.APIKey{
			KeyValue: ciphertext,
			KeyHash:  keyHash,
			GroupID:  1,
			Status:   models.KeyStatusActive,
		}).Error; err != nil {
			t.Fatalf("insert api key: %v", err)
		}
		if err := database.Create(&models.RequestLog{
			ID:         fmt.Sprintf("export-%d", index),
			KeyHash:    keyHash,
			KeyValue:   utils.KeyFingerprint(keyHash),
			GroupName:  "primary",
			StatusCode: 400,
		}).Error; err != nil {
			t.Fatalf("insert request log: %v", err)
		}
	}

	const orphanHash = "9a8b7c6d5e4f30219a8b7c6d5e4f30219a8b7c6d5e4f30219a8b7c6d5e4f3021"
	if err := database.Create(&models.RequestLog{
		ID:         "export-orphan",
		KeyHash:    orphanHash,
		KeyValue:   utils.KeyFingerprint(orphanHash),
		GroupName:  "primary",
		StatusCode: 401,
	}).Error; err != nil {
		t.Fatalf("insert orphan request log: %v", err)
	}

	logService := services.NewLogService(database, enc)
	server := &Server{LogService: logService, EncryptionSvc: enc}

	// ---- what the page shows -------------------------------------------------
	router := gin.New()
	router.GET("/logs", server.GetLogs)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs?page=1&page_size=50", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var page struct {
		Data struct {
			Items []struct {
				ID             string `json:"id"`
				KeyValue       string `json:"key_value"`
				KeyFingerprint string `json:"key_fingerprint"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page response: %v", err)
	}
	if len(page.Data.Items) != len(keys)+1 {
		t.Fatalf("page returned %d rows, want %d", len(page.Data.Items), len(keys)+1)
	}
	pageIdentifierByFingerprint := make(map[string]string, len(page.Data.Items))
	for _, item := range page.Data.Items {
		pageIdentifierByFingerprint[item.KeyFingerprint] = item.KeyValue
	}

	// ---- the real export bytes ----------------------------------------------
	var exported bytes.Buffer
	if err := logService.StreamLogKeysToCSV(services.LogFilter{}, &exported); err != nil {
		t.Fatalf("StreamLogKeysToCSV: %v", err)
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println(" Real export bytes")
	fmt.Println("================================================================================")
	fmt.Printf("%q\n", exported.String())
	fmt.Println()
	fmt.Println("Rendered:")
	fmt.Print(exported.String())

	records, err := csv.NewReader(strings.NewReader(exported.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse exported CSV: %v", err)
	}

	fmt.Println()
	fmt.Printf("%-27s  %-27s  %-8s\n", "EXPORT key_identifier", "PAGE key_value", "SAME?")
	fmt.Println(strings.Repeat("-", 68))

	matched := 0
	for _, record := range records[1:] {
		exportIdentifier, fingerprint := record[0], record[1]
		pageIdentifier, onPage := pageIdentifierByFingerprint[fingerprint]
		if !onPage {
			t.Errorf("export row %q has no matching page row", fingerprint)
			continue
		}
		matched++
		verdict := "yes"
		if exportIdentifier != pageIdentifier {
			verdict = "NO"
			t.Errorf("export shows %q but the page shows %q for %s",
				exportIdentifier, pageIdentifier, fingerprint)
		}
		fmt.Printf("%-27s  %-27s  %-8s\n", exportIdentifier, pageIdentifier, verdict)
	}
	if matched != len(keys)+1 {
		t.Fatalf("cross-checked %d export rows, want %d", matched, len(keys)+1)
	}

	// ---- what must not be in the bytes --------------------------------------
	content := exported.String()
	for _, plaintext := range keys {
		if strings.Contains(content, plaintext) {
			t.Errorf("export leaked a complete key %q", plaintext)
		}
	}
	for keyHash := range plaintextByHash {
		if strings.Contains(content, keyHash) {
			t.Errorf("export leaked a full key hash")
		}
	}
	if strings.Contains(content, orphanHash) {
		t.Errorf("export leaked the orphan key hash")
	}
	// Only the four declared columns, nothing appended.
	for index, record := range records {
		if len(record) != 4 {
			t.Errorf("export row %d has %d columns, want 4: %v", index, len(record), record)
		}
	}

	fmt.Println()
	fmt.Println("Export matches the page for every row. No complete key and no full key hash")
	fmt.Println("appears in the bytes; only the masked identifier, the truncated fingerprint,")
	fmt.Println("the group name, and the status code.")
	fmt.Println()
}
