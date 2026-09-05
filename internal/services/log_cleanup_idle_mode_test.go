package services

import (
	"context"
	"testing"
	"time"

	"gpt-load/internal/config"
	"gpt-load/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestActivityDrivenLogCleanupWaitsForRealTraffic(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&models.RequestLog{}); err != nil {
		t.Fatalf("migrate request logs: %v", err)
	}
	entry := &models.RequestLog{ID: "expired-idle-mode-log", Timestamp: time.Now().AddDate(0, 0, -8)}
	if err := database.Create(entry).Error; err != nil {
		t.Fatalf("insert expired request log: %v", err)
	}

	service := NewLogCleanupService(database, config.NewSystemSettingsManager())
	service.StartActivityDriven()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		service.Stop(ctx)
	})

	time.Sleep(25 * time.Millisecond)
	var before int64
	if err := database.Model(&models.RequestLog{}).Where("id = ?", entry.ID).Count(&before).Error; err != nil {
		t.Fatalf("count request log before activity: %v", err)
	}
	if before != 1 {
		t.Fatalf("cleanup ran without activity; remaining rows = %d", before)
	}

	service.NotifyActivity()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var remaining int64
		if err := database.Model(&models.RequestLog{}).Where("id = ?", entry.ID).Count(&remaining).Error; err != nil {
			t.Fatalf("count request log after activity: %v", err)
		}
		if remaining == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expired request log was not cleaned after activity")
}
