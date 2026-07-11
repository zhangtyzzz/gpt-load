package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gpt-load/internal/config"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"gpt-load/internal/utils"

	"gorm.io/gorm"
)

func seedHistoricalRequestLogs(t *testing.T, database *gorm.DB, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		entry := &models.RequestLog{
			ID:       fmt.Sprintf("legacy-%04d", index),
			KeyValue: fmt.Sprintf("legacy-secret-%04d", index),
			KeyHash:  fmt.Sprintf("hash-%04d", index),
		}
		if err := database.Create(entry).Error; err != nil {
			t.Fatalf("insert historical request log %d: %v", index, err)
		}
	}
}

func newStartedCleanupService(t *testing.T, database *gorm.DB) (*RequestLogService, *store.MemoryStore) {
	t.Helper()
	memoryStore := store.NewMemoryStore()
	service := NewRequestLogService(database, memoryStore, config.NewSystemSettingsManager())
	t.Cleanup(func() { _ = memoryStore.Close() })
	return service, memoryStore
}

func stopCleanupService(t *testing.T, service *RequestLogService) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	service.Stop(ctx)
	if ctx.Err() != nil {
		t.Fatalf("request log service did not stop before deadline: %v", ctx.Err())
	}
}

func TestHistoricalKeyCleanupBatchIsBounded(t *testing.T) {
	database := newRequestLogTestDB(t)
	seedHistoricalRequestLogs(t, database, 7)
	service := &RequestLogService{db: database}

	nextID, cleaned, done, err := service.purgeHistoricalKeyValueBatch(context.Background(), "", 3)
	if err != nil {
		t.Fatalf("purgeHistoricalKeyValueBatch: %v", err)
	}
	if cleaned != 3 {
		t.Fatalf("cleaned rows = %d, want 3", cleaned)
	}
	if done {
		t.Fatal("first bounded batch unexpectedly reported completion")
	}
	if nextID != "legacy-0002" {
		t.Fatalf("next cursor = %q, want legacy-0002", nextID)
	}

	var remaining int64
	if err := database.Model(&models.RequestLog{}).
		Where("key_value LIKE ?", "legacy-secret-%").
		Count(&remaining).Error; err != nil {
		t.Fatalf("count remaining legacy rows: %v", err)
	}
	if remaining != 4 {
		t.Fatalf("remaining legacy rows = %d, want 4", remaining)
	}
}

func TestHistoricalKeyCleanupCompletesAndPreservesFingerprint(t *testing.T) {
	database := newRequestLogTestDB(t)
	seedHistoricalRequestLogs(t, database, 7)
	const fingerprintHash = "0123456789abcdef0123456789abcdef"
	if err := database.Create(&models.RequestLog{
		ID:       "preserved-fingerprint",
		KeyValue: utils.KeyFingerprint(fingerprintHash),
		KeyHash:  fingerprintHash,
	}).Error; err != nil {
		t.Fatalf("insert fingerprint row: %v", err)
	}
	if err := database.Create(&models.RequestLog{ID: "preserved-empty", KeyValue: ""}).Error; err != nil {
		t.Fatalf("insert empty row: %v", err)
	}

	service := &RequestLogService{db: database, cleanupBatchSize: 2, cleanupBatchDelay: -1}
	cleaned, err := service.purgeHistoricalKeyValues(context.Background())
	if err != nil {
		t.Fatalf("purgeHistoricalKeyValues: %v", err)
	}
	if cleaned != 7 {
		t.Fatalf("cleaned rows = %d, want 7", cleaned)
	}

	var legacyRows []models.RequestLog
	if err := database.Where("id LIKE ?", "legacy-%").Find(&legacyRows).Error; err != nil {
		t.Fatalf("load cleaned legacy rows: %v", err)
	}
	for _, row := range legacyRows {
		if row.KeyValue != "" {
			t.Fatalf("legacy row %q retained reversible value %q", row.ID, row.KeyValue)
		}
	}

	var fingerprint models.RequestLog
	if err := database.First(&fingerprint, "id = ?", "preserved-fingerprint").Error; err != nil {
		t.Fatalf("load fingerprint row: %v", err)
	}
	if fingerprint.KeyValue != utils.KeyFingerprint(fingerprintHash) {
		t.Fatalf("fingerprint changed to %q", fingerprint.KeyValue)
	}
}

func TestHistoricalKeyCleanupAdvancesPastSparseSafePage(t *testing.T) {
	database := newRequestLogTestDB(t)
	const safeHash = "aaaaaaaaaaaabbbbbbbbbbbbcccccccccccc"
	rows := []models.RequestLog{
		{ID: "page-0000", KeyValue: utils.KeyFingerprint(safeHash), KeyHash: safeHash},
		{ID: "page-0001", KeyValue: ""},
		{ID: "page-0002", KeyValue: "fp:not-the-current-fingerprint", KeyHash: safeHash},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("insert sparse cleanup page: %v", err)
	}
	service := &RequestLogService{db: database}

	nextID, cleaned, done, err := service.purgeHistoricalKeyValueBatch(context.Background(), "", 2)
	if err != nil {
		t.Fatalf("purge safe page: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("safe page cleaned %d rows, want 0", cleaned)
	}
	if done {
		t.Fatal("full safe page unexpectedly reported completion")
	}
	if nextID != "page-0001" {
		t.Fatalf("safe-page cursor = %q, want page-0001", nextID)
	}

	_, cleaned, done, err = service.purgeHistoricalKeyValueBatch(context.Background(), nextID, 2)
	if err != nil {
		t.Fatalf("purge forged-fingerprint page: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("forged-fingerprint page cleaned %d rows, want 1", cleaned)
	}
	if !done {
		t.Fatal("short final page did not report completion")
	}

	var safeRow, forgedRow models.RequestLog
	if err := database.First(&safeRow, "id = ?", "page-0000").Error; err != nil {
		t.Fatalf("load safe fingerprint: %v", err)
	}
	if err := database.First(&forgedRow, "id = ?", "page-0002").Error; err != nil {
		t.Fatalf("load forged fingerprint: %v", err)
	}
	if safeRow.KeyValue != utils.KeyFingerprint(safeHash) {
		t.Fatalf("valid fingerprint changed to %q", safeRow.KeyValue)
	}
	if forgedRow.KeyValue != "" {
		t.Fatalf("forged fingerprint was retained: %q", forgedRow.KeyValue)
	}
}

func TestRequestLogServiceStartDoesNotWaitForHistoricalCleanup(t *testing.T) {
	database := newRequestLogTestDB(t)
	seedHistoricalRequestLogs(t, database, 1)
	service, _ := newStartedCleanupService(t, database)

	queryEntered := make(chan struct{})
	releaseQuery := make(chan struct{})
	var signalOnce sync.Once
	if err := database.Callback().Row().Before("gorm:row").Register(
		"test:block_historical_cleanup",
		func(tx *gorm.DB) {
			signalOnce.Do(func() { close(queryEntered) })
			<-releaseQuery
		},
	); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	startReturned := make(chan struct{})
	go func() {
		service.Start()
		close(startReturned)
	}()

	select {
	case <-startReturned:
	case <-time.After(2 * time.Second):
		close(releaseQuery)
		t.Fatal("Start blocked on historical cleanup")
	}
	select {
	case <-queryEntered:
	case <-time.After(2 * time.Second):
		close(releaseQuery)
		t.Fatal("historical cleanup did not start in background")
	}

	close(releaseQuery)
	stopCleanupService(t, service)
}

func TestRequestLogServiceStopCancelsCleanupDelay(t *testing.T) {
	database := newRequestLogTestDB(t)
	seedHistoricalRequestLogs(t, database, 2)
	service, _ := newStartedCleanupService(t, database)
	batchCompleted := make(chan int64, 1)
	service.cleanupBatchSize = 1
	service.cleanupBatchDelay = time.Hour
	service.cleanupBatchCompleted = batchCompleted
	service.Start()

	select {
	case cleaned := <-batchCompleted:
		if cleaned != 1 {
			t.Fatalf("first batch cleaned %d rows, want 1", cleaned)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first cleanup batch did not complete")
	}

	stopCtx, cancelStop := context.WithCancel(context.Background())
	stopReturned := make(chan struct{})
	go func() {
		service.Stop(stopCtx)
		close(stopReturned)
	}()
	select {
	case <-stopReturned:
		cancelStop()
	case <-time.After(2 * time.Second):
		cancelStop()
		<-stopReturned
		t.Fatal("Stop did not cancel the inter-batch cleanup delay")
	}
}

func TestHistoricalKeyCleanupRetriesTemporaryDatabaseFailure(t *testing.T) {
	database := newRequestLogTestDB(t)
	seedHistoricalRequestLogs(t, database, 1)
	service, _ := newStartedCleanupService(t, database)
	batchCompleted := make(chan int64, 1)
	service.cleanupBatchDelay = -1
	service.cleanupRetryMin = time.Millisecond
	service.cleanupRetryMax = 2 * time.Millisecond
	service.cleanupBatchCompleted = batchCompleted

	var failedOnce atomic.Bool
	if err := database.Callback().Row().Before("gorm:row").Register(
		"test:fail_first_historical_cleanup",
		func(tx *gorm.DB) {
			if failedOnce.CompareAndSwap(false, true) {
				tx.AddError(errors.New("temporary database failure"))
			}
		},
	); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	service.Start()
	select {
	case cleaned := <-batchCompleted:
		if cleaned != 1 {
			t.Fatalf("retry batch cleaned %d rows, want 1", cleaned)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not recover from temporary database failure")
	}
	stopCleanupService(t, service)
	if !failedOnce.Load() {
		t.Fatal("temporary failure callback was not exercised")
	}

	var stored models.RequestLog
	if err := database.First(&stored, "id = ?", "legacy-0000").Error; err != nil {
		t.Fatalf("load cleaned row: %v", err)
	}
	if stored.KeyValue != "" {
		t.Fatalf("retried cleanup left reversible key value %q", stored.KeyValue)
	}
}

func TestHistoricalKeyCleanupRetryResumesFromLastCursor(t *testing.T) {
	database := newRequestLogTestDB(t)
	seedHistoricalRequestLogs(t, database, 3)
	service := &RequestLogService{db: database, cleanupBatchSize: 1, cleanupBatchDelay: -1}

	var queryCount atomic.Int64
	if err := database.Callback().Row().Before("gorm:row").Register(
		"test:fail_third_historical_cleanup_query",
		func(tx *gorm.DB) {
			if queryCount.Add(1) == 3 {
				tx.AddError(errors.New("temporary late database failure"))
			}
		},
	); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	cleaned, cursor, err := service.purgeHistoricalKeyValuesFrom(context.Background(), "")
	if err == nil {
		t.Fatalf("late database failure was not returned; query count = %d", queryCount.Load())
	}
	if cleaned != 2 || cursor != "legacy-0001" {
		t.Fatalf("first sweep = (%d, %q), want (2, legacy-0001)", cleaned, cursor)
	}

	cleaned, cursor, err = service.purgeHistoricalKeyValuesFrom(context.Background(), cursor)
	if err != nil {
		t.Fatalf("resume cleanup: %v", err)
	}
	if cleaned != 1 || cursor != "legacy-0002" {
		t.Fatalf("resumed sweep = (%d, %q), want (1, legacy-0002)", cleaned, cursor)
	}
	if got := queryCount.Load(); got != 5 {
		t.Fatalf("query count = %d, want 5; cleanup appears to have rescanned prior pages", got)
	}
}
