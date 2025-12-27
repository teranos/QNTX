-- Create central AI model usage tracking table for budget enforcement
-- Tracks all AI API calls with costs for daily/weekly/monthly budget limits
CREATE TABLE IF NOT EXISTS ai_model_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_type TEXT NOT NULL,  -- 'contact_scoring', 'persona_extraction', 'taxonomy_classification', etc.
    entity_type TEXT NOT NULL,     -- 'contact', 'event', 'twitter_user', etc.
    entity_id TEXT NOT NULL,       -- ID of the entity being processed
    model_name TEXT NOT NULL,      -- Full model identifier (e.g., 'meta-llama/llama-3.2-3b-instruct:free')
    model_provider TEXT NOT NULL, -- 'openrouter', 'anthropic', 'openai', etc.
    model_config TEXT,            -- JSON blob of model configuration
    request_timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    response_timestamp TIMESTAMP,
    tokens_used INTEGER,
    cost REAL,
    success BOOLEAN DEFAULT TRUE,
    error_message TEXT,
    metadata TEXT,                -- JSON blob for additional context
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_ai_model_usage_operation ON ai_model_usage(operation_type);
CREATE INDEX IF NOT EXISTS idx_ai_model_usage_model ON ai_model_usage(model_name);
CREATE INDEX IF NOT EXISTS idx_ai_model_usage_entity ON ai_model_usage(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_ai_model_usage_timestamp ON ai_model_usage(request_timestamp);
CREATE INDEX IF NOT EXISTS idx_ai_model_usage_cost ON ai_model_usage(cost);
