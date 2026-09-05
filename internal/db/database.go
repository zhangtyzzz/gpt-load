package db

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gpt-load/internal/types"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func ensureSQLiteDatabaseDirectory(dsn string) error {
	if err := os.MkdirAll(filepath.Dir(dsn), 0700); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}
	return nil
}

func NewDB(configManager types.ConfigManager) (*gorm.DB, error) {
	dbConfig := configManager.GetDatabaseConfig()
	dsn := dbConfig.DSN
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_DSN is not configured")
	}

	newLogger := newDatabaseLogger(os.Stdout, configManager.GetLogConfig().Level == "debug")

	var dialector gorm.Dialector
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		dialector = postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true,
		})
	} else if strings.Contains(dsn, "@tcp") {
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		dialector = mysql.Open(dsn)
	} else {
		if err := ensureSQLiteDatabaseDirectory(dsn); err != nil {
			return nil, err
		}
		dialector = sqlite.Open(dsn + "?_busy_timeout=5000")
	}

	var err error
	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger:      newLogger,
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	// Set connection pool parameters for all drivers
	sqlDB.SetMaxIdleConns(50)
	sqlDB.SetMaxOpenConns(500)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return DB, nil
}

// EnableIdleMode prevents database/sql from retaining idle connections. This
// lets serverless databases suspend after the last real application request.
// It is intentionally applied after startup migrations and cache hydration so
// those operations can still reuse a connection efficiently.
func EnableIdleMode(database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB for idle mode: %w", err)
	}
	sqlDB.SetMaxIdleConns(0)
	return nil
}

func newDatabaseLogger(writer io.Writer, debug bool) logger.Interface {
	logLevel := logger.Warn
	slowThreshold := 200 * time.Millisecond
	ignoreRecordNotFound := false
	if debug {
		logLevel = logger.Info
		slowThreshold = time.Second
		ignoreRecordNotFound = true
	}
	return logger.New(
		log.New(writer, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             slowThreshold,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: ignoreRecordNotFound,
			ParameterizedQueries:      true, // Never render credentials or other values in SQL logs.
			Colorful:                  true,
		},
	)
}
