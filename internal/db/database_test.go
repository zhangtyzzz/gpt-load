package db

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type paramsFilter interface {
	ParamsFilter(ctx context.Context, sql string, params ...any) (string, []any)
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
