package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestSearchByPastedIdentifierAtScale is the copy-and-paste path measured at the
// scale where masks collide: an operator copies the whole displayed value,
// including the discriminator, and pastes it into the search box.
//
// The frontend always searches through POST /logs/search
// (web/src/api/logs.ts), so that is the endpoint driven here, with a JSON body
// exactly as the browser sends it.
func TestSearchByPastedIdentifierAtScale(t *testing.T) {
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
	enc, err := encryption.NewService("paste-search-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	// Every key shares the head "sk-p" and the tail "9z7q", so all of them have
	// the same bare mask and only the discriminator separates them.
	const keyCount = 2000
	apiKeys := make([]models.APIKey, 0, keyCount)
	logRows := make([]models.RequestLog, 0, keyCount)
	var targetKey, targetHash string

	for index := range keyCount {
		plaintext := fmt.Sprintf("sk-p%013d9z7q", index)
		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt key: %v", err)
		}
		keyHash := enc.Hash(plaintext)
		apiKeys = append(apiKeys, models.APIKey{
			KeyValue: ciphertext,
			KeyHash:  keyHash,
			GroupID:  1,
			Status:   models.KeyStatusActive,
		})
		logRows = append(logRows, models.RequestLog{
			ID:         fmt.Sprintf("paste-%04d", index),
			KeyHash:    keyHash,
			KeyValue:   utils.KeyFingerprint(keyHash),
			GroupName:  "primary",
			StatusCode: 400,
		})
		if index == keyCount/2 {
			targetKey, targetHash = plaintext, keyHash
		}
	}
	if err := database.CreateInBatches(apiKeys, 500).Error; err != nil {
		t.Fatalf("insert api keys: %v", err)
	}
	if err := database.CreateInBatches(logRows, 500).Error; err != nil {
		t.Fatalf("insert request logs: %v", err)
	}

	logService := services.NewLogService(database, enc)
	server := &Server{LogService: logService, EncryptionSvc: enc}
	router := gin.New()
	router.POST("/logs/search", server.SearchLogs)
	router.GET("/logs", server.GetLogs)

	// Establish what the column actually displays for the target row, by reading
	// it back through the API rather than recomputing it.
	displayed := identifierShownFor(t, router, targetHash)
	expected := utils.KeyIdentifier(targetKey, targetHash)
	if displayed != expected {
		t.Fatalf("column shows %q, want %q", displayed, expected)
	}

	type searchResult struct {
		input string
		kind  string
		rows  int
		first string
	}

	inputs := []struct {
		value string
		kind  string
		want  int
	}{
		{displayed, "pasted column value", 1},
		{" " + displayed + " ", "pasted with whitespace", 1},
		// A bare mask genuinely matches every key that masks that way. The result
		// is capped at services.maskSearchKeyLimit (1000) so a degenerate mask
		// cannot build an unbounded IN clause; the cap is logged as a warning.
		{utils.MaskKeyIdentifier(targetKey), "bare mask only (capped)", 1000},
		{utils.KeyFingerprint(targetHash), "fingerprint", 1},
		{targetKey, "complete key", 1},
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Printf(" POST /logs/search with %d keys that all share the bare mask %q\n",
		keyCount, utils.MaskKeyIdentifier(targetKey))
	fmt.Println("================================================================================")
	fmt.Printf("displayed column value for the target row: %s\n\n", displayed)
	fmt.Printf("%-30s  %-24s  %-7s  %s\n", "SEARCH INPUT (as pasted)", "KIND", "ROWS", "MATCHED ROW")
	fmt.Println(strings.Repeat("-", 88))

	results := make([]searchResult, 0, len(inputs))
	for _, input := range inputs {
		body, err := json.Marshal(map[string]any{"key_value": input.value, "page_size": 5})
		if err != nil {
			t.Fatalf("marshal search body: %v", err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/logs/search", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("search %q: status = %d, body = %s", input.value, recorder.Code, recorder.Body.String())
		}

		var response struct {
			Data struct {
				Items []struct {
					ID       string `json:"id"`
					KeyValue string `json:"key_value"`
				} `json:"items"`
				Pagination struct {
					TotalItems int `json:"total_items"`
				} `json:"pagination"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode search response: %v", err)
		}

		total := response.Data.Pagination.TotalItems
		first := ""
		if len(response.Data.Items) > 0 {
			first = response.Data.Items[0].ID
		}
		results = append(results, searchResult{input.value, input.kind, total, first})

		verdict := ""
		if total != input.want {
			verdict = fmt.Sprintf("  <-- WANT %d", input.want)
			t.Errorf("search by %s returned %d rows, want %d", input.kind, total, input.want)
		}
		shown := input.value
		if len(shown) > 28 {
			shown = shown[:28] + "…"
		}
		fmt.Printf("%-30s  %-24s  %-7d  %s%s\n", shown, input.kind, total, first, verdict)
	}

	// The pasted value must land on the target row and nothing else, even though
	// 1999 other keys share its mask.
	if results[0].rows != 1 {
		t.Fatalf("pasted identifier matched %d rows, want 1", results[0].rows)
	}
	if results[0].first != fmt.Sprintf("paste-%04d", keyCount/2) {
		t.Errorf("pasted identifier matched row %q, want the target row", results[0].first)
	}

	fmt.Println()
	fmt.Printf("The pasted value pinpoints one row out of %d sharing the same mask.\n", keyCount)
	fmt.Println("The bare mask still matches all of them, which is honest: a mask alone")
	fmt.Println("cannot pick one. That form is no longer what the column shows.")

	// The GET compatibility endpoint accepts only fingerprints, by design, so an
	// identifier there is rejected rather than silently widened to the bare mask
	// (a URL fragment would otherwise strip the discriminator).
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "/logs?key_value="+url.QueryEscape(displayed), nil))
	fmt.Printf("\nGET /logs?key_value=<identifier> -> %d (fingerprint-only by design; the UI uses POST)\n",
		recorder.Code)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("GET with an identifier returned %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if strings.Contains(recorder.Body.String(), displayed) {
		t.Errorf("GET rejection echoed the supplied value: %s", recorder.Body.String())
	}
	fmt.Println()
}

// identifierShownFor reads the identifier the API puts on the page for a given
// key hash, so assertions compare against what an operator would actually copy.
func identifierShownFor(
	t *testing.T,
	router *gin.Engine,
	keyHash string,
) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"key_value": utils.KeyFingerprint(keyHash),
		"page_size": 1,
	})
	if err != nil {
		t.Fatalf("marshal lookup body: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/logs/search", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("lookup: status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Data struct {
			Items []struct {
				KeyValue string `json:"key_value"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode lookup response: %v", err)
	}
	if len(response.Data.Items) == 0 {
		t.Fatalf("lookup returned no rows for %s", utils.KeyFingerprint(keyHash))
	}
	return response.Data.Items[0].KeyValue
}
