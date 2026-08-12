package services

import (
	"fmt"
	"strings"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestAuditMaskCollisionIsDistinguishable constructs two keys that differ only
// in the middle, so they mask identically. Two questions have to be answered
// with output rather than argument: can the list tell them apart, and does a
// search on the displayed value conflate them.
func TestAuditMaskCollisionIsDistinguishable(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.APIKey{}, &models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	enc, err := encryption.NewService("collision-audit-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}

	// Same first four and same last four characters, different middle. This is
	// the realistic shape: every OpenAI project key begins "sk-p", so in practice
	// only the trailing characters differ.
	const keyOne = "sk-proj-AAAAAAAAAAAA9z7q"
	const keyTwo = "sk-proj-BBBBBBBBBBBB9z7q"

	if utils.MaskKeyIdentifier(keyOne) != utils.MaskKeyIdentifier(keyTwo) {
		t.Fatalf("fixture does not collide: %q vs %q",
			utils.MaskKeyIdentifier(keyOne), utils.MaskKeyIdentifier(keyTwo))
	}

	hashOne := seedKeyWithLogs(t, database, enc, keyOne, "collide-one", 400)
	hashTwo := seedKeyWithLogs(t, database, enc, keyTwo, "collide-two", 500)

	service := NewLogService(database, enc)
	identifiers := service.ResolveKeyIdentifiers([]string{hashOne, hashTwo})

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println(" Mask collision: two keys sharing first four and last four characters")
	fmt.Println("================================================================================")
	fmt.Printf("%-26s  %-14s  %s\n", "COMPLETE KEY", "BARE MASK", "LOG COLUMN SHOWS")
	fmt.Println(strings.Repeat("-", 78))
	fmt.Printf("%-26s  %-14s  %s\n", keyOne, utils.MaskKeyIdentifier(keyOne), identifiers[hashOne])
	fmt.Printf("%-26s  %-14s  %s\n", keyTwo, utils.MaskKeyIdentifier(keyTwo), identifiers[hashTwo])

	if identifiers[hashOne] == identifiers[hashTwo] {
		t.Fatalf("two distinct keys render identically as %q; the list cannot tell them apart",
			identifiers[hashOne])
	}
	fmt.Printf("\ndistinguishable in the list: yes (%q != %q)\n", identifiers[hashOne], identifiers[hashTwo])

	// The recognizable part must still be exactly what key management renders.
	for _, pair := range []struct {
		key        string
		identifier string
	}{{keyOne, identifiers[hashOne]}, {keyTwo, identifiers[hashTwo]}} {
		mask := utils.MaskKeyIdentifier(pair.key)
		if !strings.HasPrefix(pair.identifier, mask) {
			t.Errorf("identifier %q does not begin with the key management mask %q",
				pair.identifier, mask)
		}
	}

	// Searching the displayed value must return only that key's rows.
	fmt.Println()
	fmt.Printf("%-30s  %-24s  %-5s  %s\n", "SEARCH INPUT", "KIND", "ROWS", "WHICH")
	fmt.Println(strings.Repeat("-", 84))

	searchCases := []struct {
		input string
		kind  string
		want  int64
	}{
		{identifiers[hashOne], "displayed value, key one", 1},
		{identifiers[hashTwo], "displayed value, key two", 1},
		{utils.MaskKeyIdentifier(keyOne), "bare mask (ambiguous)", 2},
		{keyOne, "complete key one", 1},
		{utils.KeyFingerprint(hashOne), "fingerprint key one", 1},
	}
	for _, searchCase := range searchCases {
		var rows []models.RequestLog
		if err := service.GetLogsQuery(LogFilter{KeyValue: searchCase.input}).
			Order("id asc").Find(&rows).Error; err != nil {
			t.Fatalf("search %q: %v", searchCase.input, err)
		}
		which := make([]string, 0, len(rows))
		for _, row := range rows {
			which = append(which, row.ID)
		}
		verdict := ""
		if int64(len(rows)) != searchCase.want {
			verdict = fmt.Sprintf("  <-- WANT %d", searchCase.want)
			t.Errorf("search %q (%s) returned %d rows, want %d",
				searchCase.input, searchCase.kind, len(rows), searchCase.want)
		}
		fmt.Printf("%-30s  %-24s  %-5d  %s%s\n",
			searchCase.input, searchCase.kind, len(rows), strings.Join(which, ","), verdict)
	}

	fmt.Println()
	fmt.Println("Searching a displayed identifier never crosses keys. The bare mask is")
	fmt.Println("intentionally still accepted and returns both, since a mask alone genuinely")
	fmt.Println("cannot pick one -- but it is no longer what the column shows.")
	fmt.Println()
}

// TestAuditKeyLifecycleNeverMisattributes walks the three states a log row's key
// can be in and prints what the column shows for each. The requirement is not
// merely that something renders, but that nothing renders one key's failure as
// another key's.
func TestAuditKeyLifecycleNeverMisattributes(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.APIKey{}, &models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	oldEnc, err := encryption.NewService("lifecycle-old-encryption-key")
	if err != nil {
		t.Fatalf("create old encryption service: %v", err)
	}

	// survivor: untouched, still in key management.
	// deleted:  used, then removed from key management.
	// rotated:  still in key management, but ENCRYPTION_KEY was rotated after the
	//           log row was written, so the row's key_hash is the pre-rotation one.
	const survivorKey = "sk-survivor-abcdefghijkl"
	const deletedKey = "sk-deleted-mnopqrstuvwx"
	const rotatedKey = "sk-rotated-yz0123456789"

	survivorHash := seedKeyWithLogs(t, database, oldEnc, survivorKey, "life-survivor", 200)
	deletedHash := seedKeyWithLogs(t, database, oldEnc, deletedKey, "life-deleted", 401)
	rotatedHashBefore := seedKeyWithLogs(t, database, oldEnc, rotatedKey, "life-rotated-before", 400)

	// A purely historical row: only a one-way hash survives, no key ever matched.
	const historicalHash = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"
	if err := database.Create(&models.RequestLog{
		ID:         "life-historical",
		KeyHash:    historicalHash,
		KeyValue:   utils.KeyFingerprint(historicalHash),
		StatusCode: 500,
	}).Error; err != nil {
		t.Fatalf("insert historical row: %v", err)
	}

	// Delete the key from key management, as the UI does.
	if err := database.Where("key_hash = ?", deletedHash).Delete(&models.APIKey{}).Error; err != nil {
		t.Fatalf("delete api key: %v", err)
	}

	// Rotate ENCRYPTION_KEY exactly the way internal/commands/migrate.go does:
	// re-encrypt and re-hash api_keys, and leave request_logs untouched.
	newEnc, err := encryption.NewService("lifecycle-new-encryption-key")
	if err != nil {
		t.Fatalf("create new encryption service: %v", err)
	}
	var keysToRotate []models.APIKey
	if err := database.Find(&keysToRotate).Error; err != nil {
		t.Fatalf("load keys for rotation: %v", err)
	}
	for _, key := range keysToRotate {
		plaintext, err := oldEnc.Decrypt(key.KeyValue)
		if err != nil {
			t.Fatalf("decrypt during rotation: %v", err)
		}
		reEncrypted, err := newEnc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("re-encrypt during rotation: %v", err)
		}
		if err := database.Model(&models.APIKey{}).Where("id = ?", key.ID).Updates(map[string]any{
			"key_value": reEncrypted,
			"key_hash":  newEnc.Hash(plaintext),
		}).Error; err != nil {
			t.Fatalf("update key during rotation: %v", err)
		}
	}

	// A row written after the rotation carries the post-rotation hash.
	rotatedHashAfter := newEnc.Hash(rotatedKey)
	if err := database.Create(&models.RequestLog{
		ID:         "life-rotated-after",
		KeyHash:    rotatedHashAfter,
		KeyValue:   utils.KeyFingerprint(rotatedHashAfter),
		StatusCode: 400,
	}).Error; err != nil {
		t.Fatalf("insert post-rotation row: %v", err)
	}

	// The running application now uses the new service.
	service := NewLogService(database, newEnc)

	var logs []models.RequestLog
	if err := database.Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load logs: %v", err)
	}
	keyHashes := make([]string, 0, len(logs))
	for _, row := range logs {
		keyHashes = append(keyHashes, row.KeyHash)
	}
	resolved := service.ResolveKeyIdentifiers(keyHashes)
	identifierFor := func(row models.RequestLog) string {
		if identifier, ok := resolved[row.KeyHash]; ok {
			return identifier
		}
		return utils.KeyFingerprint(row.KeyHash)
	}

	// Every identifier a live key can legitimately claim, for cross-attribution
	// checking.
	liveIdentifiers := make(map[string]string)
	var liveKeys []models.APIKey
	if err := database.Find(&liveKeys).Error; err != nil {
		t.Fatalf("load live keys: %v", err)
	}
	for _, key := range liveKeys {
		plaintext, err := newEnc.Decrypt(key.KeyValue)
		if err != nil {
			t.Fatalf("decrypt live key: %v", err)
		}
		liveIdentifiers[utils.KeyIdentifier(plaintext, key.KeyHash)] = plaintext
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println(" Key lifecycle: what the identifier column shows in each state")
	fmt.Println("================================================================================")
	fmt.Printf("%-22s  %-26s  %-21s  %s\n", "LOG ROW", "KEY STATE", "COLUMN SHOWS", "KIND")
	fmt.Println(strings.Repeat("-", 96))

	expectations := map[string]struct {
		state    string
		resolves bool
	}{
		// Every row written before the rotation carries the pre-rotation hash, so
		// none of them resolve any more — including rows of keys that are still
		// present and untouched in key management.
		"life-survivor":       {"live key, pre-rotation row", false},
		"life-deleted":        {"deleted from key management", false},
		"life-rotated-before": {"live key, pre-rotation row", false},
		"life-rotated-after":  {"live key, post-rotation row", true},
		"life-historical":     {"never resolvable (hash only)", false},
	}

	for _, row := range logs {
		expectation, known := expectations[row.ID]
		if !known {
			t.Fatalf("unexpected log row %s", row.ID)
		}
		identifier := identifierFor(row)
		_, isMask := resolved[row.KeyHash]
		kind := "fingerprint"
		if isMask {
			kind = "masked identifier"
		}
		fmt.Printf("%-22s  %-26s  %-21s  %s\n", row.ID, expectation.state, identifier, kind)

		if isMask != expectation.resolves {
			t.Errorf("row %s resolved=%t, want %t (shows %q)", row.ID, isMask, expectation.resolves, identifier)
		}
		if !expectation.resolves {
			// The decisive check: an unresolvable row must not borrow any live key's
			// identifier, and must not look like a mask at all.
			if plaintext, claimed := liveIdentifiers[identifier]; claimed {
				t.Errorf("row %s shows %q, which is the identifier of live key %q -- misattribution",
					row.ID, identifier, plaintext)
			}
			if strings.Contains(identifier, utils.KeyMaskMarker) {
				t.Errorf("row %s fabricated a mask: %q", row.ID, identifier)
			}
			if want := utils.KeyFingerprint(row.KeyHash); identifier != want {
				t.Errorf("row %s = %q, want fingerprint %q", row.ID, identifier, want)
			}
		}
	}

	fmt.Println()
	fmt.Println("An ENCRYPTION_KEY rotation re-hashes api_keys but never request_logs")
	fmt.Println("(internal/commands/migrate.go), so EVERY pre-rotation row loses its mask --")
	fmt.Println("even for keys still present and untouched. Those rows show a fingerprint, never")
	fmt.Println("another key's mask, so the degradation costs recognizability, not correctness.")

	// Searching a live key's current identifier must return exactly the rows that
	// carry its current hash: post-rotation rows only.
	fmt.Println()
	fmt.Printf("%-30s  %-24s  %-5s  %s\n", "SEARCH INPUT", "KIND", "ROWS", "WHICH")
	fmt.Println(strings.Repeat("-", 84))

	survivorHashAfter := newEnc.Hash(survivorKey)
	rotatedHashAfterCheck := newEnc.Hash(rotatedKey)
	searchCases := []struct {
		input string
		kind  string
		want  int64
	}{
		{utils.KeyIdentifier(survivorKey, survivorHashAfter), "survivor, no post-rot row", 0},
		{utils.KeyIdentifier(rotatedKey, rotatedHashAfterCheck), "rotated key, post-rot row", 1},
		{utils.KeyFingerprint(survivorHash), "survivor pre-rotation fp", 1},
		{utils.KeyFingerprint(deletedHash), "deleted key fingerprint", 1},
		{utils.KeyFingerprint(historicalHash), "historical row fingerprint", 1},
	}
	for _, searchCase := range searchCases {
		var found []models.RequestLog
		if err := service.GetLogsQuery(LogFilter{KeyValue: searchCase.input}).
			Order("id asc").Find(&found).Error; err != nil {
			t.Fatalf("search %q: %v", searchCase.input, err)
		}
		ids := make([]string, 0, len(found))
		for _, row := range found {
			ids = append(ids, row.ID)
		}
		verdict := ""
		if int64(len(found)) != searchCase.want {
			verdict = fmt.Sprintf("  <-- WANT %d", searchCase.want)
			t.Errorf("search %q (%s) returned %d rows, want %d",
				searchCase.input, searchCase.kind, len(found), searchCase.want)
		}
		fmt.Printf("%-30s  %-24s  %-5d  %s%s\n",
			searchCase.input, searchCase.kind, len(found), strings.Join(ids, ","), verdict)
	}

	fmt.Println()
	fmt.Println("A pre-rotation row is still reachable by its fingerprint, which is why the")
	fmt.Println("fingerprint is retained rather than replaced by the mask.")
	fmt.Println()

	_ = rotatedHashBefore
}

// TestAuditEncryptionKeyRemovedWithoutMigrationStaysConsistent covers a
// misconfiguration rather than a supported path: ENCRYPTION_KEY is removed while
// api_keys still holds ciphertext and pre-existing hashes, so the log row's hash
// still matches but decryption is a no-op that hands back ciphertext.
//
// This state already mis-renders key management, which decrypts the same way. The
// property that matters here is that logs and key management stay consistent with
// each other, so an operator can still pair a row with a key row, and that one
// key's row can never display another key's value.
func TestAuditEncryptionKeyRemovedWithoutMigrationStaysConsistent(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.APIKey{}, &models.RequestLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	configuredEnc, err := encryption.NewService("configured-then-removed-key")
	if err != nil {
		t.Fatalf("create configured encryption service: %v", err)
	}
	const keyOne = "sk-misconfig-one-abcdefgh"
	const keyTwo = "sk-misconfig-two-ijklmnop"
	hashOne := seedKeyWithLogs(t, database, configuredEnc, keyOne, "misconfig-one", 400)
	hashTwo := seedKeyWithLogs(t, database, configuredEnc, keyTwo, "misconfig-two", 400)

	// ENCRYPTION_KEY removed; no migration run. Decrypt is now a no-op.
	noopEnc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("create noop encryption service: %v", err)
	}
	service := NewLogService(database, noopEnc)
	resolved := service.ResolveKeyIdentifiers([]string{hashOne, hashTwo})

	// What key management would render for the same rows: it decrypts with the same
	// service and the browser masks the result.
	keyManagement := make(map[string]string)
	var storedKeys []models.APIKey
	if err := database.Find(&storedKeys).Error; err != nil {
		t.Fatalf("load keys: %v", err)
	}
	for _, key := range storedKeys {
		asDisplayed, _ := noopEnc.Decrypt(key.KeyValue)
		keyManagement[key.KeyHash] = utils.MaskKeyIdentifier(asDisplayed)
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println(" Misconfiguration: ENCRYPTION_KEY removed without running the key migration")
	fmt.Println("================================================================================")
	fmt.Printf("%-18s  %-14s  %s\n", "LOG ROW", "KEY MGMT SHOWS", "LOG COLUMN SHOWS")
	fmt.Println(strings.Repeat("-", 70))
	for _, pair := range []struct{ row, hash string }{{"misconfig-one", hashOne}, {"misconfig-two", hashTwo}} {
		fmt.Printf("%-18s  %-14s  %s\n", pair.row, keyManagement[pair.hash], resolved[pair.hash])

		// Consistent with each other: the mask key management shows is still an
		// exact prefix of what the log column shows.
		if !strings.HasPrefix(resolved[pair.hash], keyManagement[pair.hash]) {
			t.Errorf("row %s: log column %q does not begin with key management's %q",
				pair.row, resolved[pair.hash], keyManagement[pair.hash])
		}
		// Neither screen may show the real key, since neither can decrypt it.
		for _, plaintext := range []string{keyOne, keyTwo} {
			if strings.Contains(resolved[pair.hash], plaintext) {
				t.Errorf("row %s leaked plaintext %q", pair.row, plaintext)
			}
		}
	}
	if resolved[hashOne] == resolved[hashTwo] {
		t.Errorf("two keys collapsed to the same identifier %q under misconfiguration", resolved[hashOne])
	}

	fmt.Println()
	fmt.Println("Both screens derive from the same failed decryption, so they still agree and")
	fmt.Println("the two keys stay distinct. The values are not the real key characters --")
	fmt.Println("a pre-existing consequence of this misconfiguration, which mis-renders key")
	fmt.Println("management identically; it is not introduced by the log identifier.")
	fmt.Println()
}
