package services

import (
	"encoding/csv"
	"errors"
	"fmt"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// maskSearchKeyLimit bounds how many api_keys a single masked-key search may
// match. A mask retains only eight characters, so a deliberately broad one could
// otherwise build an unbounded IN clause.
const maskSearchKeyLimit = 1000

// errMaskSearchLimit stops batch iteration once maskSearchKeyLimit is reached.
// It never escapes resolveMaskedKeyHashes.
var errMaskSearchLimit = errors.New("masked key search limit reached")

// ExportableLogKey defines the structure for the data to be exported to CSV.
type ExportableLogKey struct {
	KeyHash    string `gorm:"column:key_hash"`
	GroupName  string `gorm:"column:group_name"`
	StatusCode int    `gorm:"column:status_code"`
}

// LogService provides services related to request logs.
type LogService struct {
	DB            *gorm.DB
	EncryptionSvc encryption.Service
}

// LogFilter contains request-log search criteria independently of the HTTP
// transport. Handlers decide whether a credential-shaped key value is allowed:
// GET compatibility endpoints accept fingerprints only, while POST JSON search
// endpoints may hash a complete upstream key without placing it in a URL.
type LogFilter struct {
	Page            int        `json:"page"`
	PageSize        int        `json:"page_size"`
	ParentGroupName string     `json:"parent_group_name"`
	GroupName       string     `json:"group_name"`
	KeyValue        string     `json:"key_value"`
	Model           string     `json:"model"`
	IsSuccess       *bool      `json:"is_success"`
	RequestType     string     `json:"request_type"`
	StatusCode      *int       `json:"status_code"`
	SourceIP        string     `json:"source_ip"`
	ErrorContains   string     `json:"error_contains"`
	StartTime       *time.Time `json:"start_time"`
	EndTime         *time.Time `json:"end_time"`
}

// NewLogService creates a new LogService.
func NewLogService(db *gorm.DB, encryptionSvc encryption.Service) *LogService {
	return &LogService{
		DB:            db,
		EncryptionSvc: encryptionSvc,
	}
}

// ResolveKeyMasks maps request-log key hashes to the masked form of the live
// api_keys row each one belongs to.
//
// The mask is derived by decrypting api_keys.key_value, which is the same source
// the key management screen masks. Consistency between the two screens is
// therefore structural rather than a convention two code paths must both
// remember to follow, and nothing has to be persisted alongside the log row.
//
// Hashes with no live key — a deleted key, or a hash written before an
// ENCRYPTION_KEY rotation — are simply absent from the result. Callers fall back
// to the fingerprint for those. A database error is treated the same way: log
// listing must keep working on a database where api_keys is unavailable.
func (s *LogService) ResolveKeyMasks(keyHashes []string) map[string]string {
	masks := make(map[string]string)

	unique := make([]string, 0, len(keyHashes))
	seen := make(map[string]struct{}, len(keyHashes))
	for _, keyHash := range keyHashes {
		if keyHash == "" {
			continue
		}
		if _, exists := seen[keyHash]; exists {
			continue
		}
		seen[keyHash] = struct{}{}
		unique = append(unique, keyHash)
	}
	if len(unique) == 0 {
		return masks
	}

	for start := 0; start < len(unique); start += chunkSize {
		end := min(start+chunkSize, len(unique))

		var keys []models.APIKey
		if err := s.DB.Model(&models.APIKey{}).
			Select("key_hash", "key_value").
			Where("key_hash IN ?", unique[start:end]).
			Find(&keys).Error; err != nil {
			// Never attach the query parameters: they are key hashes.
			logrus.WithError(err).Debug("Failed to resolve request-log key masks; falling back to fingerprints")
			return masks
		}

		for _, key := range keys {
			plaintext, err := s.EncryptionSvc.Decrypt(key.KeyValue)
			if err != nil {
				// A key that cannot be decrypted cannot be identified; the caller
				// falls back to the fingerprint for this row.
				logrus.WithError(err).Debug("Failed to decrypt key while resolving request-log key mask")
				continue
			}
			if mask := utils.MaskKeyIdentifier(plaintext); mask != "" {
				masks[key.KeyHash] = mask
			}
		}
	}

	return masks
}

// resolveMaskedKeyHashes finds the key hashes of every api_keys row that would
// render as the given mask. Masks retain only eight characters and provider key
// prefixes are shared, so more than one key can match; every match is returned
// so the caller shows all candidate rows rather than silently picking one.
func (s *LogService) resolveMaskedKeyHashes(head, tail string) []string {
	hashes := make([]string, 0, 1)
	seen := make(map[string]struct{})

	var batch []models.APIKey
	err := s.DB.Model(&models.APIKey{}).
		Select("key_hash", "key_value").
		FindInBatches(&batch, chunkSize, func(_ *gorm.DB, _ int) error {
			for _, key := range batch {
				if key.KeyHash == "" {
					continue
				}
				plaintext, err := s.EncryptionSvc.Decrypt(key.KeyValue)
				if err != nil {
					continue
				}
				if !utils.KeyMatchesMask(plaintext, head, tail) {
					continue
				}
				if _, exists := seen[key.KeyHash]; exists {
					continue
				}
				seen[key.KeyHash] = struct{}{}
				hashes = append(hashes, key.KeyHash)
				if len(hashes) >= maskSearchKeyLimit {
					return errMaskSearchLimit
				}
			}
			return nil
		}).Error

	switch {
	case errors.Is(err, errMaskSearchLimit):
		// Report the truncation rather than letting a capped result read as a
		// complete one.
		logrus.WithField("limit", maskSearchKeyLimit).
			Warn("Masked key search matched too many keys; results are truncated")
	case err != nil:
		logrus.WithError(err).Debug("Failed to resolve masked key search")
	}

	return hashes
}

// logFiltersScope returns a GORM scope function that applies a validated filter.
func (s *LogService) logFiltersScope(filter LogFilter) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filter.ParentGroupName != "" {
			db = db.Where("parent_group_name LIKE ?", "%"+filter.ParentGroupName+"%")
		}
		if filter.GroupName != "" {
			db = db.Where("group_name LIKE ?", "%"+filter.GroupName+"%")
		}
		if filter.KeyValue != "" {
			// Ordering matters. A mask is checked first because it is the value now
			// shown in the list, and operators search by copying that column. Its
			// shape check is exact, so a complete key cannot be diverted here.
			if head, tail, ok := utils.ParseKeyMask(filter.KeyValue); ok {
				keyHashes := s.resolveMaskedKeyHashes(head, tail)
				if len(keyHashes) == 0 {
					// No key renders as this mask. Match nothing explicitly rather
					// than falling through to a hash comparison that would return
					// nothing for an unrelated reason.
					db = db.Where("1 = 0")
				} else {
					db = db.Where("key_hash IN ?", keyHashes)
				}
			} else if hashPrefix, ok := utils.ParseKeyFingerprint(filter.KeyValue); ok {
				db = db.Where("key_hash LIKE ?", hashPrefix+"%")
			} else {
				keyHash := s.EncryptionSvc.Hash(filter.KeyValue)
				db = db.Where("key_hash = ?", keyHash)
			}
		}
		if filter.Model != "" {
			db = db.Where("model LIKE ?", "%"+filter.Model+"%")
		}
		if filter.IsSuccess != nil {
			db = db.Where("is_success = ?", *filter.IsSuccess)
		}
		if filter.RequestType != "" {
			db = db.Where("request_type = ?", filter.RequestType)
		}
		if filter.StatusCode != nil {
			db = db.Where("status_code = ?", *filter.StatusCode)
		}
		if filter.SourceIP != "" {
			db = db.Where("source_ip = ?", filter.SourceIP)
		}
		if filter.ErrorContains != "" {
			db = db.Where("error_message LIKE ?", "%"+filter.ErrorContains+"%")
		}
		if filter.StartTime != nil {
			db = db.Where("timestamp >= ?", *filter.StartTime)
		}
		if filter.EndTime != nil {
			db = db.Where("timestamp <= ?", *filter.EndTime)
		}
		return db
	}
}

// GetLogsQuery returns a GORM query for fetching logs with filters.
func (s *LogService) GetLogsQuery(filter LogFilter) *gorm.DB {
	return s.DB.Model(&models.RequestLog{}).Scopes(s.logFiltersScope(filter))
}

// StreamLogKeysToCSV fetches unique keys from logs based on filters and streams them as a CSV.
func (s *LogService) StreamLogKeysToCSV(filter LogFilter, writer io.Writer) error {
	// Create a CSV writer
	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	// Write CSV header
	header := []string{"key_identifier", "key_fingerprint", "group_name", "status_code"}
	if err := csvWriter.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	var results []ExportableLogKey

	baseQuery := s.DB.Model(&models.RequestLog{}).Scopes(s.logFiltersScope(filter)).Where("key_hash IS NOT NULL AND key_hash != ''")

	// 使用窗口函数获取每个key_hash的最新记录（避免同一密钥因多次加密产生重复）
	err := s.DB.Raw(`
		SELECT
			key_hash,
			group_name,
			status_code
		FROM (
			SELECT
				key_hash,
				group_name,
				status_code,
				ROW_NUMBER() OVER (PARTITION BY key_hash ORDER BY timestamp DESC) as rn
			FROM (?) as filtered_logs
		) ranked
		WHERE rn = 1
		ORDER BY key_hash
	`, baseQuery).Scan(&results).Error

	if err != nil {
		return fmt.Errorf("failed to fetch log keys: %w", err)
	}

	// Export the masked identifier so a row can be matched against key
	// management, alongside the non-reversible fingerprint so the export always
	// carries a unique, shareable identifier even when a mask is ambiguous or
	// the key can no longer be resolved.
	keyHashes := make([]string, 0, len(results))
	for _, record := range results {
		keyHashes = append(keyHashes, record.KeyHash)
	}
	masks := s.ResolveKeyMasks(keyHashes)

	for _, record := range results {
		fingerprint := utils.KeyFingerprint(record.KeyHash)
		identifier := fingerprint
		if mask, ok := masks[record.KeyHash]; ok {
			identifier = mask
		}
		csvRecord := []string{
			identifier,
			fingerprint,
			record.GroupName,
			strconv.Itoa(record.StatusCode),
		}
		if err := csvWriter.Write(csvRecord); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	return nil
}
