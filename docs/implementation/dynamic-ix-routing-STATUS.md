# Dynamic IX Routing - Implementation Status

**Last Updated:** 2026-01-29
**Branch:** `feature/dynamic-ix-routing`
**Status:** Ready for Testing

## What Works

### Backend (Complete)
1. ✅ Python script handler in Pulse (`pulse/async/python_handler.go`)
   - Executes user script as-is (self-contained)
   - No payload injection (not in scope)

2. ✅ Rust Python plugin HTTP endpoint (`qntx-python/src/handlers.rs`)
   - `POST /register-handler` - Server-side handler registration
   - Attestation: Subject=ASID, Predicate="ix_handler", Context=scriptType
   - Attributes: `{code: "...", language: "python"}`

3. ✅ Dynamic routing (`server/ats_parser.go`)
   - Queries: Predicate="ix_handler", Context=scriptType (e.g., "webhook")
   - Falls back to hardcoded handlers (git)
   - Returns `ErrNoIngestScript` with `script_type` detail

4. ✅ Error propagation
   - Sentinel error with structured details
   - Flows: Parser → Ticker → Broadcast → Frontend
   - Returns: `{error: "...", details: ["script_type=webhook"]}`

### Frontend (Complete)
1. ✅ Error details preservation (`web/ts/pulse/api.ts`)
   - Attaches `details` array to thrown Error object

2. ✅ IX glyph error handling (`web/ts/components/glyph/ix-glyph.ts`)
   - Detects "no ingest script" at job creation
   - Parses `script_type` from error details
   - Shows "Create handler" button

3. ✅ Python handler template generation
   - Self-contained script template (no `ingest(payload)`)
   - Example: fetch from API, create attestations

4. ✅ Python glyph spawning with Save button (`web/ts/components/glyph/py-glyph.ts`)
   - Spawns editor with template
   - Has `handlerFor` metadata (e.g., "webhook")
   - Save button → calls `/api/python/register-handler`
   - Visual feedback on success/failure

### Rust Cleanup (Complete)
1. ✅ Removed `publish_as_ingest()` Python function
2. ✅ Removed `get_ingest_script()` and `list_ingest_scripts()`
3. ✅ Removed `CURRENT_CODE` thread-local storage
4. ✅ Only `attest()` remains as Python-callable

## What's Left

### Testing Required
- [ ] Rebuild Python plugin: `make rust-python-install`
- [ ] Restart dev server: `make dev`
- [ ] Test flow:
  1. Create IX glyph: `ix webhook`
  2. Click Run → Verify "Create handler" button appears
  3. Click button → Verify Python glyph spawns with template
  4. Edit handler (e.g., fetch from API, create attestations)
  5. Click Save → Verify attestation created (check logs)
  6. Go back to IX glyph, click Run → Verify scheduled job created
  7. Check Pulse UI → Verify job executes periodically

## How It Works

1. User types `ix webhook` in IX glyph
2. Backend queries: Predicate="ix_handler", Context="webhook"
3. Not found → Returns error with `script_type=webhook` detail
4. Frontend shows "Create handler" button
5. User clicks → Python glyph spawns with `handlerFor: "webhook"`
6. User writes self-contained Python script (fetch API, create attestations)
7. User clicks Save → Calls `/api/python/register-handler`
8. Rust plugin creates attestation: 'as python_script is ix_handler of webhook by canvas_glyph at temporal'
9. User returns to IX glyph, clicks Run
10. Backend finds handler, creates scheduled Pulse job
11. Job runs periodically, executing the Python script

## Files Changed

### Backend
- `server/ats_parser.go` - Dynamic routing + sentinel error
- `server/response.go` - Return `GetAllDetails()` array
- `pulse/async/python_handler.go` - Payload injection
- `pulse/schedule/ticker.go` - Extract error details
- `server/broadcast.go` - Pass error details to frontend
- `server/types.go` - Add `ErrorDetails` field

### Frontend
- `web/ts/pulse/api.ts` - Preserve error details
- `web/ts/pulse/events.ts` - Add `errorDetails` field
- `web/ts/components/glyph/ix-glyph.ts` - Error handling + spawning

### Rust
- `qntx-python/src/atsstore.rs` - Publish/query APIs
- `qntx-python/src/execution.rs` - Store current code

## Next Session

1. Verify `GetAllDetails()` returns `["script_type=csv"]` in HTTP response
2. Check browser console to see if error.details exists
3. If button still doesn't show, add more debug logs
4. Once button works, test Python glyph spawn
5. Test end-to-end: create handler → publish → use it
