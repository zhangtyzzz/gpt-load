package keypool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gpt-load/internal/config"
	"gpt-load/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestActivityDrivenCronCheckerDoesNotPollUntilNotified(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.Group{}); err != nil {
		t.Fatalf("migrate groups: %v", err)
	}

	var queryCount atomic.Int64
	if err := database.Callback().Query().Before("gorm:query").Register("test:count_queries", func(*gorm.DB) {
		queryCount.Add(1)
	}); err != nil {
		t.Fatalf("register query counter: %v", err)
	}

	checker := NewCronChecker(database, config.NewSystemSettingsManager(), nil, nil)
	checker.StartActivityDriven()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		checker.Stop(ctx)
	})

	time.Sleep(25 * time.Millisecond)
	if got := queryCount.Load(); got != 0 {
		t.Fatalf("activity-driven checker queried before notification: %d", got)
	}

	checker.NotifyActivity()
	deadline := time.Now().Add(time.Second)
	for queryCount.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := queryCount.Load(); got != 1 {
		t.Fatalf("queries after first activity = %d, want 1", got)
	}

	checker.NotifyActivity()
	time.Sleep(25 * time.Millisecond)
	if got := queryCount.Load(); got != 1 {
		t.Fatalf("five-minute throttle allowed an extra query: %d", got)
	}
}
