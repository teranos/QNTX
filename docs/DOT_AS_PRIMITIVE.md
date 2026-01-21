# Dot-as-Primitive: Implementation Plan

## Vision

**The Fundamental Shift:**
- **Old model**: Window exists → minimize → creates dot in tray
- **New model**: Dot exists in tray → expand → dot becomes the window

**The Dot IS the Window:**
The dot is not an indicator or launcher. The dot entity IS the window. It has three visual states:

1. **Collapsed (8px)**: Idle state in tray zone (bottom right)
2. **Proximity morph (8px → 220px)**: Mouse approach triggers expansion with text (existing WindowTray behavior)
3. **Expanded (full window)**: Click causes dot to break OUT of tray zone, become full-sized window content positioned anywhere on screen

**The Lifecycle:**
- Dot persists in tray (always exists)
- When expanded: dot entity transforms into positioned window with full content
- When minimized: window collapses back into tray zone as 8px dot
- The dot maintains identity throughout - it's the same entity

**What Gets Deprecated:**
- Window component (window.ts) will eventually be replaced
- Windows as separate entities that can minimize
- The concept of "creating a window that can minimize to a dot"

**What Stays:**
- WindowTray infrastructure (tray zone, proximity morphing, all tuning)
- The carefully tuned thresholds, easing curves, baseline boost
- Tray positioning (bottom right)

**Scope:**
- Initially: windows only (VidStream, DatabaseStats, Self)
- Later: panels
- BasePanel stays as-is for now

**Key Point:**
When expanded, the window is NOT stuck in tray zone - it's a normal positioned window anywhere on screen. It just minimizes back to the tray zone.

---

## Current State Analysis

### WindowTray (window-tray.ts)

**What it does now:**
- Manages dots for minimized windows
- Proximity morphing (8px → 220px with text)
- Dot click calls `onRestore(dotRect)` callback
- Dots are added via `windowTray.add(item)` when windows minimize

**TrayItem interface:**
```typescript
export interface TrayItem {
    id: string;
    title: string;
    onRestore: (sourceRect?: DOMRect) => void;
    onClose?: () => void;
}
```

**Current flow:**
1. VidStreamWindow creates itself
2. User clicks minimize
3. Window calls `windowTray.add()` with restore callback
4. Dot appears in tray
5. User clicks dot
6. `onRestore()` is called
7. Window shows itself again

### Window Component (window.ts)

**What it does:**
- Creates draggable window with title bar, close/minimize buttons
- Handles positioning, resizing, dragging
- State management (visible, hidden, minimized)
- Storage via uiState

**Current usage:**
- VidStreamWindow extends or uses Window
- DatabaseStatsWindow extends or uses Window
- Self window uses Window

---

## Implementation Approach

### Phase 1: Extend TrayItem to be Self-Sufficient

**Goal:** Make TrayItem capable of rendering its own expanded content without depending on Window component.

**Changes to TrayItem interface:**
```typescript
export interface TrayItem {
    id: string;
    title: string;
    symbol?: string;  // Symbol to show in collapsed/morphed state

    // Content rendering
    renderContent: () => HTMLElement;  // Returns the content to display when expanded

    // Window configuration
    initialWidth?: string;
    initialHeight?: string;
    defaultX?: number;
    defaultY?: number;

    // Callbacks
    onExpand?: () => void;
    onCollapse?: () => void;
    onClose?: () => void;

    // DEPRECATED (for backward compatibility during migration)
    onRestore?: (sourceRect?: DOMRect) => void;
}
```

**WindowTray changes:**
1. Track expanded state: `Map<string, boolean>` to know which dots are expanded
2. Create expanded window element when dot is clicked
3. Render `item.renderContent()` inside expanded window
4. Handle positioning (break out of tray zone)
5. Handle minimize button (collapse back to dot)
6. Handle close button (remove dot or just collapse, configurable)

### Phase 2: Register Dots on Startup

**Goal:** Dots exist from the start, not created by windows minimizing.

**New registration pattern:**
```typescript
// In main.ts or a new dot-registry.ts
windowTray.register({
    id: 'vidstream-window',
    title: 'VidStream',
    symbol: '⮀',
    renderContent: () => {
        // Create VidStream content
        return vidstreamContent;
    },
    onExpand: () => {
        // Initialize VidStream when first expanded
    }
});
```

**State restoration:**
```typescript
// On init, restore expanded state from uiState
const expandedDots = uiState.getExpandedWindows();
expandedDots.forEach(id => {
    windowTray.expand(id);
});
```

### Phase 3: Migrate Windows to Dots

**VidStreamWindow migration:**
1. Extract content rendering logic
2. Create TrayItem with `renderContent` function
3. Register with windowTray on startup
4. Remove old Window-based implementation

**DatabaseStatsWindow migration:**
1. Same pattern as VidStream

**Self Window migration:**
1. Same pattern

### Phase 4: Deprecate Window Component

Once all windows are migrated to dots:
1. Mark Window component as deprecated
2. Eventually remove window.ts
3. Clean up window-related CSS

---

## Technical Decisions

### Decision 1: Temporary Window Component Usage

**Question:** Should dots create Window instances temporarily during migration?

**Options:**
A) Build expansion rendering directly in WindowTray (clean but big change)
B) Have dots create Window instances when expanded (incremental migration)

**Decision:**
Option A - Build expansion rendering directly in WindowTray.

**Rationale:**
- Clean break from old Window component
- WindowTray owns the entire dot lifecycle
- No temporary dependencies to remove later
- Simpler mental model: dot expands in place

### Decision 2: Dot Registration Location

**Question:** Where should dots be registered?

**Options:**
A) In main.ts directly
B) In a new dot-registry.ts file
C) In each feature's own file (vidstream-window.ts)

**Recommendation:**
Option B - new dot-registry.ts

**Rationale:**
- Central location for all dots
- Easy to see what's available
- Clean separation from initialization logic
- Similar to how symbol-palette.ts works now

### Decision 3: State Management

**Question:** How to track expanded/collapsed state?

**Options:**
A) Extend uiState with `expandedWindows: string[]`
B) Keep in WindowTray only
C) Use existing WindowState but repurpose `minimized` field

**Recommendation:**
Option C - use existing WindowState with inverted semantics

**Rationale:**
- Already have persistence infrastructure
- `minimized: false` = expanded, `minimized: true` = collapsed to dot
- No new storage version bump needed
- Smoother migration path

### Decision 4: Close vs Minimize Semantics

**Question:** What should close button do on expanded dot?

**Options:**
A) Always collapse to dot (dot persists)
B) Configurable per dot (some collapse, some remove)
C) Always remove dot entirely

**Recommendation:**
Option A initially, then Option B

**Rationale:**
- Dots are permanent by default (like macOS dock)
- Some features may want true removal (configurable)
- Can add `removableOnClose: boolean` to TrayItem later

---

## Migration Steps

### Step 1: Extend TrayItem and WindowTray
- [ ] Add new fields to TrayItem interface
- [ ] Add `expandedDots` tracking to WindowTray
- [ ] Add `expand(id)` and `collapse(id)` methods
- [ ] Implement dot expansion rendering
- [ ] Keep backward compatibility with `onRestore`

### Step 2: Create Dot Registry
- [ ] Create `web/ts/dot-registry.ts`
- [ ] Register VidStream as first dot
- [ ] Initialize in main.ts
- [ ] Test: dot appears on startup, expands on click

### Step 3: Migrate VidStream
- [ ] Extract VidStream content rendering
- [ ] Convert to TrayItem with `renderContent`
- [ ] Remove old Window-based initialization
- [ ] Test: full VidStream functionality via dot

### Step 4: Migrate Remaining Windows
- [ ] DatabaseStatsWindow → dot
- [ ] Self window → dot
- [ ] Any other windows

### Step 5: Clean Up
- [ ] Remove `onRestore` from TrayItem (deprecated)
- [ ] Remove Window component references
- [ ] Update documentation
- [ ] Remove window.ts (eventually)

---

## Open Questions

1. **Dragging:** Should expanded dots be draggable? Or fixed position?
   - Likely yes, need to add drag handling to WindowTray

2. **Resizing:** Should expanded dots be resizable?
   - Likely yes, need to add resize handling

3. **Z-index:** How to handle multiple expanded dots overlapping?
   - Need stacking order management

4. **Animation:** Should expansion from dot be animated?
   - Probably yes, morph from dot position to final window position

5. **Mobile:** How does vertical navigation work?
   - Touch events, scrolling through dots
   - Haptic feedback integration
   - Needs separate design/implementation

---

## Success Criteria

**Phase 1 Complete:**
- [ ] Dot expands to positioned content on click
- [ ] Dot collapses back on minimize
- [ ] Proximity morphing still works
- [ ] State persists across reload

**Phase 2 Complete:**
- [ ] Dots appear on startup (not just after minimizing)
- [ ] VidStream works entirely via dot

**Phase 3 Complete:**
- [ ] All windows migrated to dots
- [ ] No Window component usage in features

**Phase 4 Complete:**
- [ ] Window component deleted
- [ ] All window CSS removed
- [ ] Documentation updated

---

## Risk Mitigation

**Risk:** Breaking existing window functionality
**Mitigation:** Keep Window component working during migration, migrate incrementally

**Risk:** Loss of carefully tuned proximity morphing
**Mitigation:** Don't touch existing proximity code, only add expansion logic

**Risk:** State management conflicts
**Mitigation:** Use existing WindowState infrastructure, test thoroughly

**Risk:** User confusion during transition
**Mitigation:** Both systems work during migration, clear visual feedback

---

## Implementation Details

### WindowTray Expansion Rendering

**New methods to add:**

```typescript
class WindowTrayImpl {
    private expandedDots: Map<string, HTMLElement> = new Map(); // Track expanded windows

    /**
     * Expand a dot into full window
     */
    public expand(id: string): void {
        const item = this.items.get(id);
        if (!item || this.expandedDots.has(id)) return;

        // Create window container
        const windowEl = document.createElement('div');
        windowEl.className = 'tray-window';
        windowEl.setAttribute('data-window-id', id);

        // Create title bar
        const titleBar = this.createTitleBar(item);
        windowEl.appendChild(titleBar);

        // Create content area
        const contentArea = document.createElement('div');
        contentArea.className = 'tray-window-content';
        contentArea.appendChild(item.renderContent());
        windowEl.appendChild(contentArea);

        // Position window (restore from state or use defaults)
        const state = uiState.getWindowState(id);
        if (state) {
            windowEl.style.left = `${state.x}px`;
            windowEl.style.top = `${state.y}px`;
            windowEl.style.width = state.width;
        } else {
            // Default positioning
            windowEl.style.left = `${item.defaultX || 100}px`;
            windowEl.style.top = `${item.defaultY || 100}px`;
            windowEl.style.width = item.initialWidth || '600px';
        }

        // Add to DOM
        document.body.appendChild(windowEl);

        // Track expanded state
        this.expandedDots.set(id, windowEl);
        uiState.updateWindowState(id, { minimized: false });

        // Make draggable
        this.makeDraggable(windowEl);

        // Callback
        item.onExpand?.();
    }

    /**
     * Collapse expanded window back to dot
     */
    public collapse(id: string): void {
        const windowEl = this.expandedDots.get(id);
        if (!windowEl) return;

        const item = this.items.get(id);

        // Animate collapse back to tray (optional)
        // windowEl.style.transition = 'all 0.3s ease';
        // const trayPos = this.getTargetPosition();
        // windowEl.style.transform = `translate(${trayPos.x}px, ${trayPos.y}px) scale(0)`;

        // Remove from DOM
        windowEl.remove();
        this.expandedDots.delete(id);

        // Update state
        uiState.updateWindowState(id, { minimized: true });

        // Callback
        item?.onCollapse?.();
    }

    /**
     * Create title bar with minimize/close buttons
     */
    private createTitleBar(item: TrayItem): HTMLElement {
        const titleBar = document.createElement('div');
        titleBar.className = 'tray-window-title-bar';

        const title = document.createElement('span');
        title.className = 'tray-window-title';
        title.textContent = item.title;
        titleBar.appendChild(title);

        const buttons = document.createElement('div');
        buttons.className = 'tray-window-buttons';

        // Minimize button
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '−';
        minimizeBtn.addEventListener('click', () => this.collapse(item.id));
        buttons.appendChild(minimizeBtn);

        // Close button
        const closeBtn = document.createElement('button');
        closeBtn.textContent = '×';
        closeBtn.addEventListener('click', () => {
            if (item.onClose) {
                item.onClose();
            } else {
                this.collapse(item.id); // Default: just minimize
            }
        });
        buttons.appendChild(closeBtn);

        titleBar.appendChild(buttons);

        return titleBar;
    }

    /**
     * Make window draggable
     */
    private makeDraggable(windowEl: HTMLElement): void {
        const titleBar = windowEl.querySelector('.tray-window-title-bar') as HTMLElement;
        if (!titleBar) return;

        let isDragging = false;
        let startX = 0;
        let startY = 0;
        let offsetX = 0;
        let offsetY = 0;

        titleBar.addEventListener('mousedown', (e) => {
            isDragging = true;
            startX = e.clientX;
            startY = e.clientY;
            const rect = windowEl.getBoundingClientRect();
            offsetX = startX - rect.left;
            offsetY = startY - rect.top;

            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);
        });

        const onMouseMove = (e: MouseEvent) => {
            if (!isDragging) return;

            const x = e.clientX - offsetX;
            const y = e.clientY - offsetY;

            windowEl.style.left = `${x}px`;
            windowEl.style.top = `${y}px`;
        };

        const onMouseUp = () => {
            if (!isDragging) return;
            isDragging = false;

            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);

            // Save position
            const id = windowEl.getAttribute('data-window-id');
            if (id) {
                const rect = windowEl.getBoundingClientRect();
                uiState.updateWindowState(id, {
                    x: rect.left,
                    y: rect.top
                });
            }
        };
    }

    /**
     * Register a dot (new method for dot-as-primitive)
     */
    public register(item: TrayItem): void {
        this.items.set(item.id, item);
        this.renderItems();
        this.element?.setAttribute('data-empty', 'false');
    }
}
```

### CSS for Expanded Windows

```css
/* Expanded tray window */
.tray-window {
    position: fixed;
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
    z-index: 1000;
    min-width: 400px;
    min-height: 300px;
    display: flex;
    flex-direction: column;
}

.tray-window-title-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-color);
    cursor: move;
    user-select: none;
}

.tray-window-title {
    font-weight: 500;
    font-size: 14px;
}

.tray-window-buttons {
    display: flex;
    gap: 8px;
}

.tray-window-buttons button {
    width: 24px;
    height: 24px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    border-radius: 4px;
    font-size: 16px;
    line-height: 1;
}

.tray-window-buttons button:hover {
    background: var(--bg-hover);
}

.tray-window-content {
    flex: 1;
    overflow: auto;
    padding: 12px;
}
```

### Dot Registry Pattern

```typescript
// web/ts/dot-registry.ts

import { windowTray } from './components/window-tray';
import { VidStreamContent } from './vidstream-content'; // Extract from VidStreamWindow

export function initializeDots(): void {
    // VidStream dot
    windowTray.register({
        id: 'vidstream-window',
        title: 'VidStream - Real-time Video Inference',
        symbol: '⮀',
        renderContent: () => {
            const content = new VidStreamContent();
            return content.getElement();
        },
        initialWidth: '700px',
        defaultX: 100,
        defaultY: 100,
        onExpand: () => {
            console.log('VidStream expanded');
        },
        onCollapse: () => {
            console.log('VidStream collapsed');
        }
    });

    // Database stats dot
    windowTray.register({
        id: 'db-stats-window',
        title: 'Database Statistics',
        symbol: '⊔',
        renderContent: () => {
            // Create DB stats content
            const div = document.createElement('div');
            div.textContent = 'Database stats here';
            return div;
        },
        initialWidth: '600px',
        defaultX: 150,
        defaultY: 150
    });

    // Self window dot
    windowTray.register({
        id: 'self-window',
        title: 'System Diagnostic',
        symbol: '⍟',
        renderContent: () => {
            // Create self diagnostic content
            const div = document.createElement('div');
            div.textContent = 'System diagnostic here';
            return div;
        },
        initialWidth: '500px',
        defaultX: 200,
        defaultY: 200
    });
}
```

### Integration in main.ts

```typescript
import { initializeDots } from './dot-registry';

async function init(): Promise<void> {
    // ... existing init code ...

    // Initialize window tray
    windowTray.init();

    // Register dots
    initializeDots();

    // Restore expanded state from previous session
    const expandedDots = uiState.getExpandedWindows(); // New method
    expandedDots.forEach(id => {
        windowTray.expand(id);
    });

    // ... rest of init ...
}
```

### State Management Extension

```typescript
// In web/ts/state/ui.ts

export class UIState {
    // ... existing code ...

    /**
     * Get list of expanded window IDs
     */
    public getExpandedWindows(): string[] {
        const windows = this.getAllWindowStates();
        return Object.entries(windows)
            .filter(([_, state]) => !state.minimized)
            .map(([id, _]) => id);
    }
}
```

---

## Next Steps

1. **Commit this document** to the repository
2. **Review technical decisions** - ensure alignment on approach
3. **Start implementation** - follow the steps outlined above
4. **Test incrementally** - each step should be testable
5. **Iterate based on feedback** - adjust as needed during implementation
