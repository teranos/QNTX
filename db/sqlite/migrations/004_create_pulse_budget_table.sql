-- Pulse budget tracking for rate limiting and budget control
-- Supports both daily and monthly budget tracking
CREATE TABLE pulse_budget (
    date TEXT PRIMARY KEY,           -- "2025-11-23" for daily, "2025-11" for monthly
    type TEXT NOT NULL,              -- "daily" or "monthly"
    spend_usd REAL NOT NULL,         -- Current spend in USD
    operations_count INTEGER NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE INDEX idx_pulse_budget_type ON pulse_budget(type);
