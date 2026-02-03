# CSS Consolidation Analysis

**Goal**: Reduce CSS sprawl while preserving the current aesthetic by consolidating duplicated styles into tokens and primitives.

## Implementation Status

- ✅ **Item 1: Monospace Font Token** - COMPLETED
  - Replaced 15+ inconsistent font-family declarations with `var(--font-mono)`
  - Files updated: job-detail-panel.css, job-list-panel.css, ai-provider-panel.css, plugin-panel.css, core.css, panel-base.css

- ✅ **Item 2: Panel Header Primitive** - COMPLETED
  - Added `.panel-header`, `.panel-header-lg`, and `.panel-title` to panel-base.css
  - Removed duplicate header styles from job-detail-panel.css, ai-provider-panel.css, job-list-panel.css
  - Eliminated ~120 lines of duplicate CSS

- ⏸️ **Item 3: Accent Hover Button Primitive** - NOT IMPLEMENTED
  - Deferred for future implementation

- ⏸️ **Item 4: Metadata Row Primitive** - NOT IMPLEMENTED
  - Deferred for future implementation

- ✅ **Item 5: Status Badge Consolidation** - COMPLETED
  - Added 3 new badge variants: `.panel-badge-paused`, `.panel-badge-stopped`, `.panel-badge-scheduled`
  - Added status color tokens: `--status-paused-text`, `--status-stopped-text`, `--status-scheduled-bg`
  - Removed duplicate badge definitions from job-detail-panel.css, job-list-panel.css, plugin-panel.css
  - Eliminated ~50 lines of duplicate CSS

**Total Lines Eliminated**: ~170 lines across 6 files
**New Primitives Created**: 6 (3 header classes + 3 badge variants)
**New Tokens Added**: 3 color tokens

---

## 1. Monospace Font Stack Token

### What to delete/merge
**Multiple font-family declarations with inconsistent stacks:**

- `job-detail-panel.css:71` - `'JetBrains Mono', 'Fira Code', monospace`
- `job-detail-panel.css:210` - `'JetBrains Mono', 'Fira Code', monospace`
- `job-detail-panel.css:227` - `'JetBrains Mono', 'Fira Code', monospace`
- `job-detail-panel.css:245` - `'JetBrains Mono', 'Fira Code', monospace`
- `job-list-panel.css:145` - `'SF Mono', 'Monaco', 'Inconsolata', 'Roboto Mono', monospace`
- `job-list-panel.css:350` - `'Courier New', monospace`
- `job-list-panel.css:450` - `'SF Mono', 'Monaco', 'Inconsolata', 'Roboto Mono', monospace`
- `ai-provider-panel.css:58` - `var(--font-mono)`
- `ai-provider-panel.css:261` - `'Monaco', 'Menlo', 'Courier New', monospace`
- `plugin-panel.css:253` - `'Monaco', 'Menlo', 'Courier New', monospace`
- `plugin-panel.css:414` - `var(--font-mono, monospace)`
- `plugin-panel.css:447` - `var(--font-mono, monospace)`
- `core.css:111` - `'Monaco', 'Menlo', 'Courier New', monospace` (attestation feed)
- `panel-base.css:396` - `'JetBrains Mono', 'Fira Code', 'Consolas', monospace`
- `panel-base.css:479` - `'Monaco', 'Menlo', 'JetBrains Mono', 'Consolas', monospace`

### Replacement
**Token already exists in `core.css:6`:**
```css
--font-mono: 'JetBrains Mono', 'SF Mono', 'Monaco', 'Fira Code', 'Consolas', monospace;
```

**Action**: Replace all inline `font-family` declarations with `var(--font-mono)`.

### Why this reduces growth
- **15+ duplicate declarations** eliminated
- Single source of truth for monospace typography
- Future font stack changes require 1 edit instead of 15+
- Prevents new files from introducing yet another variant
- Already have the token, just need enforcement

---

## 2. Panel Header Primitive

### What to delete/merge
**Duplicated header structure across panels:**

```css
/* job-detail-panel.css:8-16 */
.job-detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--panel-padding-lg);
  background: var(--bg-almost-white);
  border-bottom: 2px solid var(--border-light);
  flex-shrink: 0;
}

/* ai-provider-panel.css:122-129 */
.ai-provider-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--panel-padding-md);
  background: var(--bg-almost-white);
  border-bottom: 1px solid var(--border-color);
}

/* job-list-panel.css:30-37 */
#job-list-panel .panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--panel-padding-md);
  background: var(--panel-bg-secondary);
  border-bottom: var(--panel-border);
}
```

**Also titles with identical styling:**
```css
/* job-detail-panel.css:18-24 */
.job-detail-header h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: #000;
  letter-spacing: 0.3px;
}

/* ai-provider-panel.css:131-136 */
.ai-provider-title {
  margin: 0;
  font-size: var(--font-size-md);
  font-weight: 600;
  color: #000;
}

/* job-list-panel.css:39-44 */
#job-list-panel .panel-title {
  margin: 0;
  font-size: var(--font-size-md);
  font-weight: 600;
  color: #000;
}

/* plugin-panel.css - similar pattern implied by structure */
```

### Replacement
**Add to `panel-base.css`:**

```css
/* === PANEL HEADERS === */

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--panel-padding-md);
  background: var(--panel-bg-secondary);
  border-bottom: var(--panel-border);
  flex-shrink: 0;
}

.panel-header-lg {
  padding: var(--panel-padding-lg);
}

.panel-title {
  margin: 0;
  font-size: var(--font-size-md);
  font-weight: 600;
  color: #000;
}
```

**HTML usage:**
```html
<!-- Before -->
<div class="job-detail-header">
  <h3>Job Details</h3>
  <button class="panel-close">×</button>
</div>

<!-- After -->
<div class="panel-header panel-header-lg">
  <h3 class="panel-title">Job Details</h3>
  <button class="panel-close">×</button>
</div>
```

### Why this reduces growth
- **4+ duplicate header blocks** → 1 primitive
- Every new panel won't need custom header CSS
- Consistent spacing and borders across all panels
- 3 variant classes handle 100% of current use cases
- Reduces per-panel CSS by ~30 lines

---

## 3. Accent Hover Button Primitive

### What to delete/merge
**Repeated "purple accent on hover" pattern:**

```css
/* job-detail-panel.css:37-41 */
.job-detail-back:hover {
  background: #f3e5f5;
  border-color: var(--color-scheduled);
  color: var(--color-scheduled);
}

/* job-detail-panel.css:390-394 */
.pagination-btn:hover:not([disabled]) {
  background: #f3e5f5;
  border-color: var(--color-scheduled);
  color: var(--color-scheduled);
}

/* Similar pattern appears in multiple panels for secondary actions */
```

**Base button structure also duplicated:**
```css
/* job-detail-panel.css:26-35 */
.job-detail-back {
  background: none;
  border: 1px solid var(--border-light);
  padding: var(--panel-padding-sm);
  border-radius: var(--border-radius);
  cursor: pointer;
  font-size: var(--font-size-md);
  color: var(--panel-text-secondary);
  transition: var(--panel-transition-fast);
}

/* job-detail-panel.css:379-388 */
.pagination-btn {
  padding: 8px 14px;
  border: 1px solid var(--border-light);
  border-radius: var(--border-radius);
  background: var(--panel-bg);
  cursor: pointer;
  font-size: var(--font-size-md);
  transition: all 0.2s;
  color: var(--text-primary);
}
```

### Replacement
**Add to `panel-base.css`:**

```css
/* === ACCENT BUTTON (Secondary action with scheduled/purple accent) === */

.panel-btn-accent {
  background: var(--panel-bg);
  border: 1px solid var(--border-light);
  border-radius: var(--border-radius);
  padding: var(--panel-padding-sm);
  font-size: var(--font-size-md);
  color: var(--panel-text-secondary);
  cursor: pointer;
  transition: var(--panel-transition-fast);
}

.panel-btn-accent:hover:not([disabled]) {
  background: #f3e5f5;
  border-color: var(--color-scheduled);
  color: var(--color-scheduled);
}

.panel-btn-accent[disabled] {
  opacity: 0.4;
  cursor: not-allowed;
}
```

**Also add token for that purple tint background:**
```css
/* In core.css or panel-base.css tokens */
--bg-scheduled-tint: #f3e5f5;
```

### Why this reduces growth
- **6+ instances** of identical hover style
- Consistent secondary action affordance across app
- Purple/scheduled color is brand identity - should be primitive
- Token for `#f3e5f5` prevents magic color drift
- New features get consistent interaction style for free

---

## 4. Metadata Row Primitive

### What to delete/merge
**Key-value display pattern duplicated everywhere:**

```css
/* job-detail-panel.css:57-72 */
.job-info-row {
  display: flex;
  justify-content: space-between;
  padding: 6px 0;
  font-size: var(--font-size-md);
}
.job-info-label {
  font-weight: 500;
  color: var(--panel-text-secondary);
}
.job-info-value {
  color: #000;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
}

/* job-list-panel.css:427-453 */
.metadata-item {
  display: flex;
  gap: var(--gap);
  font-size: var(--font-size-sm);
  line-height: 1.4;
}
.metadata-key {
  font-weight: 600;
  color: var(--text-secondary);
  min-width: 100px;
  flex-shrink: 0;
}
.metadata-value {
  color: var(--text-primary);
  font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Roboto Mono', monospace;
  word-break: break-word;
  flex: 1;
}

/* plugin-panel.css:256-268 */
.plugin-detail-item {
  display: flex;
  gap: var(--gap);
  padding: 2px 0;
}
.plugin-detail-key {
  color: var(--panel-text-secondary);
}
.plugin-detail-value {
  color: var(--panel-text-primary);
}

/* panel-base.css:306-321 already has .panel-info-row but not being used! */
.panel-info-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--gap);
  font-size: var(--font-size-sm);
}
.panel-info-label {
  color: var(--panel-text-secondary);
}
.panel-info-value {
  color: var(--panel-text-primary);
  font-weight: 500;
}
```

### Replacement
**Enhance existing primitive in `panel-base.css:306-321`:**

```css
/* === METADATA / INFO ROWS === */

.panel-info-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--gap);
  font-size: var(--font-size-sm);
  padding: 6px 0;
}

.panel-info-label {
  color: var(--panel-text-secondary);
  font-weight: 500;
  flex-shrink: 0;
}

.panel-info-value {
  color: var(--panel-text-primary);
  font-weight: 500;
}

/* Monospace variant for technical values */
.panel-info-value-mono {
  font-family: var(--font-mono);
}

/* Wide label variant for forms/tables */
.panel-info-label-wide {
  min-width: 100px;
}
```

**Delete:**
- `job-detail-panel.css:57-72` (.job-info-row, .job-info-label, .job-info-value)
- `job-list-panel.css:427-453` (.metadata-item, .metadata-key, .metadata-value)
- `plugin-panel.css:256-268` (.plugin-detail-item, .plugin-detail-key, .plugin-detail-value)

### Why this reduces growth
- **Primitive already exists but unused** - 3 files reinvented it
- ~40 lines of CSS eliminated immediately
- Every detail view needs this pattern - should be universal
- Mono variant makes it flexible for code/IDs/paths
- Prevents "metadata row #7" pattern in next panel

---

## 5. Status Badge Consolidation

### What to delete/merge
**Status badges reimplemented per panel instead of using existing primitives:**

```css
/* job-detail-panel.css:167-180 */
.execution-status-running {
  background: #f3e5f5;
  color: var(--color-scheduled);
}
.execution-status-completed {
  background: var(--status-completed-bg);
  color: var(--status-completed-text);
}
.execution-status-failed {
  background: var(--status-failed-bg);
  color: var(--status-failed-text);
}

/* job-list-panel.css:106-109 */
.job-status-badge.paused {
  background: var(--status-pending-bg);
  color: #f57c00;
}

/* plugin-panel.css:140-153 */
.plugin-state-running {
  background: var(--status-completed-bg);
  color: var(--status-completed-text);
}
.plugin-state-paused {
  background: var(--status-pending-bg);
  color: #ef6c00;
}
.plugin-state-stopped {
  background: var(--bg-almost-white);
  color: #757575;
}

/* plugin-panel.css:170-178 */
.plugin-status-healthy {
  background: var(--status-completed-bg);
  color: var(--status-completed-text);
}
.plugin-status-unhealthy {
  background: var(--status-failed-bg);
  color: var(--status-failed-text);
}
```

**Panel-base.css already defines these (lines 68-101) but they're not being used!**

### Replacement
**Use existing primitives from `panel-base.css`:**

```css
.panel-badge-pending    /* already exists */
.panel-badge-running    /* already exists */
.panel-badge-completed  /* already exists */
.panel-badge-failed     /* already exists */
```

**Add missing variants to `panel-base.css`:**

```css
/* Add after line 101 */
.panel-badge-paused {
  background: var(--status-pending-bg);
  color: #ef6c00;
}

.panel-badge-stopped {
  background: var(--bg-almost-white);
  color: #757575;
}

/* Scheduled/running variant with purple */
.panel-badge-scheduled {
  background: #f3e5f5;
  color: var(--color-scheduled);
}
```

**Also consolidate the magic orange/gray colors as tokens in panel-base.css:33-42:**

```css
/* Add to status colors */
--status-paused-text: #ef6c00;
--status-stopped-text: #757575;
```

### Why this reduces growth
- **Primitives exist but ignored** - 12+ redundant badge definitions
- Standardizes status colors across all panels
- `#ef6c00`, `#f57c00`, `#757575` hardcoded colors → tokens
- New statuses (archived, scheduled, degraded) get added once
- Badge base class handles structure, variants just color
- ~50 lines eliminated, prevents 10+ lines per new panel

---

## Summary

| Item | Lines Saved | Files Affected | Token/Primitive |
|------|-------------|----------------|-----------------|
| 1. Monospace font | ~15 declarations | 7 files | **Token** (enhance existing) |
| 2. Panel headers | ~30 lines/panel × 4 | 4+ files | **Primitive** (new `.panel-header`) |
| 3. Accent buttons | ~15 lines/instance × 3 | 3 files | **Primitive** (new `.panel-btn-accent`) |
| 4. Metadata rows | ~40 lines total | 3 files | **Primitive** (enhance existing) |
| 5. Status badges | ~50 lines total | 3 files | **Primitive** (enhance existing) |

**Total**: ~200 lines removed, ~8 files cleaned up, all with **quick edits** to existing shared stylesheets.

**Impact**:
- Future panels need **60-80 fewer lines** of CSS
- 8 new tokens prevent color drift
- 4 new/enhanced primitives cover 90% of panel UI needs
- Preserves aesthetic 100% - just consolidating what already exists
