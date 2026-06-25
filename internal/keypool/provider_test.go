package keypool

import (
	"errors"
	"fmt"
	"gpt-load/internal/encryption"
	"gpt-load/internal/errorpolicy"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"gpt-load/internal/types"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestApplyHealthActionNoop(t *testing.T) {
	provider, memStore, db := newHealthActionTestProvider(t)
	group := healthActionTestGroup(3)
	apiKey := createHealthActionTestKey(t, provider, db, group.ID, 0)

	if err := provider.ApplyHealthAction(apiKey, group, errorpolicy.HealthActionNoop, 0); err != nil {
		t.Fatalf("ApplyHealthAction noop returned error: %v", err)
	}

	assertHealthActionKeyState(t, provider, db, apiKey.ID, models.KeyStatusActive, 0)
	assertActiveKeyCount(t, memStore, group.ID, 1)
	if cooling, err := provider.IsCoolingDown(apiKey.ID); err != nil || cooling {
		t.Fatalf("coolingDown = %v, err = %v; want false, nil", cooling, err)
	}
}

func TestApplyHealthActionFailCountIncrement(t *testing.T) {
	provider, memStore, db := newHealthActionTestProvider(t)
	group := healthActionTestGroup(3)
	apiKey := createHealthActionTestKey(t, provider, db, group.ID, 0)

	if err := provider.ApplyHealthAction(apiKey, group, errorpolicy.HealthActionFailCountInc, 0); err != nil {
		t.Fatalf("ApplyHealthAction fail_count_inc returned error: %v", err)
	}

	assertHealthActionKeyState(t, provider, db, apiKey.ID, models.KeyStatusActive, 1)
	assertActiveKeyCount(t, memStore, group.ID, 1)
}

func TestApplyHealthActionFailCountIncrementCanReachBlacklistThreshold(t *testing.T) {
	provider, memStore, db := newHealthActionTestProvider(t)
	group := healthActionTestGroup(2)
	apiKey := createHealthActionTestKey(t, provider, db, group.ID, 1)

	if err := provider.ApplyHealthAction(apiKey, group, errorpolicy.HealthActionFailCountInc, 0); err != nil {
		t.Fatalf("ApplyHealthAction fail_count_inc returned error: %v", err)
	}

	assertHealthActionKeyState(t, provider, db, apiKey.ID, models.KeyStatusInvalid, 2)
	assertActiveKeyCount(t, memStore, group.ID, 0)
	if _, err := provider.SelectKey(group.ID); !errors.Is(err, app_errors.ErrNoActiveKeys) {
		t.Fatalf("SelectKey error = %v, want ErrNoActiveKeys", err)
	}
}

func TestApplyHealthActionCooldown(t *testing.T) {
	provider, memStore, db := newHealthActionTestProvider(t)
	group := healthActionTestGroup(3)
	apiKey := createHealthActionTestKey(t, provider, db, group.ID, 0)

	if err := provider.ApplyHealthAction(apiKey, group, errorpolicy.HealthActionCooldown, time.Minute); err != nil {
		t.Fatalf("ApplyHealthAction cooldown returned error: %v", err)
	}

	assertHealthActionKeyState(t, provider, db, apiKey.ID, models.KeyStatusActive, 0)
	assertActiveKeyCount(t, memStore, group.ID, 1)
	if cooling, err := provider.IsCoolingDown(apiKey.ID); err != nil || !cooling {
		t.Fatalf("coolingDown = %v, err = %v; want true, nil", cooling, err)
	}
	if _, err := provider.SelectKey(group.ID); !errors.Is(err, app_errors.ErrNoActiveKeys) {
		t.Fatalf("SelectKey error = %v, want ErrNoActiveKeys while only key is cooling down", err)
	}
}

func TestApplyHealthActionBlacklistNow(t *testing.T) {
	provider, memStore, db := newHealthActionTestProvider(t)
	group := healthActionTestGroup(3)
	apiKey := createHealthActionTestKey(t, provider, db, group.ID, 0)

	if err := provider.ApplyHealthAction(apiKey, group, errorpolicy.HealthActionBlacklistNow, 0); err != nil {
		t.Fatalf("ApplyHealthAction blacklist_now returned error: %v", err)
	}

	assertHealthActionKeyState(t, provider, db, apiKey.ID, models.KeyStatusInvalid, 0)
	assertActiveKeyCount(t, memStore, group.ID, 0)
	if _, err := provider.SelectKey(group.ID); !errors.Is(err, app_errors.ErrNoActiveKeys) {
		t.Fatalf("SelectKey error = %v, want ErrNoActiveKeys", err)
	}
}

func newHealthActionTestProvider(t *testing.T) (*KeyProvider, *store.MemoryStore, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}

	memStore := store.NewMemoryStore()
	encryptor, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("failed to create encryption service: %v", err)
	}

	return NewProvider(db, memStore, nil, encryptor, nil), memStore, db
}

func healthActionTestGroup(blacklistThreshold int) *models.Group {
	return &models.Group{
		ID: 1,
		EffectiveConfig: types.SystemSettings{
			BlacklistThreshold: blacklistThreshold,
		},
	}
}

func createHealthActionTestKey(t *testing.T, provider *KeyProvider, db *gorm.DB, groupID uint, failureCount int64) *models.APIKey {
	t.Helper()

	apiKey := &models.APIKey{
		GroupID:      groupID,
		KeyValue:     fmt.Sprintf("sk-test-%d", time.Now().UnixNano()),
		Status:       models.KeyStatusActive,
		FailureCount: failureCount,
	}
	if err := db.Create(apiKey).Error; err != nil {
		t.Fatalf("failed to create API key: %v", err)
	}
	if err := provider.addKeyToStore(apiKey); err != nil {
		t.Fatalf("failed to add API key to store: %v", err)
	}

	return apiKey
}

func assertHealthActionKeyState(t *testing.T, provider *KeyProvider, db *gorm.DB, keyID uint, wantStatus string, wantFailureCount int64) {
	t.Helper()

	var dbKey models.APIKey
	if err := db.First(&dbKey, keyID).Error; err != nil {
		t.Fatalf("failed to load API key from DB: %v", err)
	}
	if dbKey.Status != wantStatus {
		t.Fatalf("DB status = %s, want %s", dbKey.Status, wantStatus)
	}
	if dbKey.FailureCount != wantFailureCount {
		t.Fatalf("DB failure count = %d, want %d", dbKey.FailureCount, wantFailureCount)
	}

	storeKey, err := provider.getKeyByID(keyID)
	if err != nil {
		t.Fatalf("failed to load API key from store: %v", err)
	}
	if storeKey.Status != wantStatus {
		t.Fatalf("store status = %s, want %s", storeKey.Status, wantStatus)
	}
	if storeKey.FailureCount != wantFailureCount {
		t.Fatalf("store failure count = %d, want %d", storeKey.FailureCount, wantFailureCount)
	}
}

func assertActiveKeyCount(t *testing.T, memStore *store.MemoryStore, groupID uint, want int64) {
	t.Helper()

	got, err := memStore.LLen(fmt.Sprintf("group:%d:active_keys", groupID))
	if err != nil {
		t.Fatalf("failed to get active key list length: %v", err)
	}
	if got != want {
		t.Fatalf("active key list length = %d, want %d", got, want)
	}
}
