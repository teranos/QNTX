package qntxopenrouter

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/errors"
)

// ModelUsage represents a record of AI model usage
type ModelUsage struct {
	ID                int        `json:"id"`
	OperationType     string     `json:"operation_type"`
	EntityType        string     `json:"entity_type"`
	EntityID          string     `json:"entity_id"`
	ModelName         string     `json:"model_name"`
	ModelProvider     string     `json:"model_provider"`
	ModelConfig       *string    `json:"model_config,omitempty"`
	RequestTimestamp  time.Time  `json:"request_timestamp"`
	ResponseTimestamp *time.Time `json:"response_timestamp,omitempty"`
	TokensUsed        *int       `json:"tokens_used,omitempty"`
	Cost              *float64   `json:"cost,omitempty"`
	Success           bool       `json:"success"`
	ErrorMessage      *string    `json:"error_message,omitempty"`
	Metadata          *string    `json:"metadata,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// ModelConfig represents the configuration used for an AI model request
type ModelConfig struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

// UsageTracker provides functionality to track AI model usage
type UsageTracker struct {
	db        *sql.DB
	verbosity int
}

// NewUsageTracker creates a new AI usage tracker
func NewUsageTracker(db *sql.DB, verbosity int) *UsageTracker {
	return &UsageTracker{db: db, verbosity: verbosity}
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

	if err != nil {
		return errors.Wrap(err, "failed to track usage")
	}
	return nil
}

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
