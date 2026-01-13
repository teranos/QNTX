# Prompt Editor Attestations - Handover Document

**Branch:** `claude/prompt-editor-attestations-XVsRo`
**Date:** 2026-01-13
**Status:** Ready for manual verification and PR

## Summary

Implemented an n8n-style prompt editor that ties attestation data to LLM prompts using the pattern:
```
ax [query] so prompt [template]
```

## What Was Implemented

### Core Components

1. **Template Engine** (`ats/prompt/template.go`)
   - `{{field}}` interpolation for attestation fields
   - Supported fields: subject(s), predicate(s), context(s), actor(s), temporal, id, source, attributes
   - Plural forms return JSON arrays

2. **Prompt Storage** (`ats/prompt/store.go`)
   - Prompts stored as attestations (predicate: `prompt-template`, context: `prompt-library`)
   - Version tracking: new version created when saving same name (limit: 16 versions)
   - Fields: name, template, system_prompt, ax_pattern, provider, model

3. **Action Parser** (`ats/prompt/action.go`)
   - Parses `so prompt "template" with "system" model X provider Y`
   - Uses state machine, integrates with existing ax parser

4. **Pulse Handler** (`ats/prompt/handler.go`)
   - Handler name: `prompt.execute`
   - Supports scheduled execution via Pulse

### Server Endpoints (`server/prompt_handlers.go`)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/prompt/preview` | POST | Preview ax query results |
| `/api/prompt/execute` | POST | One-shot prompt execution |
| `/api/prompt/list` | GET | List all saved prompts |
| `/api/prompt/save` | POST | Save/create prompt |
| `/api/prompt/{name}/versions` | GET | Get version history |

### UI Components

1. **Prompt Editor Window** (`web/ts/prompt-editor-window.ts`)
   - Ax query input, template editor with field suggestions
   - Provider/model selection (OpenRouter/Ollama)
   - Preview, Execute, Save buttons
   - Version badge display

2. **SO Panel** (`web/ts/so-panel.ts`)
   - Prompt history panel (similar to hixtory for ix)
   - Shows saved prompts with versions
   - "+" button to create new prompt

3. **Integration Points**
   - Symbol palette: clicking ⟶ opens SO panel
   - ATS blocks in Prose: "Inspect Prompt" button when `so prompt` detected
   - Pulse job detail: "Inspect Prompt" button for prompt jobs

## Testing Status

**All tests pass:**
```
go test ./ats/prompt/... -v
PASS
ok  	github.com/teranos/QNTX/ats/prompt	0.068s
```

Tests cover:
- Prompt storage as attestations
- Version increments on update
- All optional fields preserved
- Template validation (rejects unknown fields)
- Name/template required validation

## Known Gaps / TODOs

### 1. LLM Provider Integration
- **Location:** `ats/prompt/handler.go:Execute()`
- **Current:** Placeholder that returns mock response
- **TODO:** Integrate with `ai/` package (OpenRouter, Ollama)
- **Priority:** Required before production use

### 2. Temporal Cursor Persistence
- **Location:** `ats/prompt/handler.go`
- **Current:** Payload has `TemporalCursor` field but not persisted between runs
- **TODO:** Store cursor in job metadata for incremental processing
- **Priority:** Required for continuous intelligence mode

### 3. Result Attestation Creation
- **Location:** `ats/prompt/handler.go:Execute()`
- **Current:** Creates result attestation with response
- **TODO:** Verify attestation schema matches expected format
- **Priority:** Medium

## Manual Verification Checklist

### SO Panel
- [ ] Click ⟶ in symbol palette → SO panel opens
- [ ] Panel shows list of saved prompts (empty if none)
- [ ] Click "+" → Prompt editor opens in new mode
- [ ] Click existing prompt → Opens in editor with fields populated

### Prompt Editor
- [ ] Enter ax query, template → Preview shows matching attestations
- [ ] Execute → Shows results (mock until LLM integrated)
- [ ] Save with name → Version badge appears
- [ ] Save again → Version increments

### ATS Block Integration
- [ ] Create ATS block with `ALICE speaks english so prompt "Summarize {{subject}}"`
- [ ] "Inspect Prompt" button appears below block
- [ ] Click → Prompt editor opens with fields parsed

### Pulse Panel
- [ ] Schedule a prompt job
- [ ] Open job detail panel
- [ ] "Inspect Prompt" button visible next to "Force Trigger"
- [ ] Click → Prompt editor opens

## Files Changed

```
ats/prompt/
├── action.go          # so prompt action parser
├── action_test.go     # action parser tests
├── handler.go         # Pulse job handler
├── store.go           # prompt storage as attestations
├── store_test.go      # storage tests
├── template.go        # template interpolation
└── template_test.go   # template tests

server/
├── prompt_handlers.go # HTTP endpoints
└── routing.go         # route registration

web/ts/
├── prompt-editor-window.ts  # main editor UI
├── so-panel.ts              # prompt history panel
├── symbol-palette.ts        # SO command handling
├── logger.ts                # SEG.SO alias
└── prose/nodes/ats-code-block.ts  # inspect button
└── pulse/job-detail-panel.ts      # inspect button

web/css/
├── prompt-editor.css  # editor styles
├── so-panel.css       # panel styles
├── ats-code-block.css # inspect button styles
└── job-detail-panel.css # button styles

logger/symbol.go       # AddSoSymbol helper
```

## Architecture Notes

### Parsing Approach
- TypeScript uses proper tokenization (not regex) matching Go `ats/parser`
- State machine for parsing keywords: `with`, `model`, `provider`, `predicate`
- Handles single and double quotes

### Storage Design
- Prompts are attestations with bounded storage (16 versions per name)
- Query by name returns latest version
- Version history available via dedicated endpoint

### Integration Pattern
- ix, ax, so are independent parallel histories (not a pipeline)
- SO panel is analogous to hixtory for ix
- Prompt editor accessible from multiple entry points
