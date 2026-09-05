package config

import "testing"

func TestDatabaseIdleModeDefaultsOffAndCanBeEnabled(t *testing.T) {
	t.Setenv("AUTH_KEY", "manager-test-admin-key-that-is-not-production")
	t.Setenv("DATABASE_IDLE_MODE", "")

	manager, err := NewManager(NewSystemSettingsManager())
	if err != nil {
		t.Fatalf("NewManager with default idle mode: %v", err)
	}
	if manager.GetDatabaseConfig().IdleMode {
		t.Fatal("database idle mode defaulted to true")
	}

	t.Setenv("DATABASE_IDLE_MODE", "true")
	if err := manager.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig with idle mode enabled: %v", err)
	}
	if !manager.GetDatabaseConfig().IdleMode {
		t.Fatal("database idle mode was not enabled")
	}
}
