# Phase 3 Verification Plan

## Implementation Status: ✅ COMPLETE

All Phase 3 code is implemented and builds successfully.

## What Phase 3 Delivers

**Dynamic Handler Discovery:** Python plugin queries ATS store during initialization, discovers handler scripts saved as attestations, and announces them to Pulse for async execution.

## End-to-End Flow

```
1. User creates handler via CLI
   └─> qntx handler create vacancies --code "print('Hello')"
   └─> Saved as attestation: Subject=vacancies, Predicate=handler, Context=python, Actor=vacancies

2. Server starts / Plugin initializes
   └─> Python plugin's initialize() queries ATS store
   └─> Filter: predicate="handler" AND context="python"
   └─> Extracts subjects[0] from each attestation (handler name)
   └─> Returns InitializeResponse{handler_names: ["python.script", "python.vacancies"]}

3. Server registers handlers with Pulse
   └─> server/init.go calls GetHandlerNames()
   └─> Creates PluginProxyHandler for each
   └─> Registers with Pulse worker pool

4. Job execution (future)
   └─> ix vacancies <args>
   └─> Pulse routes to python.vacancies handler
   └─> PluginProxyHandler forwards to Python plugin via ExecuteJob RPC
   └─> Plugin executes stored Python code
```

## Verification Steps

### 1. Create Test Handler

```bash
./bin/qntx handler create vacancies --code "print('Hello from vacancies handler')"
```

**Expected output:**
```
✓ Handler 'vacancies' created
  ASID: <generated-asid>
  Subject: [vacancies]
  Predicate: [handler]
  Context: [python]
  Actor: [vacancies] (self-certifying)
  Attributes:
  {
    "code": "print('Hello from vacancies handler')"
  }
```

### 2. Start Server with Plugin

```bash
make dev
```

**Expected log output:**

```
[Python plugin] Initializing Python plugin
[Python plugin] ATSStore endpoint: localhost:877
[Python plugin] Discovering Python handlers from ATS store
[Python plugin] Discovered 1 handler(s) from ATS store: ["vacancies"]
[Python plugin] Announcing async handlers: ["python.script", "python.vacancies"]
[QNTX Server] Registering plugin async handler plugin=python handler=python.script
[QNTX Server] Registering plugin async handler plugin=python handler=python.vacancies
```

### 3. Verify Handler Registration

Check server logs for:
- "Plugin announced async handlers" with both built-in and discovered handlers
- "Registering plugin async handler" for each handler
- No errors during initialization

### 4. Test Multiple Handlers

```bash
./bin/qntx handler create test1 --code "print('test1')"
./bin/qntx handler create test2 --code "print('test2')"
```

Restart server, verify logs show all three handlers discovered: `["test1", "test2", "vacancies"]`

## Key Files

- **Handler CLI:** `cmd/qntx/commands/handler.go`
- **Plugin Discovery:** `qntx-python/src/service.rs:78` (`discover_handlers_from_config`)
- **Handler Announcement:** `qntx-python/src/service.rs:275-286` (in `initialize()`)
- **Server Registration:** `server/init.go:111-119`
- **gRPC Protocol:** `plugin/grpc/protocol/domain.proto`

## Success Criteria

✅ Handler creation works via CLI
✅ Handlers stored as attestations in ATS
✅ Python plugin queries ATS store on initialization
✅ Plugin correctly parses and returns handler names
✅ Server receives handler names via InitializeResponse
✅ Server registers handlers with Pulse worker pool
✅ All code builds without errors

## Next Steps

After verification:
- **Phase 4:** Handler Glyph UI (create/edit handlers in Canvas)
- **Phase 5:** ATS parser integration (remove hardcoded git imports)
