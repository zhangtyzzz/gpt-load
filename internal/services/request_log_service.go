package services

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"gpt-load/internal/config"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"gpt-load/internal/utils"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RequestLogCachePrefix    = "request_log:"
	PendingLogKeysSet        = "pending_log_keys"
	DefaultLogFlushBatchSize = 200

	// HistoricalKeyCleanupBatchSize stays below SQLite's commonly deployed
	// parameter limit while still amortizing the two statements per batch.
	HistoricalKeyCleanupBatchSize = 500
	HistoricalKeyCleanupDelay     = 25 * time.Millisecond
	HistoricalKeyCleanupRetryMin  = 250 * time.Millisecond
	HistoricalKeyCleanupRetryMax  = 5 * time.Second
)

// RequestLogService is responsible for managing request logs.
type RequestLogService struct {
	db              *gorm.DB
	store           store.Store
	settingsManager *config.SystemSettingsManager
	wg              sync.WaitGroup
	ticker          *time.Ticker

	lifecycleOnce   sync.Once
	startOnce       sync.Once
	stopOnce        sync.Once
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc

	// These knobs keep cleanup deterministic in tests without changing the
	// production defaults. cleanupBatchCompleted must never be required for
	// progress; notifications are therefore best-effort.
	cleanupBatchSize      int
	cleanupBatchDelay     time.Duration
	cleanupRetryMin       time.Duration
	cleanupRetryMax       time.Duration
	cleanupBatchCompleted chan<- int64
}

// NewRequestLogService creates a new RequestLogService instance
func NewRequestLogService(db *gorm.DB, store store.Store, sm *config.SystemSettingsManager) *RequestLogService {
	return &RequestLogService{
		db:              db,
		store:           store,
		settingsManager: sm,
	}
}

// Start initializes the service and starts the periodic flush routine
func (s *RequestLogService) Start() {
	s.initLifecycle()
	s.startOnce.Do(func() {
		// Legacy cleanup is deliberately independent from the request-log flush
		// loop. In particular, Start must not block application readiness on a
		// table-wide write lock for a large request_logs table.
		s.wg.Add(2)
		go s.runLoop(s.lifecycleCtx)
		go s.runHistoricalKeyValueCleanup(s.lifecycleCtx)
	})
}

func (s *RequestLogService) initLifecycle() {
	s.lifecycleOnce.Do(func() {
		// #nosec G118 -- lifecycleCancel is retained on the service and invoked exactly once by Stop through stopOnce.
		s.lifecycleCtx, s.lifecycleCancel = context.WithCancel(context.Background())
	})
}

func (s *RequestLogService) historicalCleanupBatchSize() int {
	if s.cleanupBatchSize > 0 {
		return s.cleanupBatchSize
	}
	return HistoricalKeyCleanupBatchSize
}

func (s *RequestLogService) historicalCleanupBatchDelay() time.Duration {
	if s.cleanupBatchDelay != 0 {
		return s.cleanupBatchDelay
	}
	return HistoricalKeyCleanupDelay
}

func (s *RequestLogService) historicalCleanupRetryBounds() (time.Duration, time.Duration) {
	minimum := s.cleanupRetryMin
	if minimum <= 0 {
		minimum = HistoricalKeyCleanupRetryMin
	}
	maximum := s.cleanupRetryMax
	if maximum <= 0 {
		maximum = HistoricalKeyCleanupRetryMax
	}
	if maximum < minimum {
		maximum = minimum
	}
	return minimum, maximum
}

// purgeHistoricalKeyValueBatch first reads a bounded, stable page of rows and
// then updates only legacy values from that page. Paging the primary key rather
// than filtering key_value in SQL keeps every read bounded even after almost
// the entire table has already been cleaned and key_value has no index. This
// two-statement form works on SQLite, MySQL, and PostgreSQL without
// vendor-specific UPDATE LIMIT syntax.
func (s *RequestLogService) purgeHistoricalKeyValueBatch(
	ctx context.Context,
	afterID string,
	batchSize int,
) (nextID string, cleaned int64, done bool, err error) {
	if batchSize <= 0 {
		batchSize = HistoricalKeyCleanupBatchSize
	}

	query := s.db.WithContext(ctx).Model(&models.RequestLog{})
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}

	type cleanupRow struct {
		ID       string
		KeyValue string
		KeyHash  string
	}
	rows := make([]cleanupRow, 0, batchSize)
	if err := query.
		Select("id", "key_value", "key_hash").
		Order("id ASC").
		Limit(batchSize).
		Scan(&rows).Error; err != nil {
		return afterID, 0, false, fmt.Errorf("select historical request-log batch: %w", err)
	}
	if len(rows) == 0 {
		return afterID, 0, true, nil
	}

	nextID = rows[len(rows)-1].ID
	legacyIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.KeyValue != "" && row.KeyValue != utils.KeyFingerprint(row.KeyHash) {
			legacyIDs = append(legacyIDs, row.ID)
		}
	}
	if len(legacyIDs) == 0 {
		return nextID, 0, len(rows) < batchSize, nil
	}

	result := s.db.WithContext(ctx).
		Model(&models.RequestLog{}).
		Where("id IN ?", legacyIDs).
		Update("key_value", "")
	if result.Error != nil {
		return afterID, 0, false, fmt.Errorf("update historical request-log batch: %w", result.Error)
	}

	return nextID, result.RowsAffected, len(rows) < batchSize, nil
}

// purgeHistoricalKeyValues completes one bounded sweep. It returns the count
// already cleaned even if a later batch fails, allowing the retry loop to keep
// accurate aggregate progress without logging row contents.
func (s *RequestLogService) purgeHistoricalKeyValues(ctx context.Context) (int64, error) {
	total, _, err := s.purgeHistoricalKeyValuesFrom(ctx, "")
	return total, err
}

// purgeHistoricalKeyValuesFrom returns the last fully processed cursor even
// when a later batch fails. The background retry loop can therefore resume at
// the failed page instead of rescanning a large prefix of the table.
func (s *RequestLogService) purgeHistoricalKeyValuesFrom(
	ctx context.Context,
	afterID string,
) (total int64, lastID string, err error) {
	lastID = afterID
	batchSize := s.historicalCleanupBatchSize()

	for {
		nextID, cleaned, done, err := s.purgeHistoricalKeyValueBatch(ctx, lastID, batchSize)
		total += cleaned
		if err != nil {
			return total, lastID, err
		}
		if cleaned > 0 && s.cleanupBatchCompleted != nil {
			select {
			case s.cleanupBatchCompleted <- cleaned:
			default:
			}
		}
		lastID = nextID
		if done {
			return total, lastID, nil
		}
		if !waitForCleanup(ctx, s.historicalCleanupBatchDelay()) {
			return total, lastID, ctx.Err()
		}
	}
}

func (s *RequestLogService) runHistoricalKeyValueCleanup(ctx context.Context) {
	defer s.wg.Done()

	minimumRetryDelay, maximumRetryDelay := s.historicalCleanupRetryBounds()
	retryDelay := minimumRetryDelay
	var totalCleaned int64
	var retryCount int64
	cursor := ""

	for {
		cleaned, nextCursor, err := s.purgeHistoricalKeyValuesFrom(ctx, cursor)
		totalCleaned += cleaned
		cursor = nextCursor
		if err == nil {
			logrus.WithField("cleaned_count", totalCleaned).
				Info("Historical request-log credential cleanup completed")
			return
		}
		if ctx.Err() != nil {
			return
		}
		if cleaned > 0 {
			retryDelay = minimumRetryDelay
		}

		retryCount++
		// Do not attach the database error: some drivers include statement
		// parameters in error strings. Cleanup logs intentionally expose counts
		// only and retry from the remaining eligible rows.
		logrus.WithFields(logrus.Fields{
			"cleaned_count": totalCleaned,
			"retry_count":   retryCount,
		}).Warn("Historical request-log credential cleanup will retry")

		if !waitForCleanup(ctx, retryDelay) {
			return
		}
		if retryDelay < maximumRetryDelay {
			retryDelay *= 2
			if retryDelay > maximumRetryDelay {
				retryDelay = maximumRetryDelay
			}
		}
	}
}

func waitForCleanup(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		runtime.Gosched()
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *RequestLogService) runLoop(ctx context.Context) {
	defer s.wg.Done()

	// Initial flush on start
	s.flush()

	interval := time.Duration(s.settingsManager.GetSettings().RequestLogWriteIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Minute
	}
	s.ticker = time.NewTicker(interval)
	defer s.ticker.Stop()

	for {
		select {
		case <-s.ticker.C:
			newInterval := time.Duration(s.settingsManager.GetSettings().RequestLogWriteIntervalMinutes) * time.Minute
			if newInterval <= 0 {
				newInterval = time.Minute
			}
			if newInterval != interval {
				s.ticker.Reset(newInterval)
				interval = newInterval
				logrus.Debugf("Request log write interval updated to: %v", interval)
			}
			s.flush()
		case <-ctx.Done():
			return
		}
	}
}

// Stop gracefully stops the RequestLogService
func (s *RequestLogService) Stop(ctx context.Context) {
	s.initLifecycle()
	s.stopOnce.Do(s.lifecycleCancel)

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.flush()
		logrus.Info("RequestLogService stopped gracefully.")
	case <-ctx.Done():
		logrus.Warn("RequestLogService stop timed out.")
	}
}

// Record logs a request to the database and cache
func (s *RequestLogService) Record(log *models.RequestLog) error {
	log.ID = uuid.NewString()
	log.Timestamp = time.Now()
	sanitizeRequestLog(log)

	if s.settingsManager.GetSettings().RequestLogWriteIntervalMinutes == 0 {
		return s.writeLogsToDB([]*models.RequestLog{log})
	}

	cacheKey := RequestLogCachePrefix + log.ID

	logBytes, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal request log: %w", err)
	}

	ttl := time.Duration(s.settingsManager.GetSettings().RequestLogWriteIntervalMinutes*5) * time.Minute
	if err := s.store.Set(cacheKey, logBytes, ttl); err != nil {
		return err
	}

	return s.store.SAdd(PendingLogKeysSet, cacheKey)
}

// flush data from cache to database
func (s *RequestLogService) flush() {
	if s.settingsManager.GetSettings().RequestLogWriteIntervalMinutes == 0 {
		logrus.Debug("Sync mode enabled, skipping scheduled log flush.")
		return
	}

	logrus.Debug("Master starting to flush request logs...")

	for {
		keys, err := s.store.SPopN(PendingLogKeysSet, DefaultLogFlushBatchSize)
		if err != nil {
			logrus.Errorf("Failed to pop pending log keys from store: %v", err)
			return
		}

		if len(keys) == 0 {
			return
		}

		logrus.Debugf("Popped %d request logs to flush.", len(keys))

		var logs []*models.RequestLog
		var processedKeys []string
		for _, key := range keys {
			logBytes, err := s.store.Get(key)
			if err != nil {
				if err == store.ErrNotFound {
					logrus.Warnf("Log key %s found in set but not in store, skipping.", key)
				} else {
					logrus.Warnf("Failed to get log for key %s: %v", key, err)
				}
				continue
			}
			var log models.RequestLog
			if err := json.Unmarshal(logBytes, &log); err != nil {
				logrus.Warnf("Failed to unmarshal log for key %s: %v", key, err)
				continue
			}
			logs = append(logs, &log)
			processedKeys = append(processedKeys, key)
		}

		if len(logs) == 0 {
			continue
		}

		err = s.writeLogsToDB(logs)

		if err != nil {
			logrus.Errorf("Failed to flush request logs batch, will retry next time. Error: %v", err)
			if len(keys) > 0 {
				keysToRetry := make([]any, len(keys))
				for i, k := range keys {
					keysToRetry[i] = k
				}
				if saddErr := s.store.SAdd(PendingLogKeysSet, keysToRetry...); saddErr != nil {
					logrus.Errorf("CRITICAL: Failed to re-add failed log keys to set: %v", saddErr)
				}
			}
			return
		}

		if len(processedKeys) > 0 {
			if err := s.store.Del(processedKeys...); err != nil {
				logrus.Errorf("Failed to delete flushed log bodies from store: %v", err)
			}
		}
		logrus.Infof("Successfully flushed %d request logs.", len(logs))
	}
}

// writeLogsToDB writes a batch of request logs to the database
func (s *RequestLogService) writeLogsToDB(logs []*models.RequestLog) error {
	if len(logs) == 0 {
		return nil
	}
	// This also covers legacy pending entries loaded from Redis. They bypass
	// Record during startup flush and may have been created by a version that
	// cached reversible key values.
	for _, log := range logs {
		sanitizeRequestLog(log)
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(logs, len(logs)).Error; err != nil {
			return fmt.Errorf("failed to batch insert request logs: %w", err)
		}

		type keyUsageStat struct {
			Count      int64
			LastUsedAt time.Time
		}

		groupedKeyStats := make(map[uint]map[string]keyUsageStat)
		for _, log := range logs {
			if !log.IsSuccess || log.KeyHash == "" {
				continue
			}

			if _, exists := groupedKeyStats[log.GroupID]; !exists {
				groupedKeyStats[log.GroupID] = make(map[string]keyUsageStat)
			}

			stats := groupedKeyStats[log.GroupID][log.KeyHash]
			stats.Count++
			if stats.LastUsedAt.IsZero() || log.Timestamp.After(stats.LastUsedAt) {
				stats.LastUsedAt = log.Timestamp
			}
			groupedKeyStats[log.GroupID][log.KeyHash] = stats
		}

		if len(groupedKeyStats) > 0 {
			for groupID, keyStats := range groupedKeyStats {
				var requestCountCase strings.Builder
				requestCountCase.WriteString("CASE key_hash ")

				var lastUsedAtCase strings.Builder
				lastUsedAtCase.WriteString("CASE key_hash ")

				requestCountArgs := make([]any, 0, len(keyStats)*2)
				lastUsedAtArgs := make([]any, 0, len(keyStats)*2)
				keyHashes := make([]string, 0, len(keyStats))

				for keyHash, stats := range keyStats {
					requestCountCase.WriteString("WHEN ? THEN request_count + ? ")
					requestCountArgs = append(requestCountArgs, keyHash, stats.Count)

					lastUsedAtCase.WriteString("WHEN ? THEN ? ")
					lastUsedAtArgs = append(lastUsedAtArgs, keyHash, stats.LastUsedAt)

					keyHashes = append(keyHashes, keyHash)
				}

				requestCountCase.WriteString("ELSE request_count END")
				lastUsedAtCase.WriteString("ELSE last_used_at END")

				if err := tx.Model(&models.APIKey{}).
					Where("group_id = ? AND key_hash IN ?", groupID, keyHashes).
					Updates(map[string]any{
						"request_count": gorm.Expr(requestCountCase.String(), requestCountArgs...),
						"last_used_at":  gorm.Expr(lastUsedAtCase.String(), lastUsedAtArgs...),
					}).Error; err != nil {
					return fmt.Errorf("failed to batch update api_key stats: %w", err)
				}
			}
		}

		// 更新统计表
		hourlyStats := make(map[struct {
			Time    time.Time
			GroupID uint
		}]struct{ Success, Failure int64 })
		for _, log := range logs {
			if log.RequestType == models.RequestTypeRetry {
				continue
			}
			hourlyTime := log.Timestamp.Truncate(time.Hour)
			key := struct {
				Time    time.Time
				GroupID uint
			}{Time: hourlyTime, GroupID: log.GroupID}

			counts := hourlyStats[key]
			if log.IsSuccess {
				counts.Success++
			} else {
				counts.Failure++
			}
			hourlyStats[key] = counts

			if log.ParentGroupID > 0 {
				parentKey := struct {
					Time    time.Time
					GroupID uint
				}{Time: hourlyTime, GroupID: log.ParentGroupID}

				parentCounts := hourlyStats[parentKey]
				if log.IsSuccess {
					parentCounts.Success++
				} else {
					parentCounts.Failure++
				}
				hourlyStats[parentKey] = parentCounts
			}
		}

		if len(hourlyStats) > 0 {
			for key, counts := range hourlyStats {
				err := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "time"}, {Name: "group_id"}},
					DoUpdates: clause.Assignments(map[string]any{
						"success_count": gorm.Expr("group_hourly_stats.success_count + ?", counts.Success),
						"failure_count": gorm.Expr("group_hourly_stats.failure_count + ?", counts.Failure),
						"updated_at":    time.Now(),
					}),
				}).Create(&models.GroupHourlyStat{
					Time:         key.Time,
					GroupID:      key.GroupID,
					SuccessCount: counts.Success,
					FailureCount: counts.Failure,
				}).Error

				if err != nil {
					return fmt.Errorf("failed to upsert group hourly stat: %w", err)
				}
			}
		}

		return nil
	})
}

func sanitizeRequestLog(log *models.RequestLog) {
	if log == nil {
		return
	}
	// Defense in depth for every caller: persist only the fingerprint derived
	// from the one-way hash, never a supplied credential.
	log.KeyValue = utils.KeyFingerprint(log.KeyHash)
	log.RequestPath = utils.SanitizeText(log.RequestPath)
	log.UpstreamAddr = utils.SanitizeText(log.UpstreamAddr)
	log.ErrorMessage = utils.SanitizeText(log.ErrorMessage)
	log.RequestBody = utils.SanitizeText(log.RequestBody)
}
