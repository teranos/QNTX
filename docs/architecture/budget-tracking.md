# Budget Tracking Architecture

## Overview

QNTX has a two-component budget tracking system where `ai/tracker` records individual API calls and feeds data to `pulse/budget` for centralized budget management and enforcement.

## Component Responsibilities

### ai/tracker
**Purpose**: Records every API call for auditing and cost tracking

- Captures API call metadata (model, tokens, cost)
- Stores detailed usage history
- Provides usage analytics and reporting
- Acts as the data collection layer

### pulse/budget
**Purpose**: Centralized budget management and enforcement

- Enforces daily/weekly/monthly spending limits
- Makes go/no-go decisions for new operations
- Tracks aggregate spending across all sources
- Implements rate limiting based on budget constraints
- Can pause or fail jobs when budget exceeded (configurable)

## Data Flow

```
API Call → ai/tracker (records) → pulse/budget (aggregates) → Decision
                ↓                           ↓
          Usage History              Budget Enforcement
```

1. **API Call Made**: Any component making an API call uses ai/tracker
2. **Usage Recorded**: ai/tracker logs the call details and cost
3. **Budget Updated**: ai/tracker feeds cost data to pulse/budget
4. **Enforcement**: pulse/budget checks if future calls are within limits
5. **Decision**: Allow/deny/pause based on budget status

## Database Schema

### pulse_budget table
Tracks aggregate spending and limits:
- `spend_usd` - Current spend amount
- `daily_budget` - Daily limit
- `weekly_budget` - Weekly limit
- `monthly_budget` - Monthly limit
- `last_reset` - When counters were last reset

### AI usage tracking
Detailed per-call tracking (exact schema varies by implementation)

## Configuration

Budget settings in `am.toml`:

```toml
[pulse]
daily_budget_usd = 10.0
weekly_budget_usd = 50.0
monthly_budget_usd = 150.0
pause_on_budget_exceeded = true  # or false to fail jobs
```

## Integration Points

- **Async Jobs**: Check budget before expensive operations
- **API Clients**: Use ai/tracker for all external API calls
- **Pulse Ticker**: Resets daily/weekly/monthly counters
- **Web UI**: Displays budget status and usage history

## Monitoring Budget Status

```bash
# Check current budget status
qntx am get pulse.daily_budget_usd    # See limit
qntx pulse status                      # See current usage

# View in web UI
# Navigate to Pulse panel → Budget tab
```

## Common Scenarios

**Budget Exceeded**: Jobs will pause (if `pause_on_budget_exceeded = true`) or fail. Check logs for `꩜ Budget exceeded` messages.

**Reset Timing**: Counters reset at midnight UTC for daily, Sunday midnight for weekly, first of month for monthly.