# Config Panel UI Design Specification

> **Architecture Reference**: For backend config system architecture, see [`docs/architecture/config-system.md`](../architecture/config-system.md)

## Purpose

This document specifies the UI/UX design for the QNTX configuration panel. It defines the visual layout, interaction model, and future product vision for how users interact with configuration in the web UI.

## Current Issues

1. **Size Constraint**: Panel is cramped at max-height: 500px
2. **Layout Problem**: Overlays content, competes for space with ATS editor
3. **Poor Readability**: Long config values (API keys, tokens) are truncated
4. **No Visual Hierarchy**: Flat list doesn't show config precedence clearly
5. **Mixed Concerns**: Shows everything together, hard to distinguish editable vs read-only

## Design Goals

### Layout
- **Full-screen left half**: Config panel takes 50% of screen width when shown
- **Hide ATS editor**: When config panel is open, hide the left panel entirely
- **Dedicated space**: No competition for vertical space, full scrollable area

### Information Architecture

**Group by source**: Collapsible sections for each config source (see [config-system.md](../architecture/config-system.md) for precedence details):
- System Config (`/etc/qntx/config.toml`)
- User Config (`~/.qntx/config.toml`)
- UI Config (`~/.qntx/config_from_ui.toml`) - **Editable**
- Project Config (`project/config.toml`)
- Environment Variables (`QNTX_*`)

**Visual precedence**: Make it clear which value is effective
- Highlight active value
- Show overridden values in gray/dimmed
- Indicate "This setting is overridden by [source]"

### Interaction Model
- **Read-only by default**: Most config shown as information
- **Editable UI config**: Only UI config section allows editing
- **Inline editing**: Click to edit UI config values
- **Save feedback**: Clear indication when changes are saved to `~/.qntx/config_from_ui.toml`

### Visual Design
- **Better typography**: Use code font for config values
- **Truncation with expand**: Long values truncated with "..." and click to expand
- **Search/filter**: Retain existing search, but with better visual feedback
- **Source badges**: Color-coded badges showing source (system/user/user_ui/project/env)

## Technical Implementation

### Files to Modify

1. **`web/ts/config-panel.js`**
   - Change from overlay to full-screen toggle
   - Add source grouping logic
   - Implement inline editing for UI config
   - Add expand/collapse for sections

2. **`web/css/config-panel.css`**
   - New layout: `position: fixed; left: 0; width: 50%; height: 100%;`
   - Styles for source groups
   - Visual hierarchy for precedence
   - Source badge styles

3. **`web/index.html`** (if needed)
   - May need template changes for new structure

### Data Flow

```javascript
// Current:
GET /api/config?introspection=true
→ Flat list of settings with sources

// Enhanced (future):
GET /api/config?introspection=true
→ Grouped by source
→ Each setting knows: key, value, source, is_effective, overridden_by
```

### Backend Support

Backend implementation is documented in [`docs/architecture/config-system.md`](../architecture/config-system.md).

Current backend capabilities:
- ✅ Config precedence includes `config_from_ui.toml`
- ✅ Introspection tracks granular sources (system/user/user_ui/project/env)
- ✅ Clean TOML marshaling for updates
- ✅ POST /api/config with updates writes to UI config only

## UI Mockup (Text-Based)

```
┌─────────────────────────────────────┐
│ ≡ Configuration            [✕ Close]│
├─────────────────────────────────────┤
│ 🔍 [Filter settings...]             │
├─────────────────────────────────────┤
│                                     │
│ ▼ System Config                     │
│   (Read-only, /etc/qntx/config.toml)│
│   ┌─────────────────────────────┐   │
│   │ database.path               │   │
│   │ /var/lib/qntx/qntx.db       │   │
│   │ [system]                    │   │
│   └─────────────────────────────┘   │
│                                     │
│ ▼ User Config                       │
│   (Read-only, ~/.qntx/config.toml)  │
│   ┌─────────────────────────────┐   │
│   │ openrouter.api_key          │   │
│   │ sk-or-v1-9bee...            │   │
│   │ [user] ⚠ Overridden by env  │   │
│   └─────────────────────────────┘   │
│                                     │
│ ▼ UI Config (Editable)              │
│   (Managed by UI, ~/.qntx/config... │
│   ┌─────────────────────────────┐   │
│   │ local_inference.enabled     │   │
│   │ [✓] true            [Edit]  │   │
│   │ [user_ui] ✅ Active         │   │
│   └─────────────────────────────┘   │
│   ┌─────────────────────────────┐   │
│   │ local_inference.model       │   │
│   │ mistral          [▼ Edit]   │   │
│   │ [user_ui] ✅ Active         │   │
│   └─────────────────────────────┘   │
│                                     │
│ ▼ Project Config                    │
│   (Read-only, project/config.toml)  │
│   (No settings - project config     │
│    would override UI settings)      │
│                                     │
│ ▼ Environment Variables             │
│   ┌─────────────────────────────┐   │
│   │ openrouter.api_key          │   │
│   │ sk-or-v1-bb5b...            │   │
│   │ [environment] ✅ Active     │   │
│   └─────────────────────────────┘   │
│                                     │
└─────────────────────────────────────┘
```

## Behavior Specifications

### Opening Config Panel
1. Click ≡ in symbol palette
2. Left panel (#left-panel) fades out and hides
3. Config panel slides in from left, taking 50% width
4. ATS editor completely hidden (no competing for space)

### Closing Config Panel
1. Click ✕ or click outside panel
2. Config panel slides out to left
3. Left panel fades back in
4. ATS editor visible again

### Editing UI Config Values
1. Only UI Config section allows editing
2. Click [Edit] button or value to enter edit mode
3. For booleans: toggle switch
4. For strings: inline text input
5. For enums (like model): dropdown
6. Changes auto-save to `~/.qntx/config_from_ui.toml`
7. Show toast: "Saved to UI config"

### Source Precedence Display
- **Effective value**: Green checkmark, normal weight
- **Overridden value**: Gray text, strike-through
- **Override indicator**: "⚠ Overridden by [source]" in orange

## Implementation Phases

### Phase 1: Layout Restructure ✅ (Completed)
- Change panel to full-screen left half
- Implement show/hide toggle with left panel
- Basic scrolling and close button

### Phase 2: Source Grouping ✅ (Completed)
- Group settings by source
- Collapsible sections
- Source badges

### Phase 3: Precedence Visualization (In Progress)
- Highlight active values
- Show overridden state
- Precedence indicators
- **Requires**: Backend to provide `is_effective` and `overridden_by` fields

### Phase 4: Inline Editing (Planned)
- Edit UI config values inline
- Type-appropriate inputs (toggle, text, dropdown)
- Auto-save with feedback
- **Requires**: Edit modal or inline form implementation

### Phase 5: Polish ✅ (Completed)
- Smooth animations
- Better typography
- Search improvements
- Responsive design

## Future Product Vision

### Multi-Provider Config Sources

As QNTX scales, config will come from diverse providers:
- **HashiCorp Vault** - Secrets management (API keys, tokens)
- **Consul** - Dynamic service configuration
- **AWS Secrets Manager** - Cloud-native secrets
- **etcd** - Distributed configuration
- **Environment** - Container orchestration (k8s ConfigMaps)
- **Project files** - Repository-specific overrides
- **UI toggles** - User preferences

**Key Challenge**: **Visibility into composition**
- Which provider supplied each config fragment?
- What's the effective precedence chain?
- How do I debug config resolution?

### Design Principle: Computer Does the Merging

The config panel's job is to **visualize the dataflow**:

1. **Show final merged state** (what server sees)
2. **Attribute each value to its provider** (source visibility)
3. **Indicate override relationships** (precedence chain)
4. **Support provider-specific metadata** (TTL, refresh, health)

**Example with multiple providers:**
```
openrouter.api_key = sk-***           [VAULT:prod/ai] ✓ (refreshed 2m ago)
  ⚠ overrides [ENV], [USER]

database.path = /var/lib/qntx/...     [CONSUL:service/qntx] ✓ (synced)
  ⚠ overrides [SYSTEM]

pulse.daily_budget_usd = 10.0         [USER_UI] ✓
  ⚠ overridden by [PROJECT]
```

### Provider Extensibility (Future)

- **Provider registry** - Pluggable config providers with standardized interface
- **Provider health indicators** - Show sync status, last refresh, errors
- **Provider metadata** - TTL, refresh intervals, version info
- **Provider-specific actions** - "Refresh from Vault", "Force sync from Consul"

### Config Dataflow Visualization (Future)

- **Precedence chain view** - Show all sources for a key, with precedence order
- **Config diff view** - Compare effective config against specific source
- **Config timeline** - Show when values changed and which provider updated them
- **Config dependencies** - Visualize which settings depend on each other

### Enhanced UX Features (Future)

- **Inline editing**: Edit UI config values with type-appropriate inputs
- **Value expansion**: Click to expand long API keys/secrets
- **Config validation**: Show warnings for invalid values
- **Reset to defaults**: Button to clear UI config overrides
- **Export config**: Download merged config with or without secrets
- **Documentation drawer**: Click config key to open documentation panel on right side

### Documentation Drawer Vision (Future)

**Layout**: Config panel (left 50%) + Documentation drawer (right 50%)

**Interaction Flow**:
1. User clicks config key (e.g., `openrouter.model`)
2. Right side slides in with documentation for that specific config option
3. Documentation drawer shows:
   - **Description**: What this config controls
   - **Type**: String, number, boolean, enum
   - **Valid values**: Enumeration or constraints
   - **Default value**: What happens if unset
   - **Examples**: Common use cases with sample values
   - **Related settings**: Other config that interacts with this one
   - **Provider context**: If from Vault/Consul, show provider-specific metadata

**Example Documentation Entry**:
```
┌─────────────────────────────────────────────────────────────┐
│ openrouter.model                                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│ Description:                                                │
│   Specifies which AI model to use for OpenRouter           │
│   inference calls. Affects cost, speed, and quality.       │
│                                                             │
│ Type: string                                                │
│                                                             │
│ Valid Values:                                               │
│   • anthropic/claude-haiku-4    (fast, cheap)              │
│   • anthropic/claude-3.5-sonnet:beta (balanced)            │
│   • x-ai/grok-code-fast-1       (code-focused)             │
│   • meta-llama/llama-3.2-3b-instruct (very fast)           │
│                                                             │
│ Default: anthropic/claude-haiku-4.5                         │
│                                                             │
│ Examples:                                                   │
│   For code generation:  x-ai/grok-code-fast-1              │
│   For reasoning tasks:  anthropic/claude-3.5-sonnet:beta   │
│   For bulk operations:  meta-llama/llama-3.2-3b-instruct   │
│                                                             │
│ Related Settings:                                           │
│   • openrouter.api_key - Required for OpenRouter access    │
│   • local_inference.enabled - Alternative to cloud         │
│                                                             │
│ Cost Impact: Varies by model (see OpenRouter pricing)      │
│                                                             │
│ [View OpenRouter Models →]                                  │
└─────────────────────────────────────────────────────────────┘
```

**Implementation Considerations**:
- Documentation stored in structured format (TOML/YAML/Markdown with frontmatter)
- Backend serves documentation via `/api/config/docs/:key` endpoint
- Frontend caches docs for instant display
- Fallback to generated docs from schema if manual docs missing
- Support for markdown rendering in description field
- Link out to external docs (OpenRouter pricing, Vault setup, etc.)

**Symbol Integration**:
- **▣** - Documentation symbol
- Clicking config key directly opens its documentation in right panel
- ▣ in symbol palette opens documentation browser (all available docs)
- Visual indicator (subtle) shows which settings have documentation available

## Related Documentation

- **Backend Architecture**: [`docs/architecture/config-system.md`](../architecture/config-system.md) - Config loading, persistence, source tracking
- **User Guide**: How to configure QNTX (TBD)
- **API**: `internal/config/config.go` (implementation)
