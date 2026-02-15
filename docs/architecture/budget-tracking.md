# Budget Tracking Architecture

## Overview

QNTX has a two-component budget tracking system where `ai/tracker` records individual API calls and feeds data to `pulse/budget` for centralized budget management and enforcement.

When multiple QNTX instances sync, spend summaries are exchanged so each node can enforce budgets against the aggregate spend across all nodes.

## Component Responsibilities

### ai/tracker
**Purpose**: Records every API call for auditing and cost tracking

- Captures API call metadata (model, tokens, cost)
- Stores detailed usage history
- Provides usage analytics and reporting
- Acts as the data collection layer

### pulse/budget
**Purpose**: Budget management and enforcement (local + distributed)

- Enforces daily/weekly/monthly spending limits
- Makes go/no-go decisions for new operations
- Aggregates local spend with peer spend from sync
- Enforces optional cluster-wide limits averaged across nodes
- Can pause or fail jobs when budget exceeded (configurable)

## Data Flow

```
API Call → ai/tracker (records) → pulse/budget (aggregates) → Decision
                ↓                           ↓                     ↑
          Usage History              Budget Enforcement      Peer Spends
                                                                  ↑
                                                          sync_done message
                                                          (from each peer)
```

1. **API Call Made**: Any component making an API call uses ai/tracker
2. **Usage Recorded**: ai/tracker logs the call details and cost
3. **Budget Queried**: pulse/budget reads spend from `ai_model_usage` table (sliding windows: 24h/7d/30d)
4. **Peers Aggregated**: Non-stale peer spends (received via sync) are added to local spend
5. **Enforcement**: `CheckBudget()` checks aggregate against node limits, then cluster limits
6. **Decision**: Allow/deny based on budget status

## Distributed Enforcement

When [sync](../sync.md) is configured, each reconciliation's `sync_done` message carries the sender's spend summary and cluster limit configuration. No extra round-trips — the data piggybacks on an existing protocol message.

### Two-tier model

**Node budget** (`daily_budget_usd`, etc.) — The node's local limit, checked against aggregate spend (local + all non-stale peer spends). Every node enforces its own limit independently.

**Cluster budget** (`cluster_daily_budget_usd`, etc.) — An org-wide ceiling. The effective cluster limit is the **average** of all participating nodes' configured cluster limits. Checked against the same aggregate spend. Set to 0 (default) to disable.

Example: desktop configures `cluster_daily_budget_usd = 6.00`, phone configures `8.00`. Effective cluster daily limit = `(6 + 8) / 2 = $7.00`. If desktop spent $3 and phone spent $5, aggregate = $8 > $7 — both nodes will block new operations.

### Staleness

Peer spends older than 10 minutes are excluded from aggregation. If a peer goes offline, its stale spend data stops blocking local operations.

### Backward compatibility

Budget fields use `*float64` pointer types with `omitempty`. Old peers that don't send budget data simply produce `nil` — no budget aggregation occurs. New peers syncing with old peers still exchange attestations normally.

## Database Schema

### ai_model_usage table
Per-call usage records. Budget enforcement queries this with sliding windows (24h, 7d, 30d).

## Configuration

Budget settings in `am.toml`:

```toml
[pulse]
# Node-level budget (enforced per-node against aggregate spend)
daily_budget_usd = 3.0
weekly_budget_usd = 7.0
monthly_budget_usd = 15.0
cost_per_score_usd = 0.002

# Cluster-level budget (enforced against aggregate spend across all synced nodes)
# Effective limit = average of all nodes' configured cluster limits
# 0 = no cluster enforcement (default)
cluster_daily_budget_usd = 10.0
cluster_weekly_budget_usd = 50.0
cluster_monthly_budget_usd = 150.0
```

## Integration Points

- **Async Jobs**: Check budget before expensive operations
- **API Clients**: Use ai/tracker for all external API calls
- **Sync Protocol**: Exchanges spend summaries and cluster limits on `sync_done`
- **Web UI**: Displays budget status and usage history

## Common Scenarios

**Budget Exceeded**: Jobs will fail with a message indicating which limit was hit (node or cluster) and the aggregate spend breakdown (local + peers).

**Peer Offline**: After 10 minutes of no sync, the offline peer's spend is excluded. Local node operates against its own spend only until the next successful sync.