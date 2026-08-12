package services

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestVerifyLogKeyIdentifierMatchesKeyManagement is an executable verification
// of the requirement behind this change, not a unit test of one function:
//
//	seeing a failing row in the request log, an operator must be able to tell
//	which row in key management that key is.
//
// It builds a realistic database (keys encrypted at rest, request logs holding
// only a one-way hash), then prints the key management value and the request-log
// identifier side by side so the correspondence can be checked by eye. It also
// exercises the historical-row fallback, all three search inputs, and the CSV
// export in the same run.
//
// Run it with:
//
//	GOPROXY=off go test ./internal/services/ \
//	  -run TestVerifyLogKeyIdentifierMatchesKeyManagement -v
func TestVerifyLogKeyIdentifierMatchesKeyManagement(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.APIKey{}, &models.RequestLog{}, &models.GroupHourlyStat{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	// A real deployment with encryption enabled: keys are AES-GCM encrypted at
	// rest and correlated by HMAC-SHA256.
	enc, err := encryption.NewService("verification-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	service := NewLogService(database, enc)

	type scenario struct {
		plaintext  string
		logID      string
		statusCode int
	}
	scenarios := []scenario{
		{"sk-proj-alpha-9f3c2b1a7d4e", "log-alpha-ok", 200},
		{"sk-proj-alpha-9f3c2b1a7d4e", "log-alpha-bad", 400}, // same key, failing row
		{"sk-proj-bravo-5c8d1e0f6a2b", "log-bravo-bad", 400},
		{"sk-ant-charlie-3e7f9d2c4b81", "log-charlie-ok", 200},
	}

	keyHashByPlaintext := make(map[string]string)
	for _, sc := range scenarios {
		keyHash := enc.Hash(sc.plaintext)
		if _, seeded := keyHashByPlaintext[sc.plaintext]; !seeded {
			encrypted, err := enc.Encrypt(sc.plaintext)
			if err != nil {
				t.Fatalf("encrypt key: %v", err)
			}
			if err := database.Create(&models.APIKey{
				KeyValue: encrypted,
				KeyHash:  keyHash,
				GroupID:  1,
				Status:   models.KeyStatusActive,
			}).Error; err != nil {
				t.Fatalf("insert api key: %v", err)
			}
			keyHashByPlaintext[sc.plaintext] = keyHash
		}
		if err := database.Create(&models.RequestLog{
			ID:         sc.logID,
			KeyHash:    keyHash,
			KeyValue:   utils.KeyFingerprint(keyHash),
			GroupName:  "primary",
			StatusCode: sc.statusCode,
			IsSuccess:  sc.statusCode < 400,
		}).Error; err != nil {
			t.Fatalf("insert request log: %v", err)
		}
	}

	// A historical row: the key was used once, then deleted from key management.
	// Nothing remains but the one-way hash.
	const deletedKeyHash = "7b1e4a90c3d25f68a1b2c3d4e5f60718293a4b5c6d7e8f9012345678abcdef01"
	if err := database.Create(&models.RequestLog{
		ID:         "log-historical-deleted",
		KeyHash:    deletedKeyHash,
		KeyValue:   utils.KeyFingerprint(deletedKeyHash),
		GroupName:  "primary",
		StatusCode: 401,
	}).Error; err != nil {
		t.Fatalf("insert historical request log: %v", err)
	}

	// ---------------------------------------------------------------- part 1
	// The core claim: the log identifier equals the key management value.

	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" PART 1  Key management  vs  request-log identifier")
	fmt.Println("=========================================================================")
	fmt.Printf("%-16s  %-14s  %-14s  %-8s\n", "LOG ROW", "KEY MGMT SHOWS", "LOG COLUMN", "MATCH?")
	fmt.Println(strings.Repeat("-", 73))

	var logs []models.RequestLog
	if err := database.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load request logs: %v", err)
	}

	keyHashes := make([]string, 0, len(logs))
	for _, logRow := range logs {
		keyHashes = append(keyHashes, logRow.KeyHash)
	}
	masks := service.ResolveKeyMasks(keyHashes)

	// What key management renders for each key: the decrypted value, masked in the
	// browser by web/src/utils/display.ts maskKey().
	keyManagementDisplay := make(map[string]string)
	var storedKeys []models.APIKey
	if err := database.Find(&storedKeys).Error; err != nil {
		t.Fatalf("load api keys: %v", err)
	}
	for _, storedKey := range storedKeys {
		plaintext, err := enc.Decrypt(storedKey.KeyValue)
		if err != nil {
			t.Fatalf("decrypt stored key: %v", err)
		}
		keyManagementDisplay[storedKey.KeyHash] = utils.MaskKeyIdentifier(plaintext)
	}

	identifierFor := func(logRow models.RequestLog) string {
		if mask, ok := masks[logRow.KeyHash]; ok {
			return mask
		}
		return utils.KeyFingerprint(logRow.KeyHash)
	}

	liveRowsChecked := 0
	for _, logRow := range logs {
		expected, isLive := keyManagementDisplay[logRow.KeyHash]
		identifier := identifierFor(logRow)
		if !isLive {
			continue
		}
		liveRowsChecked++
		verdict := "yes"
		if identifier != expected {
			verdict = "NO"
		}
		fmt.Printf("%-16s  %-14s  %-14s  %-8s\n", logRow.ID, expected, identifier, verdict)
		if identifier != expected {
			t.Errorf("log row %s shows %q but key management shows %q", logRow.ID, identifier, expected)
		}
	}
	if liveRowsChecked != len(scenarios) {
		t.Fatalf("checked %d live rows, want %d", liveRowsChecked, len(scenarios))
	}

	// Distinct keys must be distinguishable, otherwise the column is useless even
	// when every value technically "matches".
	distinctMasks := make(map[string]struct{})
	for _, mask := range keyManagementDisplay {
		distinctMasks[mask] = struct{}{}
	}
	fmt.Printf("\n%d distinct keys -> %d distinct identifiers", len(keyManagementDisplay), len(distinctMasks))
	if len(distinctMasks) != len(keyManagementDisplay) {
		t.Errorf("masks collide: %d keys share %d identifiers", len(keyManagementDisplay), len(distinctMasks))
	}
	fmt.Println(" (each key is individually recognizable)")

	// The failing row and the succeeding row for one key must resolve to the same
	// identifier, which is what lets an operator group a key's failures.
	alphaHash := keyHashByPlaintext["sk-proj-alpha-9f3c2b1a7d4e"]
	var alphaOK, alphaBad models.RequestLog
	if err := database.First(&alphaOK, "id = ?", "log-alpha-ok").Error; err != nil {
		t.Fatalf("load succeeding row: %v", err)
	}
	if err := database.First(&alphaBad, "id = ?", "log-alpha-bad").Error; err != nil {
		t.Fatalf("load failing row: %v", err)
	}
	okIdentifier, badIdentifier := identifierFor(alphaOK), identifierFor(alphaBad)
	fmt.Printf("\nOne key, two outcomes:  200 row -> %q   400 row -> %q\n", okIdentifier, badIdentifier)
	if okIdentifier != badIdentifier {
		t.Errorf("the same key rendered as %q and %q across two rows", okIdentifier, badIdentifier)
	}

	// ---------------------------------------------------------------- part 2
	// Historical rows whose key no longer exists.

	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" PART 2  Historical row whose key was deleted from key management")
	fmt.Println("=========================================================================")

	var historical models.RequestLog
	if err := database.First(&historical, "id = ?", "log-historical-deleted").Error; err != nil {
		t.Fatalf("load historical row: %v", err)
	}
	historicalIdentifier := identifierFor(historical)
	fmt.Printf("log row        : %s\n", historical.ID)
	fmt.Printf("key mgmt shows : (nothing - the key no longer exists)\n")
	fmt.Printf("log column     : %s\n", historicalIdentifier)
	fmt.Printf("reason         : nothing remains but a one-way hash, so a mask cannot be\n")
	fmt.Printf("                 derived; the fingerprint is shown instead of a fabricated one.\n")

	if historicalIdentifier != utils.KeyFingerprint(deletedKeyHash) {
		t.Errorf("historical row identifier = %q, want fingerprint %q",
			historicalIdentifier, utils.KeyFingerprint(deletedKeyHash))
	}
	if strings.Contains(historicalIdentifier, utils.KeyMaskMarker) {
		t.Errorf("historical row fabricated a mask: %q", historicalIdentifier)
	}

	// ---------------------------------------------------------------- part 3
	// Search: copying the identifier column into the search box must work.

	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" PART 3  Search paths")
	fmt.Println("=========================================================================")
	fmt.Printf("%-34s  %-26s  %-5s\n", "SEARCH INPUT", "KIND", "ROWS")
	fmt.Println(strings.Repeat("-", 70))

	searchCases := []struct {
		input string
		kind  string
		want  int64
	}{
		{masks[alphaHash], "mask (copied column)", 2},
		{"sk-proj-alpha-9f3c2b1a7d4e", "complete key", 2},
		{utils.KeyFingerprint(alphaHash), "fingerprint", 2},
		{utils.KeyFingerprint(deletedKeyHash), "fingerprint (deleted key)", 1},
		{"zzzz****zzzz", "mask matching no key", 0},
	}
	for _, searchCase := range searchCases {
		var count int64
		if err := service.GetLogsQuery(LogFilter{KeyValue: searchCase.input}).Count(&count).Error; err != nil {
			t.Fatalf("search %q: %v", searchCase.input, err)
		}
		verdict := ""
		if count != searchCase.want {
			verdict = fmt.Sprintf("  <-- WANT %d", searchCase.want)
			t.Errorf("search %q (%s) returned %d rows, want %d",
				searchCase.input, searchCase.kind, count, searchCase.want)
		}
		fmt.Printf("%-34s  %-26s  %-5d%s\n", searchCase.input, searchCase.kind, count, verdict)
	}

	// ---------------------------------------------------------------- part 4
	// Export.

	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" PART 4  CSV export")
	fmt.Println("=========================================================================")

	var exported bytes.Buffer
	if err := service.StreamLogKeysToCSV(LogFilter{}, &exported); err != nil {
		t.Fatalf("StreamLogKeysToCSV: %v", err)
	}
	fmt.Print(exported.String())

	records, err := csv.NewReader(strings.NewReader(exported.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse exported CSV: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("CSV had no data rows: %q", exported.String())
	}
	for _, sc := range scenarios {
		if strings.Contains(exported.String(), sc.plaintext) {
			t.Fatalf("CSV leaked a complete key: %s", exported.String())
		}
	}
	exportedIdentifiers := make(map[string]struct{})
	for _, record := range records[1:] {
		exportedIdentifiers[record[0]] = struct{}{}
	}
	if _, ok := exportedIdentifiers[masks[alphaHash]]; !ok {
		t.Errorf("CSV did not carry the masked identifier %q", masks[alphaHash])
	}
	if _, ok := exportedIdentifiers[utils.KeyFingerprint(deletedKeyHash)]; !ok {
		t.Errorf("CSV did not carry the historical row fingerprint")
	}

	// ---------------------------------------------------------------- part 5
	// The database must not have gained any key material.

	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" PART 5  What the request_logs table actually stores")
	fmt.Println("=========================================================================")
	fmt.Printf("%-24s  %-22s  %s\n", "LOG ROW", "STORED key_value", "CONTAINS KEY MATERIAL?")
	fmt.Println(strings.Repeat("-", 74))

	var storedRows []models.RequestLog
	if err := database.Order("id asc").Find(&storedRows).Error; err != nil {
		t.Fatalf("reload request logs: %v", err)
	}
	for _, row := range storedRows {
		leaks := "no"
		for _, sc := range scenarios {
			if strings.Contains(row.KeyValue, sc.plaintext) {
				leaks = "YES"
			}
		}
		// The mask is computed at read time; it must never be persisted, or the
		// startup cleanup job would erase it and the database would hold partial
		// key material at rest.
		if strings.Contains(row.KeyValue, utils.KeyMaskMarker) {
			leaks = "YES (mask persisted)"
			t.Errorf("row %s persisted a mask in key_value: %q", row.ID, row.KeyValue)
		}
		if leaks != "no" {
			t.Errorf("row %s stored key material: %q", row.ID, row.KeyValue)
		}
		fmt.Printf("%-24s  %-22s  %s\n", row.ID, row.KeyValue, leaks)
	}
	fmt.Println("\nMasks are derived at read time from api_keys, so the log table keeps")
	fmt.Println("storing only a one-way hash and its fingerprint - unchanged by this work.")
	fmt.Println()
}
