# Branch Split: Structured Error Details

**Source Branch:** `feature/dynamic-ix-routing`
**Target Branch:** `feature/structured-error-details`
**Priority:** HIGH (immediate value, stable)
**Status:** Ready to extract

## Overview

Extract error detail propagation infrastructure from the dynamic IX routing work. This provides rich, structured error information across the backend-to-frontend stack, useful for all features.

## Problem Solved

Currently, error details from `cockroachdb/errors` are flattened into strings before being sent to the frontend. This loses valuable structured debugging information. This change preserves error details as string arrays through the entire stack.

## Files to Extract

### Backend (4 files)

1. **`server/response.go`**
   - Change: Return `errors.GetAllDetails(err)` array instead of `errors.FlattenDetails(err)` string
   - Line ~48: `"details": errors.GetAllDetails(err),`

2. **`server/types.go`**
   - Change: Add `ErrorDetails []string` field to `PulseExecutionFailedMessage`
   - Line ~171: New field in struct

3. **`server/broadcast.go`**
   - Change: Accept and pass `errorDetails []string` parameter
   - Lines ~248, ~623: Update `BroadcastPulseExecutionFailed()` signature and implementation
   - Note: One call site passes `nil` (async job completion callback) - this is correct

4. **`pulse/schedule/ticker.go`**
   - Change: Extract error details and pass to broadcaster
   - Lines ~296-300: Add `errorDetails := errors.GetAllDetails(err)` and pass to broadcast

### Frontend (2 files)

5. **`web/ts/pulse/events.ts`**
   - Change: Add `errorDetails?: string[]` to `ExecutionFailedDetail` interface
   - Line ~47: New optional field

6. **`web/ts/pulse/api.ts`**
   - Change: Attach error details from API response to Error object
   - Preserves details for UI components to parse

## Extraction Steps

```bash
# 1. Create new branch from main
git checkout main
git pull
git checkout -b feature/structured-error-details

# 2. Cherry-pick the commit (or manually apply changes)
# Since there's only one commit in source branch, we'll manually apply

# 3. Apply changes file-by-file
# (Use Edit tool for each file listed above)

# 4. Verify builds
make dev

# 5. Test error propagation
# Create a failing Pulse job and verify error details appear in frontend

# 6. Commit
git add server/response.go server/types.go server/broadcast.go pulse/schedule/ticker.go web/ts/pulse/events.ts web/ts/pulse/api.ts
git commit -m "Add structured error detail propagation

Preserve error details from cockroachdb/errors as string arrays
through the entire stack (backend → broadcast → frontend).

Enables richer debugging information in UI without flattening
structured error context."

# 7. Create PR
gh pr create --base main --title "Add structured error detail propagation" --body "..."
```

## Testing Checklist

- [ ] Build succeeds: `make dev`
- [ ] TypeScript type generation: `make types`
- [ ] Create a Pulse job that will fail (e.g., bad git URL)
- [ ] Verify error details appear in browser console
- [ ] Verify error details are structured as array, not flattened string

## Dependencies

**None** - This is pure infrastructure, no dependencies on other feature work.

## Value Proposition

- **Debugging:** Richer error context in UI helps diagnose issues faster
- **Extensibility:** Future features can attach structured details to errors
- **Foundation:** Enables features like dynamic IX routing's "Create handler" button (which parses error details)

## Risk Assessment

**LOW**
- Purely additive (adds `errorDetails` field, doesn't remove existing `errorMessage`)
- Backward compatible (frontend handles missing `errorDetails` gracefully with `?:`)
- Small surface area (6 files)
- No business logic changes

## Commit Message

```
Add structured error detail propagation

Preserve error details from cockroachdb/errors as string arrays
through the entire stack (backend → broadcast → frontend).

Enables richer debugging information in UI without flattening
structured error context.
```
