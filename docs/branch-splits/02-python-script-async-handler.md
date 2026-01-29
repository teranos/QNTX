# Branch Split: Python Script Async Handler

**Source Branch:** `feature/dynamic-ix-routing`
**Target Branch:** `feature/python-script-async-handler`
**Priority:** MEDIUM (useful if Python execution needed elsewhere)
**Status:** Complete with tests, ready to extract

## Overview

Extract a standalone async handler for executing Python scripts stored in attestations. This allows Pulse jobs to execute arbitrary Python code registered via the attestation system.

## Problem Solved

Provides a generic mechanism for running Python scripts as Pulse jobs. Scripts are stored as attestation attributes and executed on-demand. This could be useful beyond IX handlers (e.g., scheduled data processing, automated tasks).

## Files to Extract

1. **`pulse/async/python_handler.go`**
   - New file: Complete async handler implementation
   - Executes Python scripts via `/api/python/execute` endpoint
   - Self-contained, no payload injection logic

2. **`pulse/async/python_handler_test.go`**
   - New file: Comprehensive test suite
   - Tests success, failure, timeout, invalid payload scenarios

## Extraction Steps

```bash
# 1. Create new branch from main
git checkout main
git pull
git checkout -b feature/python-script-async-handler

# 2. Copy files from source branch
git checkout feature/dynamic-ix-routing -- pulse/async/python_handler.go
git checkout feature/dynamic-ix-routing -- pulse/async/python_handler_test.go

# 3. Verify builds
make dev

# 4. Run tests
go test ./pulse/async/...

# 5. Commit
git add pulse/async/python_handler.go pulse/async/python_handler_test.go
git commit -m "Add Python script async handler

Enables Pulse jobs to execute Python scripts stored in attestations.
Handler sends script code to Python plugin via /api/python/execute
and returns results through async job system.

Useful for scheduled Python tasks and dynamic script execution."

# 6. Create PR
gh pr create --base main --title "Add Python script async handler" --body "..."
```

## Handler Details

### Payload Format

```go
type PythonScriptPayload struct {
    ScriptCode string `json:"script_code"` // Python code to execute
    ScriptType string `json:"script_type"` // Type hint (csv, webhook, etc.)
}
```

### Execution Flow

1. Handler receives job with payload containing `script_code`
2. Sends POST to `/api/python/execute` with code
3. Python plugin executes script in isolated environment
4. Returns output or error through async job system

### Usage Example

```go
// Register handler
RegisterHandler(NewPythonScriptHandler("http://localhost:8080", logger))

// Create job
payload := PythonScriptPayload{
    ScriptCode: "import qntx\nqntx.attest(['subject'], ['predicate'], ['context'])",
    ScriptType: "webhook",
}
payloadJSON, _ := json.Marshal(payload)
jobID := CreateJob("python.script", payloadJSON)
```

## Testing Checklist

- [ ] Build succeeds: `make dev`
- [ ] Tests pass: `go test ./pulse/async/python_handler_test.go`
- [ ] Python plugin is running (required for handler to work)
- [ ] Manual test: Create job with simple Python script, verify execution

## Dependencies

**Runtime:**
- Python plugin running at configured URL
- `/api/python/execute` endpoint available

**Build:**
- None - standard library + existing QNTX packages

## Use Cases

1. **Dynamic IX handlers** - Store ingest scripts as attestations, execute on schedule
2. **Scheduled tasks** - Run periodic Python scripts (data cleanup, reporting)
3. **User-defined automation** - Let users write custom Python scripts for workflows
4. **Testing** - Execute test scripts as Pulse jobs

## Decision Point

**Should we extract this?**

**YES if:**
- You plan to use Python scripts in contexts beyond IX handlers
- You want scheduled Python task execution
- You want to enable user-defined automation

**NO if:**
- This handler is only valuable for dynamic IX routing
- You don't see other use cases for running Python scripts via Pulse
- You prefer to keep it bundled with IX routing feature

## Risk Assessment

**LOW**
- Self-contained handler, no modifications to existing code
- Well-tested (4 test cases covering success, failure, timeout, invalid payload)
- No database changes
- Only runs when explicitly registered and invoked

## Commit Message

```
Add Python script async handler

Enables Pulse jobs to execute Python scripts stored in attestations.
Handler sends script code to Python plugin via /api/python/execute
and returns results through async job system.

Useful for scheduled Python tasks and dynamic script execution.
```
