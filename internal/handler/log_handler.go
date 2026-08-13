package handler

import (
	"fmt"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/i18n"
	"gpt-load/internal/models"
	"gpt-load/internal/response"
	"gpt-load/internal/services"
	"gpt-load/internal/utils"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// LogResponse is the public request-log representation. It intentionally does
// not contain RequestLog.KeyHash: even a one-way hash is internal correlation
// material and must never be serialized by the API.
//
// KeyValue carries the identifier shown in the list. It is the masked key plus a
// short discriminator when the row can still be resolved to a key in key
// management, and the non-reversible fingerprint otherwise. KeyFingerprint always
// carries the fingerprint.
type LogResponse struct {
	ID              string    `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	GroupID         uint      `json:"group_id"`
	GroupName       string    `json:"group_name"`
	ParentGroupID   uint      `json:"parent_group_id"`
	ParentGroupName string    `json:"parent_group_name"`
	KeyValue        string    `json:"key_value"`
	KeyFingerprint  string    `json:"key_fingerprint"`
	Model           string    `json:"model"`
	IsSuccess       bool      `json:"is_success"`
	SourceIP        string    `json:"source_ip"`
	StatusCode      int       `json:"status_code"`
	RequestPath     string    `json:"request_path"`
	Duration        int64     `json:"duration_ms"`
	ErrorMessage    string    `json:"error_message"`
	UserAgent       string    `json:"user_agent"`
	RequestType     string    `json:"request_type"`
	UpstreamAddr    string    `json:"upstream_addr"`
	IsStream        bool      `json:"is_stream"`
	RequestBody     string    `json:"request_body"`
}

// newLogResponse builds the public representation. keyIdentifiers maps key
// hashes to display identifiers for rows whose key still exists; a row absent
// from the map falls back to its fingerprint.
func newLogResponse(logEntry models.RequestLog, keyIdentifiers map[string]string) LogResponse {
	fingerprint := utils.KeyFingerprint(logEntry.KeyHash)
	identifier := fingerprint
	if resolved, ok := keyIdentifiers[logEntry.KeyHash]; ok {
		identifier = resolved
	}

	return LogResponse{
		ID:              logEntry.ID,
		Timestamp:       logEntry.Timestamp,
		GroupID:         logEntry.GroupID,
		GroupName:       logEntry.GroupName,
		ParentGroupID:   logEntry.ParentGroupID,
		ParentGroupName: logEntry.ParentGroupName,
		KeyValue:        identifier,
		KeyFingerprint:  fingerprint,
		Model:           logEntry.Model,
		IsSuccess:       logEntry.IsSuccess,
		SourceIP:        logEntry.SourceIP,
		StatusCode:      logEntry.StatusCode,
		RequestPath:     utils.SanitizeText(logEntry.RequestPath),
		Duration:        logEntry.Duration,
		ErrorMessage:    utils.SanitizeText(logEntry.ErrorMessage),
		UserAgent:       logEntry.UserAgent,
		RequestType:     logEntry.RequestType,
		UpstreamAddr:    utils.SanitizeURLStringForLogging(logEntry.UpstreamAddr),
		IsStream:        logEntry.IsStream,
		RequestBody:     utils.SanitizeText(logEntry.RequestBody),
	}
}

// GetLogs is the backwards-compatible GET endpoint. A key filter in a URL may
// only be a non-reversible fp: identifier; complete upstream keys must use the
// POST JSON search endpoint.
func (s *Server) GetLogs(c *gin.Context) {
	filter, ok := logFilterFromQuery(c)
	if !ok {
		response.Error(c, app_errors.ErrBadRequest)
		return
	}
	s.respondWithLogs(c, filter)
}

// SearchLogs accepts filters in a JSON body so a complete upstream key can be
// hashed for an exact lookup without entering URLs, access logs, or history.
func (s *Server) SearchLogs(c *gin.Context) {
	var filter services.LogFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	s.respondWithLogs(c, filter)
}

func (s *Server) respondWithLogs(c *gin.Context, filter services.LogFilter) {
	query := s.LogService.GetLogsQuery(filter)
	page, pageSize := normalizedLogPagination(filter.Page, filter.PageSize)

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	var logs []models.RequestLog
	offset := (page - 1) * pageSize
	if err := query.Order("timestamp desc").Limit(pageSize).Offset(offset).Find(&logs).Error; err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	items := make([]LogResponse, len(logs))
	keyHashes := make([]string, 0, len(logs))
	for i := range logs {
		keyHashes = append(keyHashes, logs[i].KeyHash)
	}
	// One batched, indexed lookup for the whole page rather than one per row.
	keyIdentifiers := s.LogService.ResolveKeyIdentifiers(keyHashes)
	for i := range logs {
		items[i] = newLogResponse(logs[i], keyIdentifiers)
	}

	response.Success(c, &response.PaginatedResponse{
		Items: items,
		Pagination: response.Pagination{
			Page:       page,
			PageSize:   pageSize,
			TotalItems: totalItems,
			TotalPages: int(math.Ceil(float64(totalItems) / float64(pageSize))),
		},
	})
}

func normalizedLogPagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = response.DefaultPageSize
	}
	if pageSize > response.MaxPageSize {
		pageSize = response.MaxPageSize
	}
	return page, pageSize
}

func logFilterFromQuery(c *gin.Context) (services.LogFilter, bool) {
	filter := services.LogFilter{
		ParentGroupName: c.Query("parent_group_name"),
		GroupName:       c.Query("group_name"),
		KeyValue:        c.Query("key_value"),
		Model:           c.Query("model"),
		RequestType:     c.Query("request_type"),
		SourceIP:        c.Query("source_ip"),
		ErrorContains:   c.Query("error_contains"),
	}

	// Reject, rather than hash, a complete key supplied through a GET URL. The
	// response is deliberately generic and never repeats the supplied value.
	if filter.KeyValue != "" {
		if _, valid := utils.ParseKeyFingerprint(filter.KeyValue); !valid {
			return services.LogFilter{}, false
		}
	}
	if value, err := strconv.Atoi(c.Query("page")); err == nil {
		filter.Page = value
	}
	if value, err := strconv.Atoi(c.Query("page_size")); err == nil {
		filter.PageSize = value
	}
	if value, err := strconv.ParseBool(c.Query("is_success")); err == nil {
		filter.IsSuccess = &value
	}
	if value, err := strconv.Atoi(c.Query("status_code")); err == nil {
		filter.StatusCode = &value
	}
	if value, err := time.Parse(time.RFC3339, c.Query("start_time")); err == nil {
		filter.StartTime = &value
	}
	if value, err := time.Parse(time.RFC3339, c.Query("end_time")); err == nil {
		filter.EndTime = &value
	}

	return filter, true
}

// ExportLogs is the backwards-compatible GET export. Like GetLogs, it accepts
// only a fingerprint in key_value.
func (s *Server) ExportLogs(c *gin.Context) {
	filter, ok := logFilterFromQuery(c)
	if !ok {
		response.Error(c, app_errors.ErrBadRequest)
		return
	}
	s.exportLogs(c, filter)
}

// ExportLogsSearch accepts export filters in a JSON body, including a complete
// key that is hashed in memory for an exact lookup.
func (s *Server) ExportLogsSearch(c *gin.Context) {
	var filter services.LogFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	s.exportLogs(c, filter)
}

func (s *Server) exportLogs(c *gin.Context, filter services.LogFilter) {
	filename := fmt.Sprintf("log_key_identifiers_export_%s.csv", time.Now().Format("20060102150405"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "text/csv; charset=utf-8")

	if err := s.LogService.StreamLogKeysToCSV(filter, c.Writer); err != nil {
		log.Printf("Failed to stream log keys to CSV: %v", err)
		c.JSON(500, gin.H{"error": i18n.Message(c, "error.export_logs")})
	}
}
