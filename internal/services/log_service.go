package services

import (
	"encoding/csv"
	"fmt"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"strconv"
	"time"

	"gorm.io/gorm"
)

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
			if hashPrefix, ok := utils.ParseKeyFingerprint(filter.KeyValue); ok {
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
	header := []string{"key_identifier", "group_name", "status_code"}
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

	// Export only the non-reversible key identifier. This also makes exports
	// safe for historical rows whose key_value may contain legacy ciphertext or
	// plaintext.
	for _, record := range results {
		csvRecord := []string{
			utils.KeyFingerprint(record.KeyHash),
			record.GroupName,
			strconv.Itoa(record.StatusCode),
		}
		if err := csvWriter.Write(csvRecord); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	return nil
}
