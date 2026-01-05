# QNTX Abstraction Opportunities Analysis

**Date:** January 2026
**Branch:** `claude/analyze-abstraction-opportunities-hYzVw`

This document identifies areas where better abstractions would save the most development time in the future, based on a deep analysis of the QNTX codebase.

---

## Executive Summary

The QNTX codebase is well-architected with clear separation of concerns. However, several patterns emerge repeatedly across packages that would benefit from consolidation. The highest-impact opportunities are:

1. **Error Handling Unification** - Multiple independent error systems that don't interoperate
2. **HTTP Handler Utilities** - Repeated validation/response patterns across 50+ handlers
3. **Storage Query Abstraction** - Duplicated JSON query building and scan patterns
4. **Ingestion Framework Generalization** - 8+ similar file processing functions
5. **Job Handler Base Class** - Common patterns in async job execution

---

## Priority 1: High Impact, High Frequency

### 1.1 Unified Error System

**Problem:** Three independent custom error systems exist without interoperability:
- `ParseError` (parser-specific) - `/home/user/QNTX/ats/parser/error.go`
- `GraphError` (graph-specific) - `/home/user/QNTX/graph/error/types.go`
- `ErrorContext` (job-specific) - `/home/user/QNTX/pulse/async/error.go`

**Issues:**
- No `errors.As()` usage anywhere in codebase (0 instances found)
- `ClassifyError()` uses brittle string pattern matching instead of type hierarchy
- Sentinel error checkers (`IsDatabaseClosed`, `IsNotFoundError`) mix `errors.Is()` with string fallbacks
- Error message duplication in validation (alias_store.go lines 28-34 repeated at 133-136)

**Recommendation:** Create a unified error package:

```go
// errors/qntx_error.go
package errors

type QNTXError struct {
    Err         error
    Code        ErrorCode      // Unified codes across system
    Category    Category       // parse, query, storage, job, etc.
    Severity    Severity       // error, warning, info
    UserMessage string         // For UI display
    Context     map[string]any // Structured context
    Retryable   bool
    Recoverable bool
}

func (e *QNTXError) Error() string
func (e *QNTXError) Unwrap() error
func (e *QNTXError) Is(target error) bool

// Builder pattern for construction
func New(code ErrorCode) *QNTXError
func Wrap(err error, code ErrorCode) *QNTXError
```

**Time Saved:** ~2-4 hours per new feature that handles errors, plus reduced debugging time from consistent error chains.

---

### 1.2 HTTP Handler Utilities

**Problem:** The same patterns repeat across 50+ HTTP handlers in server/, qntx-code/, and plugin/:

```go
// Pattern 1: Method validation (repeated ~30 times)
if r.Method != http.MethodGet {
    http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    return
}

// Pattern 2: JSON response (repeated ~40 times)
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(data)

// Pattern 3: Error response with logging (repeated ~25 times)
logger.Errorw("Failed to X", "error", err)
http.Error(w, "Failed message", http.StatusInternalServerError)
```

**Current State:** Server has `response.go` with utilities, but they're not used consistently across plugins.

**Recommendation:** Extend and consistently use handler utilities:

```go
// server/handler_utils.go (or plugin/http_utils.go)
package server

// Already exists but underutilized:
func WriteJSON(w http.ResponseWriter, status int, data any) error
func WriteError(w http.ResponseWriter, status int, message string)
func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool
func RequireMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool

// Add new utilities:
func ParseQueryInt(r *http.Request, name string, defaultVal, min, max int) int
func ParsePathID(r *http.Request, prefix string) (string, error)
func LogAndError(w http.ResponseWriter, logger *zap.SugaredLogger, status int, msg string, err error)
```

**Time Saved:** ~15-30 minutes per new HTTP handler, plus consistency benefits.

---

### 1.3 Storage Query Builder Abstraction

**Problem:** SQL query building for JSON fields is duplicated across storage files:

```go
// Repeated in query_builder.go for subjects, predicates, contexts, actors
func (b *QueryBuilder) buildSubjectFilter(subjects []string) string {
    for _, s := range subjects {
        escaped := escapeLikePattern(s)
        conditions = append(conditions, fmt.Sprintf(`subjects LIKE '%%"%s"%%' ESCAPE '\'`, escaped))
    }
}
```

**Files with similar patterns:**
- `ats/storage/query_builder.go` - 4 similar filter methods
- `ats/storage/sql_store.go` - JSON existence checks
- `ats/storage/bounded_store_enforcement.go` - Similar queries for limits

**Recommendation:** Create a generic JSON field query builder:

```go
// ats/storage/json_query.go
type JSONFieldQuery struct {
    Field     string   // "subjects", "predicates", etc.
    Values    []string
    MatchMode MatchMode // Exact, Contains, Prefix
    Collation string   // "", "NOCASE"
}

func (q *JSONFieldQuery) ToSQL() (clause string, args []any)
func (q *JSONFieldQuery) ToExistsSubquery() string

// Usage:
query := JSONFieldQuery{Field: "subjects", Values: subjects, MatchMode: Contains}
clause, args := query.ToSQL()
```

**Time Saved:** ~1 hour per new query type, reduced bugs from consistent escaping.

---

### 1.4 Ingestion Processor Abstraction

**Problem:** The dependency ingestion has 8+ nearly identical file processing functions:

| Function | Lines | File Types |
|----------|-------|------------|
| `processGoMod` | 191-286 | go.mod |
| `processGoSum` | 290-340 | go.sum |
| `processCargoToml` | 341-412 | Cargo.toml |
| `processCargoLock` | 413-444 | Cargo.lock |
| `processPackageJson` | 445-491 | package.json |
| `processFlakeNix` | 492-555 | flake.nix |
| `processFlakeLock` | 556-610 | flake.lock |
| `processPyproject` | 611-701 | pyproject.toml |
| `processRequirements` | 702-745 | requirements.txt |

Each follows identical pattern:
1. Read file
2. Parse format-specific content
3. Generate attestations with `storeAttestation()`
4. Return results

**Recommendation:** Create a file processor registry:

```go
// ats/ix/file_processor.go
type FileProcessor interface {
    FilePattern() string                    // "go.mod", "Cargo.toml", etc.
    Parse(content []byte) ([]Dependency, error)
    PredicateFor(dep Dependency) string     // "requires", "depends_on", etc.
}

type ProcessorRegistry struct {
    processors map[string]FileProcessor
}

func (r *ProcessorRegistry) Process(path string, content []byte) (*Result, error) {
    proc := r.GetProcessor(filepath.Base(path))
    deps, err := proc.Parse(content)
    // Common attestation generation logic...
}
```

**Also extract:** The repeated `storeAttestation()` pattern (appears 3 times with identical code):
- `GitIxProcessor.storeAttestation()` (line 648)
- `GitIxProcessor.storeAttestationWithActor()` (line 603)
- `DepsIxProcessor.storeAttestation()` (line 745)

**Time Saved:** ~2-3 hours per new file type support, easier testing of parsers in isolation.

---

### 1.5 Async Job Handler Base

**Problem:** Every job handler repeats the same patterns:

```go
// Payload decoding (every handler)
var payload SpecificPayload
if err := json.Unmarshal(job.Payload, &payload); err != nil {
    return fmt.Errorf("failed to decode payload: %w", err)
}

// Progress updates (every handler)
job.UpdateProgress(current, total)

// Error classification (should be automatic)
ctx := ClassifyError("stage", err)
```

**Recommendation:** Create a base handler with template methods:

```go
// pulse/async/base_handler.go
type BaseHandler[P any] struct {
    name   string
    logger *zap.SugaredLogger
}

func (h *BaseHandler[P]) Name() string { return h.name }

func (h *BaseHandler[P]) DecodePayload(job *Job) (P, error) {
    var payload P
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return payload, fmt.Errorf("failed to decode %s payload: %w", h.name, err)
    }
    return payload, nil
}

// Concrete handlers embed this:
type GitIngestionHandler struct {
    BaseHandler[GitIngestionPayload]
    db *sql.DB
}
```

**Time Saved:** ~30 minutes per new job handler, consistent error handling.

---

## Priority 2: Medium Impact, Medium Frequency

### 2.1 Configuration Access Patterns

**Problem:** Mixed patterns for configuration access:
- Full config load: `cfg, err := am.Load()` (most common)
- Convenience getters: `am.GetString("code.gopls.workspace_root")` (qntx-code)
- Special case: `DB_PATH` env var bypasses Viper in `GetDatabasePath()`

**Issues:**
- `server.dev_mode` used in code but not in Config struct (type safety loss)
- `DB_PATH` vs `QNTX_DATABASE_PATH` inconsistency
- OpenRouter API key not in sensitive bindings (security risk)

**Recommendation:**
1. Add `DevMode bool` to `ServerConfig` struct
2. Document and standardize on full config load pattern
3. Add `openrouter.api_key` to sensitive variable bindings
4. Consider deprecating `DB_PATH` in favor of `QNTX_DATABASE_PATH`

**Time Saved:** Reduced debugging time from consistent patterns.

---

### 2.2 WebSocket Upgrader Factory

**Problem:** WebSocket upgrader configuration duplicated:
- `qntx-code/websocket.go:12`
- `plugin/grpc/bookplugin_test.go:442`

Both define identical configurations with buffer sizes and origin checks.

**Recommendation:**

```go
// plugin/websocket.go
func NewPluginWebSocketUpgrader(checkOrigin func(*http.Request) bool) *websocket.Upgrader {
    return &websocket.Upgrader{
        ReadBufferSize:  1024,
        WriteBufferSize: 1024,
        CheckOrigin:     checkOrigin,
    }
}

// For plugins that allow all origins (testing):
func NewPermissiveUpgrader() *websocket.Upgrader
```

**Time Saved:** ~10 minutes per new WebSocket handler, consistent security.

---

### 2.3 Test Mock Utilities

**Problem:** Hand-crafted mocks repeated across packages:
- `mockQueryStore` in multiple test files
- `mockEmitter` patterns duplicated
- No shared mock builders

**Good existing patterns:**
- `qntxtest.CreateTestDB(t)` - excellent centralized helper
- `testutil.LoadFixtures()` - reusable fixture loading

**Recommendation:** Add mock builders to testutil:

```go
// internal/testing/mocks.go
type MockQueryStoreBuilder struct {
    predicates   []string
    contexts     []string
    attestations []*types.As
}

func NewMockQueryStore() *MockQueryStoreBuilder
func (b *MockQueryStoreBuilder) WithPredicates(p []string) *MockQueryStoreBuilder
func (b *MockQueryStoreBuilder) Build() *MockQueryStore
```

**Time Saved:** ~20 minutes per test file that needs mocks.

---

### 2.4 Job Status Constant Sets

**Problem:** Status lists repeated in queries:
- `'queued', 'running', 'paused'` (lines 146, 166 in store.go)
- `'completed', 'failed'` (lines 265, 299)

**Recommendation:**

```go
// pulse/async/status.go
var ActiveStatuses = []JobStatus{JobStatusQueued, JobStatusRunning, JobStatusPaused}
var TerminalStatuses = []JobStatus{JobStatusCompleted, JobStatusFailed, JobStatusCancelled}

func (s JobStatus) IsActive() bool
func (s JobStatus) IsTerminal() bool
func StatusPlaceholders(statuses []JobStatus) string  // Returns "?, ?, ?"
```

**Time Saved:** Eliminates status list drift between queries.

---

## Priority 3: Lower Impact, Worth Tracking

### 3.1 Validation Error Messages

**Current:** Duplicated validation messages in `alias_store.go`:
```go
// Lines 28-34 AND 133-136
if alias == "" {
    return fmt.Errorf("alias cannot be empty")
}
```

**Recommendation:** Extract to sentinel errors or validation helper.

### 3.2 Rate Limit / Budget Check Pattern

**Current:** Similar check-pause-update pattern in worker.go:
- `checkRateLimit()` (lines 549-584)
- `checkBudget()` (lines 586-617)

**Recommendation:** Extract common "gate" interface:
```go
type JobGate interface {
    Evaluate(job *Job) (blocked bool, reason string, err error)
}
```

### 3.3 Recovery Strategy Consolidation

**Current:** `gradualRecovery()` and `recoverJobsWithInterval()` have overlapping logic.

**Recommendation:** Consolidate into single configurable recovery function.

### 3.4 Plugin HTTP Path Routing

**Current:** Manual path parsing in handlers:
```go
pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/pulse/jobs/"), "/")
```

**Recommendation:** Consider lightweight router or path extraction helpers.

---

## Impact Matrix

| Abstraction | Dev Time Saved | Bug Reduction | Consistency | Effort |
|------------|----------------|---------------|-------------|--------|
| Unified Errors | High | High | High | Medium |
| HTTP Utilities | Medium | Medium | High | Low |
| Query Builder | Medium | High | High | Medium |
| Ingestion Processor | High | Medium | High | Medium |
| Job Handler Base | Medium | Medium | High | Low |
| Config Patterns | Low | Medium | High | Low |
| WebSocket Factory | Low | Low | Medium | Low |
| Test Mocks | Medium | Low | Medium | Low |
| Status Constants | Low | Medium | High | Low |

---

## Implementation Roadmap

### Phase 1: Quick Wins (1-2 days)
1. Status constant sets in pulse/async
2. WebSocket upgrader factory
3. Extend HTTP handler utilities usage

### Phase 2: Foundation (3-5 days)
1. Unified error package design and implementation
2. JSON query builder abstraction
3. Config struct additions (DevMode, sensitive bindings)

### Phase 3: Major Refactoring (1-2 weeks)
1. Ingestion processor registry
2. Job handler base class
3. Migrate existing code to new patterns

### Phase 4: Testing Infrastructure (ongoing)
1. Mock builders in testutil
2. Error chain testing utilities
3. Documentation of patterns

---

## Conclusion

The QNTX codebase has a solid foundation with clear architectural patterns. The identified abstractions focus on:

1. **Eliminating repetition** - DRY principle for common patterns
2. **Improving consistency** - Same patterns everywhere reduce cognitive load
3. **Enabling extensibility** - New features build on abstractions, not copy-paste
4. **Reducing bugs** - Centralized logic means fixes apply everywhere

The highest-ROI improvements are the **unified error system** and **HTTP handler utilities**, as they touch the most code paths and would immediately improve developer experience.
