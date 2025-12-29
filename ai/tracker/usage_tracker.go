package tracker

import (
	"database/sql"
	"encoding/json"
	"time"
)

// ModelUsage represents a record of AI model usage
//
// Testing: Basic sqlmock tests implemented for core operations (TrackUsage, GetUsageStats).
// See usage_tracker_test.go for comprehensive TODOs on additional test coverage needed.
type ModelUsage struct {
	ID                int        `json:"id" db:"id"`
	OperationType     string     `json:"operation_type" db:"operation_type"`
	EntityType        string     `json:"entity_type" db:"entity_type"`
	EntityID          string     `json:"entity_id" db:"entity_id"`
	ModelName         string     `json:"model_name" db:"model_name"`
	ModelProvider     string     `json:"model_provider" db:"model_provider"`
	ModelConfig       *string    `json:"model_config,omitempty" db:"model_config"`
	RequestTimestamp  time.Time  `json:"request_timestamp" db:"request_timestamp"`
	ResponseTimestamp *time.Time `json:"response_timestamp,omitempty" db:"response_timestamp"`
	TokensUsed        *int       `json:"tokens_used,omitempty" db:"tokens_used"`
	Cost              *float64   `json:"cost,omitempty" db:"cost"`
	Success           bool       `json:"success" db:"success"`
	ErrorMessage      *string    `json:"error_message,omitempty" db:"error_message"`
	Metadata          *string    `json:"metadata,omitempty" db:"metadata"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}

// ModelConfig represents the configuration used for an AI model request
type ModelConfig struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
}

// UsageMetadata represents additional context for AI model usage
type UsageMetadata struct {
	UserAgent       string `json:"user_agent,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	OperationDetail string `json:"operation_detail,omitempty"`
	InputLength     *int   `json:"input_length,omitempty"`
	OutputLength    *int   `json:"output_length,omitempty"`
}

// UsageTracker provides functionality to track AI model usage
type UsageTracker struct {
	db        *sql.DB
	verbosity int
}

// NewUsageTracker creates a new AI usage tracker
func NewUsageTracker(db *sql.DB, verbosity int) *UsageTracker {
	return &UsageTracker{
		db:        db,
		verbosity: verbosity,
	}
}

// TrackUsage records AI model usage in the database
func (t *UsageTracker) TrackUsage(usage *ModelUsage) error {
	query := `
		INSERT INTO ai_model_usage (
			operation_type, entity_type, entity_id, model_name, model_provider,
			model_config, request_timestamp, response_timestamp, tokens_used,
			cost, success, error_message, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := t.db.Exec(query,
		usage.OperationType, usage.EntityType, usage.EntityID,
		usage.ModelName, usage.ModelProvider, usage.ModelConfig,
		usage.RequestTimestamp, usage.ResponseTimestamp, usage.TokensUsed,
		usage.Cost, usage.Success, usage.ErrorMessage, usage.Metadata,
	)

	return err
}

// GetUsageStats returns usage statistics for a given time period
func (t *UsageTracker) GetUsageStats(since time.Time) (*UsageStats, error) {
	query := `
		SELECT
			COUNT(*) as total_requests,
			COUNT(CASE WHEN success = 1 THEN 1 END) as successful_requests,
			COALESCE(SUM(COALESCE(tokens_used, 0)), 0) as total_tokens,
			COALESCE(SUM(COALESCE(cost, 0)), 0) as total_cost,
			COUNT(DISTINCT CASE WHEN model_name IS NOT NULL THEN model_name END) as unique_models
		FROM ai_model_usage
		WHERE request_timestamp >= ?`

	var stats UsageStats
	err := t.db.QueryRow(query, since).Scan(
		&stats.TotalRequests, &stats.SuccessfulRequests,
		&stats.TotalTokens, &stats.TotalCost, &stats.UniqueModels,
	)

	if err != nil {
		return nil, err
	}

	if stats.TotalRequests > 0 {
		stats.SuccessRate = float64(stats.SuccessfulRequests) / float64(stats.TotalRequests)
	}

	return &stats, nil
}

// GetModelBreakdown returns usage breakdown by model
func (t *UsageTracker) GetModelBreakdown(since time.Time) ([]ModelBreakdown, error) {
	query := `
		SELECT
			model_name,
			model_provider,
			COUNT(*) as request_count,
			SUM(COALESCE(tokens_used, 0)) as total_tokens,
			SUM(COALESCE(cost, 0)) as total_cost,
			AVG(CASE WHEN response_timestamp IS NOT NULL THEN
				(julianday(response_timestamp) - julianday(request_timestamp)) * 86400000
				ELSE NULL END) as avg_response_time_ms
		FROM ai_model_usage
		WHERE request_timestamp >= ? AND success = 1
		GROUP BY model_name, model_provider
		ORDER BY total_cost DESC`

	rows, err := t.db.Query(query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var breakdown []ModelBreakdown
	for rows.Next() {
		var mb ModelBreakdown
		err := rows.Scan(&mb.ModelName, &mb.ModelProvider, &mb.RequestCount,
			&mb.TotalTokens, &mb.TotalCost, &mb.AvgResponseTimeMs)
		if err != nil {
			continue
		}
		breakdown = append(breakdown, mb)
	}

	return breakdown, nil
}

// UsageStats represents aggregated usage statistics
type UsageStats struct {
	TotalRequests      int     `json:"total_requests"`
	SuccessfulRequests int     `json:"successful_requests"`
	SuccessRate        float64 `json:"success_rate"`
	TotalTokens        int     `json:"total_tokens"`
	TotalCost          float64 `json:"total_cost"`
	UniqueModels       int     `json:"unique_models"`
}

// GetTimeSeriesData returns daily aggregated cost and request counts
// TODO: Research better time-series architecture - see GitHub issue for WebSocket streaming investigation
//
// TODO(future): Per-model breakdown support
// Add optional model filtering to time-series queries:
// - Parameter: modelName string (empty = all models)
// - Query: Add WHERE model_name = ? when filter specified
// - Response: Include model metadata in TimeSeriesPoint
// - Consider: Multi-model response format (map[string][]TimeSeriesPoint)
//
// TODO(future): Period comparison queries
// Support fetching multiple time ranges for comparison:
// - Method: GetTimeSeriesComparison(currentDays, previousDays int)
// - Returns: Current period + previous period data
// - Enables: Week-over-week, month-over-month calculations
// - Optimization: Single query with UNION or window functions
func (t *UsageTracker) GetTimeSeriesData(days int) ([]TimeSeriesPoint, error) {
	query := `
		SELECT
			DATE(request_timestamp) as date,
			COUNT(*) as requests,
			COALESCE(SUM(COALESCE(cost, 0)), 0) as cost
		FROM ai_model_usage
		WHERE request_timestamp >= datetime('now', '-' || ? || ' days')
		GROUP BY DATE(request_timestamp)
		ORDER BY date ASC`

	rows, err := t.db.Query(query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var point TimeSeriesPoint
		err := rows.Scan(&point.Date, &point.Requests, &point.Cost)
		if err != nil {
			continue
		}
		points = append(points, point)
	}

	return points, nil
}

// TimeSeriesPoint represents a single data point in time-series
type TimeSeriesPoint struct {
	Date     string  `json:"date"`
	Requests int     `json:"requests"`
	Cost     float64 `json:"cost"`
}

// ModelBreakdown represents usage statistics for a specific model
type ModelBreakdown struct {
	ModelName         string   `json:"model_name"`
	ModelProvider     string   `json:"model_provider"`
	RequestCount      int      `json:"request_count"`
	TotalTokens       int      `json:"total_tokens"`
	TotalCost         float64  `json:"total_cost"`
	AvgResponseTimeMs *float64 `json:"avg_response_time_ms,omitempty"`
}

// Helper functions for creating model configs and metadata

// NewModelConfig creates a ModelConfig and serializes it to JSON
func NewModelConfig(temperature *float64, maxTokens *int) *string {
	if temperature == nil && maxTokens == nil {
		return nil
	}

	config := ModelConfig{
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil
	}

	jsonStr := string(data)
	return &jsonStr
}

// NewUsageMetadata creates UsageMetadata and serializes it to JSON
func NewUsageMetadata(metadata UsageMetadata) *string {
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}

	jsonStr := string(data)
	return &jsonStr
}
