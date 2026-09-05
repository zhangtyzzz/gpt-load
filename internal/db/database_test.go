package db

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type paramsFilter interface {
	ParamsFilter(ctx context.Context, sql string, params ...any) (string, []any)
}

func TestEnableIdleModeReleasesIdleConnections(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "idle-mode.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	if got := sqlDB.Stats().Idle; got == 0 {
		t.Fatal("test setup did not create an idle connection")
	}

	if err := EnableIdleMode(database); err != nil {
		t.Fatalf("EnableIdleMode: %v", err)
	}
	if got := sqlDB.Stats().Idle; got != 0 {
		t.Fatalf("idle connections after EnableIdleMode = %d, want 0", got)
	}
}

func TestDatabaseLoggerDoesNotRenderSQLParameters(t *testing.T) {
	for _, debug := range []bool{false, true} {
		databaseLogger := newDatabaseLogger(io.Discard, debug)

		filter, ok := databaseLogger.(paramsFilter)
		if !ok {
			t.Fatalf("GORM logger (debug=%t) does not expose parameter filtering", debug)
		}
		const secret = "auth-key-that-must-not-be-logged"
		sql, params := filter.ParamsFilter(context.Background(), "INSERT INTO settings(value) VALUES (?)", secret)
		if sql != "INSERT INTO settings(value) VALUES (?)" {
			t.Fatalf("SQL changed unexpectedly: %q", sql)
		}
		if len(params) != 0 {
			t.Fatalf("logger (debug=%t) retained %d SQL parameter(s), including sensitive values", debug, len(params))
		}
	}
}

func TestEnsureSQLiteDatabaseDirectoryUsesPrivatePermissions(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "nested", "database", "gpt-load.db")
	if err := ensureSQLiteDatabaseDirectory(databasePath); err != nil {
		t.Fatalf("ensureSQLiteDatabaseDirectory: %v", err)
	}

	info, err := os.Stat(filepath.Dir(databasePath))
	if err != nil {
		t.Fatalf("stat database directory: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions&0077 != 0 {
		t.Fatalf("database directory permissions = %04o, want no group or other access", permissions)
	}
}
