package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"
)

// hashFor mirrors encryption.aesService.Hash (HMAC-SHA256, hex encoded) without
// importing the encryption package, which would be an import cycle.
func hashFor(secret []byte, plaintext string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// identifierWithDiscriminatorLength builds a display identifier using an
// explicit discriminator width, so the effect of the width can be measured
// independently of the current production constant.
func identifierWithDiscriminatorLength(key, keyHash string, length int) string {
	mask := MaskKeyIdentifier(key)
	discriminator := strings.ToLower(keyHash)
	if len(discriminator) > length {
		discriminator = discriminator[:length]
	}
	return mask + KeyIdentifierSeparator + discriminator
}

// TestKeyIdentifierUniquenessAtGroupScale answers whether the discriminator
// itself collides, at the scale where mask collisions were shown to be likely.
//
// Every key here shares the head "sk-p" and the tail "9z7q", so the bare mask is
// identical for all of them and the whole burden of telling them apart falls on
// the discriminator. That is the worst realistic case, and not a contrived one:
// keys minted by one provider share a prefix and often a fixed-width format.
func TestKeyIdentifierUniquenessAtGroupScale(t *testing.T) {
	secret := []byte("group-scale-uniqueness-secret-key")

	// The scale flagged as ordinary for a key-rotation gateway.
	const keyCount = 5000

	keys := make([]string, 0, keyCount)
	hashes := make([]string, 0, keyCount)
	for index := range keyCount {
		// Distinct in the middle, identical in the first and last four characters.
		key := fmt.Sprintf("sk-p%013d9z7q", index)
		keys = append(keys, key)
		hashes = append(hashes, hashFor(secret, key))
	}

	// Sanity: the fixture really does collapse to one mask.
	masks := make(map[string]struct{})
	for _, key := range keys {
		masks[MaskKeyIdentifier(key)] = struct{}{}
	}
	if len(masks) != 1 {
		t.Fatalf("fixture produced %d distinct masks, want 1", len(masks))
	}

	type result struct {
		width           int
		distinct        int
		keysInCollision int
		space           float64
		expectedPairs   float64
	}

	widths := []int{4, 8, keyFingerprintLength}
	results := make([]result, 0, len(widths))

	for _, width := range widths {
		byIdentifier := make(map[string][]string, keyCount)
		for i, key := range keys {
			identifier := identifierWithDiscriminatorLength(key, hashes[i], width)
			byIdentifier[identifier] = append(byIdentifier[identifier], key)
		}

		keysInCollision := 0
		for _, sharing := range byIdentifier {
			if len(sharing) > 1 {
				keysInCollision += len(sharing)
			}
		}

		space := math.Pow(16, float64(width))
		results = append(results, result{
			width:           width,
			distinct:        len(byIdentifier),
			keysInCollision: keysInCollision,
			space:           space,
			expectedPairs:   float64(keyCount) * float64(keyCount-1) / 2 / space,
		})
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Printf(" Identifier uniqueness with %d keys that all share one bare mask\n", keyCount)
	fmt.Println("================================================================================")
	fmt.Printf("%-12s  %-18s  %-12s  %-18s  %s\n",
		"HEX WIDTH", "DISCRIM. SPACE", "DISTINCT", "KEYS COLLIDING", "EXPECTED PAIRS")
	fmt.Println(strings.Repeat("-", 84))
	for _, r := range results {
		fmt.Printf("%-12d  %-18.0f  %-12d  %-18d  %.3f\n",
			r.width, r.space, r.distinct, r.keysInCollision, r.expectedPairs)
	}

	byWidth := make(map[int]result, len(results))
	for _, r := range results {
		byWidth[r.width] = r
	}

	// Measured, not assumed: four hex characters are not enough at this scale.
	// 5000 keys into 65536 slots collide by the birthday bound, and the fixture
	// shows it happening rather than merely being possible.
	if byWidth[4].keysInCollision == 0 {
		t.Errorf("expected four hex characters to collide at %d keys; fixture may be wrong", keyCount)
	}

	// The production width must leave the identifier as unique as the fingerprint
	// the API has always published: no two keys may share an identifier.
	production := byWidth[keyFingerprintLength]
	if production.keysInCollision != 0 {
		t.Errorf("production discriminator width collided for %d keys", production.keysInCollision)
	}
	if production.distinct != keyCount {
		t.Errorf("production width produced %d distinct identifiers for %d keys",
			production.distinct, keyCount)
	}

	fmt.Println()
	fmt.Printf("At four hex characters, %d of %d keys share an identifier with another key.\n",
		byWidth[4].keysInCollision, keyCount)
	fmt.Printf("At the production width (%d), all %d identifiers are distinct: the identifier\n",
		keyFingerprintLength, keyCount)
	fmt.Println("carries the whole fingerprint body, so it is exactly as unique as the")
	fmt.Println("fingerprint itself and introduces no collision risk of its own.")
	fmt.Println()
}

// TestKeyIdentifierCarriesWholeFingerprintBody pins the property the uniqueness
// argument rests on: the discriminator is not a shortened hash, it is the same
// hash prefix the fingerprint publishes.
func TestKeyIdentifierCarriesWholeFingerprintBody(t *testing.T) {
	secret := []byte("fingerprint-body-secret")
	const key = "sk-proj-abcdefghijklmnop"
	keyHash := hashFor(secret, key)

	identifier := KeyIdentifier(key, keyHash)
	fingerprint := KeyFingerprint(keyHash)
	fingerprintBody := strings.TrimPrefix(fingerprint, "fp:")

	if !strings.HasSuffix(identifier, KeyIdentifierSeparator+fingerprintBody) {
		t.Fatalf("identifier %q does not end with the fingerprint body %q", identifier, fingerprintBody)
	}
	if !strings.HasPrefix(identifier, MaskKeyIdentifier(key)) {
		t.Fatalf("identifier %q does not begin with the mask %q", identifier, MaskKeyIdentifier(key))
	}

	// Two identifiers can only collide if the fingerprints do.
	_, _, hashPrefix, ok := ParseKeyIdentifier(identifier)
	if !ok {
		t.Fatalf("ParseKeyIdentifier rejected %q", identifier)
	}
	if hashPrefix != fingerprintBody {
		t.Fatalf("parsed hash prefix %q, want the whole fingerprint body %q", hashPrefix, fingerprintBody)
	}
}
