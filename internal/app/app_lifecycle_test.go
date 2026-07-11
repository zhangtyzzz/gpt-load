package app

import (
	"testing"

	"gpt-load/internal/store"
)

type clearGuardStore struct {
	store.Store
	clearCalls int
}

func (s *clearGuardStore) Clear() error {
	s.clearCalls++
	return nil
}

type fakeMasterKeyPoolLoader struct {
	store store.Store
	calls int
}

func (l *fakeMasterKeyPoolLoader) LoadKeysFromDB() error {
	l.calls++
	return l.store.ReplaceList("group:1:active_keys", 11, 12)
}

func TestMasterStartupStorageInitializationNeverClearsCoordinationState(t *testing.T) {
	memoryStore := store.NewMemoryStore()
	guardedStore := &clearGuardStore{Store: memoryStore}
	loader := &fakeMasterKeyPoolLoader{store: guardedStore}

	coordinationState := map[string]string{
		"request_log:pending": "buffered-log",
		"global_task":         "running-task",
		"affinity:session":    "key-11",
		"key:11:cooldown":     "cooling-down",
	}
	for key, value := range coordinationState {
		if err := guardedStore.Set(key, []byte(value), 0); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	if err := initializeMasterStorage(guardedStore, loader); err != nil {
		t.Fatalf("initialize master storage: %v", err)
	}
	if guardedStore.clearCalls != 0 {
		t.Fatalf("master startup called Store.Clear %d time(s)", guardedStore.clearCalls)
	}
	if loader.calls != 1 {
		t.Fatalf("LoadKeysFromDB calls = %d, want 1", loader.calls)
	}

	for key, want := range coordinationState {
		got, err := guardedStore.Get(key)
		if err != nil {
			t.Fatalf("coordination state %s was removed: %v", key, err)
		}
		if string(got) != want {
			t.Fatalf("coordination state %s = %q, want %q", key, got, want)
		}
	}
	if length, err := guardedStore.LLen("group:1:active_keys"); err != nil || length != 2 {
		t.Fatalf("derived active-key list length = %d, err = %v; want 2", length, err)
	}
}
