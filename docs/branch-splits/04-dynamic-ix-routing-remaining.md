# Branch: Dynamic IX Routing (Remaining Work)

**Current Branch:** `feature/dynamic-ix-routing`
**Priority:** COMPLETE TESTING BEFORE EXTRACTION
**Status:** Untested, per PR description

## What Stays in This Branch

After extracting the stable infrastructure pieces (branches 01-03), this branch should contain only the core dynamic IX routing feature.

## Remaining Files

### Backend (3 files)

1. **`server/ats_parser.go`**
   - Dynamic handler lookup via attestation queries
   - Fallback to hardcoded handlers (git)
   - `ErrNoIngestScript` sentinel error with `script_type` detail
   - Query: Predicate="ix_handler", Context=scriptType

2. **`qntx-python/src/handlers.rs`**
   - `POST /register-handler` endpoint
   - Creates attestation: Subject=ASID, Predicate="ix_handler", Context=scriptType
   - Attributes: `{code: "...", language: "python"}`

3. **`qntx-python/src/service.rs`**
   - Wire up `/register-handler` endpoint (minor change)

### Frontend (2 files)

4. **`web/ts/components/glyph/ix-glyph.ts`**
   - Detect "no ingest script" error
   - Parse `script_type` from error details
   - Show "Create handler" button
   - Spawn Python glyph with template

5. **`web/ts/components/glyph/py-glyph.ts`**
   - Handler template generation
   - `handlerFor` metadata
   - Save button → calls `/api/python/register-handler`
   - Visual feedback

### Configuration

6. **`cmd/qntx/commands/pulse.go`**
   - Register Python script handler (minor addition)

7. **`server/pulse_schedules.go`**
   - Minor update to pass db to parser

### Documentation

8. **`docs/implementation/dynamic-ix-routing.md`**
9. **`docs/implementation/dynamic-ix-routing-STATUS.md`**

### Build

10. **`qntx-python/Cargo.toml`** - Version bump
11. **`Cargo.lock`** - Dependency update

## Dependencies After Split

This branch will depend on:
- **Branch 01** (`structured-error-details`) - MUST merge first
  - Required for error detail parsing in IX glyph
- **Branch 02** (`python-script-async-handler`) - MUST merge first
  - Required for executing registered Python handlers
- **Branch 03** (`atsstore-query-helpers`) - Optional
  - Makes code cleaner but not strictly required

## Cleanup Steps Before Testing

```bash
# 1. Ensure dependencies are merged
git checkout feature/dynamic-ix-routing
git rebase main  # Pick up merged dependencies

# 2. Remove extracted code (if already extracted to other branches)
# This step depends on whether other branches merged first

# 3. Update documentation to reflect split
# Remove references to infrastructure pieces now in other branches
```

## Testing Plan (REQUIRED Before Merge)

Per PR description: "Not tested end-to-end", "may not build cleanly"

### Build Verification
- [ ] `make rust-python-install` succeeds
- [ ] `make dev` starts without errors
- [ ] `make types` generates correct TypeScript types

### End-to-End Flow
- [ ] Create IX glyph with `ix webhook`
- [ ] Click Run
- [ ] Verify "Create handler" button appears
- [ ] Click button
- [ ] Verify Python glyph spawns with template code
- [ ] Edit handler (e.g., fetch API, create attestations)
- [ ] Click Save
- [ ] Verify success message
- [ ] Check backend logs for attestation creation
- [ ] Return to IX glyph, click Run again
- [ ] Verify scheduled job created (no error)
- [ ] Check Pulse UI for job
- [ ] Wait for job to execute
- [ ] Verify execution succeeded

### Error Handling
- [ ] Test with invalid handler name (special chars)
- [ ] Test with empty code
- [ ] Test with Python syntax errors
- [ ] Verify error messages are clear

### Edge Cases
- [ ] Multiple handlers with same script type (should use latest?)
- [ ] Delete handler (how? manual attestation deletion?)
- [ ] Update handler (create new attestation or edit existing?)

## Known Issues (from PR)

> "⚠️ WARNING: This PR is in rough shape"
> - Build failed due to disk space issues (needs verification)
> - 22 files changed for what should be simpler (now reduced after split)
> - Significant error detail plumbing (now in separate branch)
> - Template went through multiple rewrites

## Post-Testing Cleanup

Once testing is complete:

1. **Remove dead code**
   - Any leftover experimental attempts
   - Unused imports
   - Debug logging

2. **Simplify**
   - Can the flow be more straightforward?
   - Are all 22 files (now ~11 after split) necessary?

3. **Documentation**
   - Update STATUS.md with test results
   - Add user-facing docs on how to create IX handlers
   - Add developer docs on handler registration pattern

4. **Performance**
   - Does attestation query happen on every parse? Cache?
   - Is handler lookup fast enough?

## Merge Criteria

**DO NOT MERGE until:**
- ✅ All dependencies merged (`structured-error-details`, `python-script-async-handler`)
- ✅ All tests pass (build, unit, end-to-end)
- ✅ Code reviewed for simplicity
- ✅ Documentation complete
- ✅ No known issues

## Future Enhancements (Post-Merge)

- Handler versioning (multiple versions of same script type)
- Handler discovery UI (list all registered handlers)
- Handler testing (dry-run before scheduling)
- Handler sharing (export/import handler code)
- Handler marketplace (community-contributed handlers)

## Commit Message (After Testing)

```
Add dynamic IX handler registration via UI

Enables users to create custom ingest handlers through the canvas:
1. Run IX glyph with unknown type (e.g., 'ix webhook')
2. Click "Create handler" button
3. Write Python script in spawned editor
4. Save to register as attestation
5. Re-run IX glyph to schedule job

Handlers are stored as attestations with predicate 'ix_handler'
and context matching the script type. Backend queries attestations
before falling back to hardcoded handlers.
```

## Notes

This feature represents a significant shift in QNTX's extensibility model:
- Users can define new ingest types without Go code
- Attestations become executable code storage
- Canvas becomes a programming environment

Consider implications for:
- Security (arbitrary code execution)
- Reliability (user scripts may be buggy)
- Discoverability (how do users find handlers?)
- Sharing (can handlers be exported/imported?)
