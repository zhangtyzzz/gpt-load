package keypool

import (
	"bytes"
	"errors"
	"fmt"
	"gpt-load/internal/encryption"
	"gpt-load/internal/errorpolicy"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"gpt-load/internal/types"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

func TestSecureRandomDuration(t *testing.T) {
	t.Run("bounded deterministic sample", func(t *testing.T) {
		const maximum = 100 * time.Millisecond
		jitter, err := secureRandomDuration(bytes.NewReader(make([]byte, 32)), maximum)
		if err != nil {
			t.Fatalf("secureRandomDuration: %v", err)
		}
		if jitter < 0 || jitter >= maximum {
			t.Fatalf("jitter = %v, want value in [0, %v)", jitter, maximum)
		}
	})

	t.Run("non-positive maximum needs no randomness", func(t *testing.T) {
		jitter, err := secureRandomDuration(failingRandomReader{}, 0)
		if err != nil || jitter != 0 {
			t.Fatalf("secureRandomDuration = (%v, %v), want (0, nil)", jitter, err)
		}
	})

	t.Run("reader failure is returned", func(t *testing.T) {
		jitter, err := secureRandomDuration(failingRandomReader{}, time.Second)
		if err == nil || jitter != 0 {
			t.Fatalf("secureRandomDuration = (%v, %v), want (0, error)", jitter, err)
		}
	})
}

func TestLoadKeysFromDBRebuildsListsWithoutClearingTransientState(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&models.Group{}, &models.APIKey{}); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}

	groups := []models.Group{
		{
			Name:               "active-group",
			GroupType:          "standard",
			Upstreams:          datatypes.JSON([]byte("[]")),
			ValidationEndpoint: "/v1/models",
			ChannelType:        "openai",
			TestModel:          "test-model",
		},
		{
			Name:               "empty-group",
			GroupType:          "standard",
			Upstreams:          datatypes.JSON([]byte("[]")),
			ValidationEndpoint: "/v1/models",
			ChannelType:        "openai",
			TestModel:          "test-model",
		},
	}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatalf("failed to create groups: %v", err)
	}

	apiKey := models.APIKey{
		GroupID:  groups[0].ID,
		KeyValue: "sk-rebuild-test",
		KeyHash:  "rebuild-test-hash",
		Status:   models.KeyStatusActive,
	}
	if err := db.Create(&apiKey).Error; err != nil {
		t.Fatalf("failed to create API key: %v", err)
	}

	memStore := store.NewMemoryStore()
	if err := memStore.LPush(fmt.Sprintf("group:%d:active_keys", groups[0].ID), 999); err != nil {
		t.Fatalf("failed to seed active group list: %v", err)
	}
	if err := memStore.LPush(fmt.Sprintf("group:%d:active_keys", groups[1].ID), 888); err != nil {
		t.Fatalf("failed to seed empty group list: %v", err)
	}
	preservedKeys := []string{
		"request_log:pending",
		"pending_log_keys",
		"global_task",
		"affinity:1:user",
		fmt.Sprintf("key:%d:cooldown", apiKey.ID),
	}
	for _, key := range preservedKeys {
		if err := memStore.Set(key, []byte("preserve"), time.Hour); err != nil {
			t.Fatalf("failed to seed preserved key %q: %v", key, err)
		}
	}

	encryptor, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("failed to create encryption service: %v", err)
	}
	provider := NewProvider(db, memStore, nil, encryptor, nil)
	if err := provider.LoadKeysFromDB(); err != nil {
		t.Fatalf("LoadKeysFromDB() error = %v", err)
	}

	if got, err := memStore.LLen(fmt.Sprintf("group:%d:active_keys", groups[0].ID)); err != nil || got != 1 {
		t.Fatalf("active group list length = %d, err = %v; want 1, nil", got, err)
	}
	selectedID, err := memStore.Rotate(fmt.Sprintf("group:%d:active_keys", groups[0].ID))
	if err != nil {
		t.Fatalf("failed to read rebuilt active list: %v", err)
	}
	if selectedID != strconv.FormatUint(uint64(apiKey.ID), 10) {
		t.Fatalf("rebuilt active key ID = %q, want %d", selectedID, apiKey.ID)
	}
	if got, err := memStore.LLen(fmt.Sprintf("group:%d:active_keys", groups[1].ID)); err != nil || got != 0 {
		t.Fatalf("empty group list length = %d, err = %v; want 0, nil", got, err)
	}
	for _, key := range preservedKeys {
		if exists, err := memStore.Exists(key); err != nil || !exists {
			t.Errorf("preserved key %q exists = %v, err = %v; want true, nil", key, exists, err)
		}
	}
}

func TestSelectKeyWithAffinityRejectsStaleHashOutsideActiveList(t *testing.T) {
	t.Parallel()

	memStore := store.NewMemoryStore()
	encryptor, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("failed to create encryption service: %v", err)
	}
	affinityManager := NewAffinityManager(memStore)
	provider := NewProvider(nil, memStore, nil, encryptor, affinityManager)

	const groupID uint = 7
	staleKey := &models.APIKey{ID: 11, GroupID: groupID, KeyValue: "sk-stale", Status: models.KeyStatusActive}
	activeKey := &models.APIKey{ID: 12, GroupID: groupID, KeyValue: "sk-active", Status: models.KeyStatusActive}
	if err := memStore.HSet(fmt.Sprintf("key:%d", staleKey.ID), provider.apiKeyToMap(staleKey)); err != nil {
		t.Fatalf("failed to seed stale key hash: %v", err)
	}
	if err := memStore.HSet(fmt.Sprintf("key:%d", activeKey.ID), provider.apiKeyToMap(activeKey)); err != nil {
		t.Fatalf("failed to seed active key hash: %v", err)
	}
	if err := memStore.LPush(fmt.Sprintf("group:%d:active_keys", groupID), activeKey.ID); err != nil {
		t.Fatalf("failed to seed active list: %v", err)
	}

	affinityHash := ComputeAffinityHash("sticky-user")
	if err := affinityManager.SetMapping(groupID, affinityHash, staleKey.ID, time.Hour); err != nil {
		t.Fatalf("failed to seed stale affinity mapping: %v", err)
	}

	selected, err := provider.SelectKeyWithAffinity(groupID, affinityHash)
	if err != nil {
		t.Fatalf("SelectKeyWithAffinity() error = %v", err)
	}
	if selected.ID != activeKey.ID {
		t.Fatalf("selected key ID = %d, want active-list key %d", selected.ID, activeKey.ID)
	}
}

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
