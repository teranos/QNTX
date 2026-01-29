# Dynamic IX Routing with Python Script Attestations

**Status:** In Progress
**Branch:** `feature/dynamic-ix-routing`
**Started:** 2026-01-28

## Vision

Enable dynamic, user-extensible ingestion handlers for IX glyphs by storing Python scripts as attestations in the graph.

**Key Benefits:**
- Python ingest scripts stored AS attestations (e.g., `script:ingest:git`)
- IX routing: `ix git <url>` queries graph for script, executes via Rust-Python interpreter
- Scripts create attestations using existing `attest()` API
- Users can create custom handlers (`ix csv`, `ix jd`, `ix cts1`) without touching Go code
- Scripts are queryable, versionable, shareable through attestation graph

## Architecture

### Current Flow (Hardcoded)
```
IX glyph: "ix https://github.com/..."
  ↓
ATS parser: hardcodes "ixgest.git" handler
  ↓
Pulse async queue
  ↓
Worker: Execute Go handler (qntx-code plugin)
  ↓
Attestations created
```

### New Flow (Dynamic)
```
Python glyph: Write ingest script
  ↓
Publish as "script:ingest:git" attestation
  ↓
IX glyph: "ix git https://github.com/..."
  ↓
ATS parser: Query attestations for "script:ingest:git"
  ↓
Load Python code from attestation
  ↓
Pulse async queue with "python.script" handler
  ↓
Worker: Execute Python via /api/python/execute
  ↓
Python script calls attest() to create attestations
```

## Implementation Phases

### Phase 1: Python Script Handler (Backend)

**Goal:** Enable Pulse async jobs to execute Python scripts

**Files:**
- `pulse/async/python_handler.go` (new)
- `cmd/qntx/commands/server.go` (modify)

**Implementation:**

1. **Create PythonScriptHandler** (`pulse/async/python_handler.go`):
```go
type PythonScriptHandler struct {
    httpClient *http.Client
    pythonURL  string  // URL to Python plugin
    logger     *zap.SugaredLogger
}

func (h *PythonScriptHandler) Name() string {
    return "python.script"
}

func (h *PythonScriptHandler) Execute(ctx context.Context, job *Job) error {
    // 1. Extract script code from job.Payload
    // 2. POST to /api/python/execute with code
    // 3. Handle response (stdout, stderr, errors)
    // 4. Return result
}
```

2. **Register handler** in `cmd/qntx/commands/server.go`:
```go
pythonHandler := async.NewPythonScriptHandler(httpClient, pythonPluginURL, logger)
handlerRegistry.Register(pythonHandler)
```

### Phase 2: Store Scripts as Attestations (Backend)

**Goal:** API to save/load Python scripts from attestation graph

**Files:**
- `server/scripts.go` (new)
- `server/routes.go` (modify)

**Endpoints:**

1. **POST /api/scripts/publish**
   - Request: `{name: "git", code: "...", language: "python"}`
   - Creates attestation with:
     - Subject: `script:ingest:{name}`
     - Predicate: `defines`
     - Context: `python`
     - Attributes: `{code: "...", language: "python", created_at: timestamp}`
   - Returns: `{id: "ASID...", name: "git"}`

2. **GET /api/scripts/{name}**
   - Query: `subject = "script:ingest:{name}"`
   - Returns: `{name: "git", code: "...", created_at: "...", id: "ASID..."}`
   - 404 if not found

3. **GET /api/scripts**
   - Lists all available ingest scripts
   - Returns: `[{name: "git", created_at: "...", id: "ASID..."}, ...]`

**Attestation Schema:**
```json
{
  "subjects": ["script:ingest:git"],
  "predicates": ["defines"],
  "contexts": ["python"],
  "attributes": {
    "code": "def execute(payload):\n    repo_url = payload['url']\n    # ...\n    attest(...)",
    "language": "python",
    "version": "1",
    "created_by": "user@example.com"
  }
}
```

### Phase 3: Dynamic IX Routing (Backend)

**Goal:** Route `ix {type} {input}` to appropriate Python script

**Files:**
- `server/ats_parser.go` (modify)

**Changes to `parseIxCommand()`:**

```go
func parseIxCommand(tokens []string, jobID string, store ats.AttestationStore) (*ParsedATSCode, error) {
    if len(tokens) == 0 {
        return nil, errors.New("ix command requires a target")
    }

    // Extract type (first token or auto-detect)
    scriptType := tokens[0]
    var input []string

    // Check for explicit subcommand
    if !strings.HasPrefix(scriptType, "http") && !strings.HasPrefix(scriptType, "/") {
        // Explicit type: ix git <url>
        input = tokens[1:]
    } else {
        // Auto-detect: ix <url> (default to git)
        scriptType = "git"
        input = tokens
    }

    // Query attestation store for script
    filters := ats.AttestationFilter{
        Subjects: []string{fmt.Sprintf("script:ingest:%s", scriptType)},
        Limit: 1,
    }
    attestations, err := store.GetAttestations(filters)

    if err != nil || len(attestations) == 0 {
        // Fallback: If type == "git", try hardcoded Go handler
        if scriptType == "git" {
            return parseIxGitCommand(input, jobID)  // Backward compatibility
        }
        return nil, errors.Newf("No ingest script '%s' found. Create one using Python glyph.", scriptType)
    }

    // Extract script code from attestation
    scriptCode := attestations[0].Attributes["code"].(string)

    // Build payload for Python handler
    payload := map[string]interface{}{
        "script_code": scriptCode,
        "script_type": scriptType,
        "input": input,
    }

    payloadJSON, _ := json.Marshal(payload)

    return &ParsedATSCode{
        HandlerName: "python.script",
        Payload:     payloadJSON,
        SourceURL:   strings.Join(input, " "),  // For deduplication
    }, nil
}
```

**Backward Compatibility:**
- If `script:ingest:git` attestation doesn't exist, fall back to Go `ixgest.git` handler
- Gradual migration path for existing IX glyphs

### Phase 4: UI - Publish Script Button (Frontend)

**Goal:** Python glyph can publish scripts as ingest handlers

**Files:**
- `web/ts/components/glyph/python-glyph.ts` (modify)
- `web/ts/api/scripts.ts` (new)

**Changes:**

1. **Add publish button** to Python glyph toolbar:
```typescript
// Add button with ⬆ icon next to run button
const publishButton = createButton({
    icon: '⬆',
    title: 'Publish as Ingest Script',
    onClick: handlePublish
});
```

2. **Implement publish flow:**
```typescript
async function handlePublish() {
    // 1. Prompt for script name
    const name = prompt('Enter ingest script name (e.g., git, csv, jd):');
    if (!name) return;

    // 2. Validate name (alphanumeric + hyphen only)
    if (!/^[a-z0-9-]+$/.test(name)) {
        showError('Name must be lowercase alphanumeric (a-z, 0-9, -)');
        return;
    }

    // 3. Check if exists
    const existing = await getScript(name);
    if (existing) {
        if (!confirm(`Script '${name}' already exists. Overwrite?`)) {
            return;
        }
    }

    // 4. Publish
    const code = getCurrentCode();
    await publishScript({name, code, language: 'python'});

    showSuccess(`Published ingest script: ${name}`);
}
```

3. **Create API client** (`web/ts/api/scripts.ts`):
```typescript
export async function publishScript(params: {
    name: string;
    code: string;
    language: string;
}): Promise<{id: string; name: string}> {
    const response = await fetch('/api/scripts/publish', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(params)
    });
    return response.json();
}

export async function getScript(name: string): Promise<{
    name: string;
    code: string;
    created_at: string;
    id: string;
} | null> {
    const response = await fetch(`/api/scripts/${name}`);
    if (response.status === 404) return null;
    return response.json();
}

export async function listScripts(): Promise<Array<{
    name: string;
    created_at: string;
    id: string;
}>> {
    const response = await fetch('/api/scripts');
    return response.json();
}
```

### Phase 5: IX Glyph Script Discovery (Frontend)

**Goal:** IX glyph shows available ingest types

**Files:**
- `web/ts/components/glyph/ix-glyph.ts` (modify)

**Changes:**

1. **Fetch available scripts on mount:**
```typescript
async function initIxGlyph(element: HTMLElement) {
    // ... existing init code

    // Fetch available ingest scripts
    const scripts = await listScripts();
    availableTypes = ['git', ...scripts.map(s => s.name)];

    updatePlaceholder(availableTypes);
}
```

2. **Update placeholder text:**
```typescript
function updatePlaceholder(types: string[]) {
    textarea.placeholder = `Enter URL or specify type:
Examples:
  https://github.com/user/repo
  git https://github.com/user/repo
  ${types.slice(1).join('\n  ')} <input>`;
}
```

3. **Show helpful error:**
```typescript
function handleError(error: string) {
    if (error.includes('No ingest script')) {
        const match = error.match(/No ingest script '(\w+)'/);
        if (match) {
            const type = match[1];
            statusElement.innerHTML = `
                ✗ No ingest script '${type}' found.
                <a href="#" onclick="spawnPythonGlyph()">Create one in Python glyph</a>
            `;
            return;
        }
    }

    // Default error display
    statusElement.textContent = `✗ ${error}`;
}
```

### Phase 6: Testing & Documentation

**Goal:** Production-ready with tests and examples

**Files:**
- `docs/examples/ingest-git.py` (new)
- `docs/guides/custom-ingest-scripts.md` (new)
- `server/ats_parser_test.go` (modify)
- `pulse/async/python_handler_test.go` (new)

**Example Script** (`docs/examples/ingest-git.py`):
```python
"""
Example ingest script for git repositories.

This script is published as script:ingest:git and executed when
users run: ix git https://github.com/user/repo

Payload format:
{
  "script_type": "git",
  "input": ["https://github.com/user/repo"]
}
"""

def execute(payload):
    repo_url = payload['input'][0]

    # Clone repository (use subprocess or gitpython)
    import subprocess
    result = subprocess.run(['git', 'clone', repo_url, '/tmp/repo'],
                          capture_output=True)

    if result.returncode != 0:
        raise Exception(f"Git clone failed: {result.stderr.decode()}")

    # Parse files and create attestations
    files = list_files('/tmp/repo')

    for file_path in files:
        content = read_file(file_path)

        # Create attestation for each file
        attest(
            subjects=[f"file:{file_path}"],
            predicates=["contains"],
            contexts=[f"repo:{repo_url}"],
            attributes={"content": content, "size": len(content)}
        )

    return {"files_ingested": len(files), "repo": repo_url}
```

**Tests:**

1. **ATS Parser Test** (`server/ats_parser_test.go`):
```go
func TestParseIxCommand_DynamicRouting(t *testing.T) {
    // Create mock attestation store with script
    store := createMockStore()
    store.CreateAttestation(&types.As{
        Subjects: []string{"script:ingest:csv"},
        Attributes: map[string]interface{}{
            "code": "def execute(payload): ...",
        },
    })

    // Test parsing
    result, err := parseIxCommand([]string{"csv", "data.csv"}, "job123", store)

    assert.NoError(t, err)
    assert.Equal(t, "python.script", result.HandlerName)
    // ... verify payload contains script code
}
```

2. **Python Handler Test** (`pulse/async/python_handler_test.go`):
```go
func TestPythonScriptHandler_Execute(t *testing.T) {
    // Create mock Python plugin server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Return success response
        json.NewEncoder(w).Encode(map[string]interface{}{
            "success": true,
            "result": "OK",
        })
    }))
    defer server.Close()

    handler := NewPythonScriptHandler(http.DefaultClient, server.URL, logger)

    job := &Job{
        Payload: []byte(`{"script_code": "print('test')", "input": ["test"]}`),
    }

    err := handler.Execute(context.Background(), job)
    assert.NoError(t, err)
}
```

**Documentation** (`docs/guides/custom-ingest-scripts.md`):
- How to write ingest scripts
- Available Python APIs (`attest()`, payload format)
- Publishing workflow
- Examples for different data sources
- Troubleshooting common errors

## Key Design Decisions

### Script Attestation Schema

**Why this format:**
```
Subject: script:ingest:{name}
Predicate: defines
Context: python
```

- **Subject prefix** (`script:ingest:`) enables easy querying
- **Name** becomes the IX type (`ix {name} <input>`)
- **Predicate** "defines" indicates this attestation defines a handler
- **Context** "python" indicates language/runtime

### Backward Compatibility

**Strategy:**
1. Keep existing `ixgest.git` Go handler
2. Check for Python script first, fall back to Go
3. Gradual migration: Users can override Git handler with Python version
4. No breaking changes to existing IX glyphs

### Error Handling

**User-friendly errors:**
- "No ingest script 'xyz' found. Create one using Python glyph."
- "Script 'abc' failed: {full error from Python execution}"
- Full error context passed through to IX glyph status display
- Link to Python glyph creation from IX error message

### Security Considerations

**Script execution:**
- Python scripts run in qntx-python plugin (already sandboxed)
- Same security model as Python glyphs
- Timeout enforcement (default 30s)
- Resource limits inherited from plugin

**Attestation validation:**
- Validate script name format (alphanumeric + hyphen)
- Store creator identity in attestation attributes
- Future: Add script signing/verification

## Implementation Order

1. ✅ **Phase 1** - Python handler (enables execution) - COMPLETE
   - `pulse/async/python_handler.go` created
   - `pulse/async/python_handler_test.go` created (6 tests, all passing)
2. ✅ **Phase 2** - Handler registration - COMPLETE
   - Registered in `cmd/qntx/commands/pulse.go`
   - Handler calls Python plugin at `http://localhost:877/api/python/execute`
3. ✅ **Phase 3** - Script storage API - COMPLETE
   - Python APIs implemented in `qntx-python/src/atsstore.rs`
   - `publish_as_ingest(name, code=None)` - Publishes script as attestation
   - `get_ingest_script(name)` - Retrieves script by querying Predicate="handles", Context="{name}-ingestion"
   - `list_ingest_scripts()` - Lists all scripts with Predicate="handles"
   - Correct attestation model: Subject=ASID, Predicate="handles", Context="{name}-ingestion"
4. ✅ **Phase 4** - Dynamic routing - COMPLETE
   - Modified `server/ats_parser.go` to query attestations before falling back to hardcoded handlers
   - `parseIxCommand()` queries for Python scripts using Predicate="handles", Context="{type}-ingestion"
   - Extracts script code from attributes and builds payload for Python handler
   - Backward compatible: Falls back to `ixgest.git` if no Python script found
   - Helpful error message: "no ingest script 'xyz' found. Create one using Python glyph with publish_as_ingest('xyz')"
5. ⏳ **Phase 5** - Publish button (user workflow)
6. ⏳ **Phase 6** - IX discovery (polish UX)
7. ⏳ **Phase 7** - Docs/tests (production ready)

**Estimated effort:** 6-8 hours of focused work across backend/frontend

## Future Enhancements

### Script Versioning
- Store multiple versions of scripts
- Query for specific version: `script:ingest:git:v2`
- Default to latest version

### Script Marketplace
- Share scripts across users/organizations
- Browse available ingest handlers
- Rate/review scripts

### Visual Script Editor
- Click pencil on IX glyph → Opens minimizable Python editor
- Shows current script code for the type
- Edit → Auto-publishes new version
- Test button to run with sample data

### Script Templates
- Provide templates for common patterns
- "New Git Ingest Script" → Generates boilerplate
- Template library in docs

### Monitoring & Observability
- Track script execution stats (success rate, duration)
- Alert on script failures
- Script performance dashboard

## References

- **Python Execution:** `/qntx-python/src/engine.rs`
- **Attestation Storage:** `/ats/storage/sql_store.go`
- **Handler Registry:** `/pulse/async/handler.go`
- **ATS Parser:** `/server/ats_parser.go`
- **IX Glyph:** `/web/ts/components/glyph/ix-glyph.ts`
