# Glyph Persistence: Visual Sync State System

**Status:** Planning
**Priority:** Critical for mobile/offline usage
**Scope:** Canvas workspace visual feedback for connectivity and sync state

## Problem Statement

Users working with glyphs on mobile devices with spotty network connectivity need real-time visual feedback about:

1. Current connectivity state (online/offline)
2. Which glyphs are synced to backend vs local-only
3. What's happening during drag/modification operations
4. Whether data is at risk of loss

Current implementation syncs glyphs to backend but provides no visual feedback. This creates uncertainty, especially on mobile where network state is unpredictable.

## Design Philosophy Alignment

This proposal aligns with QNTX design philosophy:

- **Data-First Hierarchy**: Connectivity state is critical data that affects user decision-making
- **Semantic Clarity**: Visual state communicates sync status without explicit labels
- **Functional Color**: Color palette shifts serve functional purpose (indicating connectivity mode)
- **Performance as Constraint**: CSS custom properties and minimal JS for mode switching
- **No Unnecessary Effects**: No borders, badges, or decorative indicators - entire visual state shifts

## Visual Language System

### Two Distinct Modes

**Offline/Local-Only Mode** (Optimized for mobile/small screens):
- Base palette: mid-gray with azure-ish tones
- Background: lighter than current (improved readability in dark mode)
- Canvas workspace: server-side results shown in grayscale (stale/cached data)
- Overall: almost monochrome, desaturated
- Purpose: signals working locally, better small-screen readability, clear distinction between live and stale server data

**Online/Connected Mode** (Optimized for desktop/reliable connection):
- Base palette: current colors with enhanced vibrancy
- Background: current darkness levels
- Canvas workspace: clean, no stripes
- Overall: full color saturation with sync-based enhancement
- Purpose: signals live connection, enhanced visual richness for synced content

### Glyph Visual States

#### Offline Mode States

1. **Unmodified offline glyph**
   - Azure-ish/mid-gray monochrome
   - Base level of desaturation

2. **Being modified offline** (during drag)
   - Extra colorless compared to base offline state
   - Further desaturation to indicate "dirty" state
   - Applied immediately when drag starts

3. **Modified but not synced** (after drop, still offline)
   - Remains in extra-monochrome state
   - Visual memory of offline modification

#### Online Mode States

1. **Unsynced glyph** (online but not yet synced)
   - Normal color palette (no enhancement)

2. **Syncing glyph**
   - Transitioning from monochrome/desaturated to enhanced colors
   - Smooth CSS transition (animation follows natural easing)

3. **Synced glyph** (successfully persisted to backend)
   - Enhanced color vibrancy (sync reward)
   - Only synced glyphs receive color boost

### Transition Behavior

When connectivity changes offline → online:
- Glyphs that were modified offline gradually fade from monochrome to full color
- Transition duration: ~1-2 seconds (feels natural, not instant)
- Synced glyphs receive enhanced color boost progressively
- Server-side results (watcher matches) transition from grayscale to full color

When connectivity changes online → offline:
- All glyphs desaturate to azure-ish monochrome
- Server-side results (watcher matches) desaturate to grayscale (stale data indicator)
- Instant feedback that mode changed

## Technical Implementation

### 1. Connectivity Detection

**File:** `web/ts/connectivity.ts` (new)

```typescript
export type ConnectivityState = 'online' | 'offline';

export interface ConnectivityManager {
    state: ConnectivityState;
    subscribe(callback: (state: ConnectivityState) => void): () => void;
}
```

**Detection strategy:**
- Monitor `navigator.onLine` (browser API)
- Monitor WebSocket connection state (more reliable than browser API)
- Combine both: offline if either reports offline
- Debounce state changes (300ms) in both directions to avoid flapping

**Why both sources:** `navigator.onLine` can report false positives. WebSocket state is ground truth for QNTX backend connectivity.

**Why equal debounce:** Consistent 300ms delay prevents UI flapping during unstable connections, applies uniformly to both online→offline and offline→online transitions.

### 2. Sync State Tracking

**File:** `web/ts/state/sync-state.ts` (new)

```typescript
export type GlyphSyncState =
    | 'unsynced'    // Never sent to backend
    | 'syncing'     // Request in flight
    | 'synced'      // Confirmed by backend
    | 'failed';     // Sync attempt failed

export interface SyncStateManager {
    getState(glyphId: string): GlyphSyncState;
    setState(glyphId: string, state: GlyphSyncState): void;
    subscribe(glyphId: string, callback: (state: GlyphSyncState) => void): () => void;
}
```

**Integration points:**
- `web/ts/api/canvas.ts`: Update sync state before/after API calls
- `web/ts/state/ui.ts`: Trigger callbacks when state changes
- `web/ts/components/glyph/`: Visual components subscribe to state changes

### 3. Visual Mode System

**File:** `web/ts/visual-mode.ts` (new)

Manages CSS custom property updates for mode switching.

**CSS Custom Properties** (add to `web/css/variables.css`):

```css
:root {
    /* Online mode (default) */
    --mode-bg-lightness: 15%;
    --mode-glyph-saturation: 100%;
    --mode-glyph-boost: 1.0;

    /* Transition timing */
    --mode-transition-duration: 1.5s;
    --mode-transition-timing: cubic-bezier(0.4, 0.0, 0.2, 1);
}

:root[data-connectivity-mode="offline"] {
    --mode-bg-lightness: 25%;
    --mode-glyph-saturation: 20%;
    --mode-glyph-boost: 0.8;
}
```

**Glyph state classes:**

```css
.canvas-glyph {
    filter: saturate(var(--mode-glyph-saturation));
    transition: filter var(--mode-transition-duration) var(--mode-transition-timing);
}

.canvas-glyph[data-sync-state="synced"] {
    filter: saturate(calc(var(--mode-glyph-saturation) * var(--mode-glyph-boost)));
}

.canvas-glyph.is-dragging[data-connectivity-mode="offline"] {
    filter: saturate(10%); /* Extra desaturated while dragging offline */
}
```

### 4. Glyph Component Integration

**Files to modify:**
- `web/ts/components/glyph/glyph-interaction.ts`
- `web/ts/components/glyph/canvas-glyph.ts`
- `web/ts/components/glyph/py-glyph.ts`
- `web/ts/components/glyph/ax-glyph.ts`

**Changes needed:**

1. Subscribe to connectivity state
2. Apply `data-connectivity-mode` attribute to glyph elements
3. Apply `data-sync-state` attribute based on sync state
4. Add/remove `.is-dragging` class during drag operations
5. Update sync state after successful/failed API calls

### 5. Offline Queue System

**File:** `web/ts/offline-queue.ts` (new)

```typescript
export interface QueuedOperation {
    id: string;
    type: 'upsert_glyph' | 'delete_glyph' | 'upsert_composition' | 'delete_composition';
    payload: any;
    timestamp: number;
    retryCount: number;
}

export interface OfflineQueue {
    enqueue(operation: QueuedOperation): void;
    processQueue(): Promise<void>;
    getQueueSize(): number;
}
```

**Behavior:**
- Queue operations when offline
- Automatically process queue when connectivity returns
- Retry failed operations with exponential backoff
- Persist queue to IndexedDB (survives page refresh)

### 6. Server-Side Result Visibility

**Files to modify:**
- `web/ts/components/glyph/ax-glyph.ts` (handle stale watcher match results when offline)
- Any other components that display server-pushed data

**Behavior:**
- When offline: show cached server-side results in grayscale (indicates stale/not-live data)
- When online: show server-side results in full color (live data)
- Purpose: clear visual distinction between live server data and cached/stale results
- Note: Grayscale treatment aligns with overall offline visual language (desaturation = offline/stale)

## Implementation Phases

### Phase 1: Foundation (Current PR)
- [x] Backend persistence API
- [x] Frontend API client
- [x] Basic sync on drag/drop
- [ ] Connectivity detection system
- [ ] Sync state tracking infrastructure

### Phase 2: Visual System
- [ ] CSS custom properties for mode switching
- [ ] Visual mode manager
- [ ] Glyph component integration (apply classes)
- [ ] Server-side result hiding (offline mode)
- [ ] Transition animations

### Phase 3: Offline Support
- [ ] Offline queue implementation
- [ ] Queue persistence (IndexedDB)
- [ ] Automatic queue processing on reconnect
- [ ] Retry logic with exponential backoff

### Phase 4: Polish & Testing
- [ ] Mobile testing (real devices with spotty connections)
- [ ] Performance validation (no jank during transitions)
- [ ] Edge case handling (rapid connect/disconnect)
- [ ] User testing feedback

## Success Criteria

1. **Functional**: User can work offline and changes sync when online
2. **Perceptible**: Mode change is immediately obvious without conscious analysis
3. **Performant**: No jank during transitions or mode switches
4. **Reliable**: No data loss even with unstable connections
5. **Mobile-optimized**: Readable and usable on small screens in poor connectivity

## Open Questions for Implementation

### Question 1: Diagonal Stripe Pattern Implementation
Should the diagonal stripe pattern be:
- **Option A**: CSS background with linear-gradient (no external file, instant, but less crisp)
- **Option B**: SVG pattern file (external request, scalable, crisper lines)
- **Option C**: CSS-generated pseudo-element with border styling (no external file, crisp, more CSS)

### Question 2: Connectivity State Debouncing
For the 300ms debounce on connectivity state changes:
- **Option A**: Debounce only offline→online transitions (avoid premature "back online" signals)
- **Option B**: Debounce both directions equally (consistent behavior)
- **Option C**: Different timing for each direction (e.g., 300ms for online→offline, 500ms for offline→online)

### Question 3: Failed Sync Visual Treatment
When a glyph fails to sync (after retries exhausted):
- **Option A**: Return to "unsynced" state visually, silently log error
- **Option B**: Show distinct "failed" visual state (e.g., slight red tint to monochrome)
- **Option C**: Add glyph to a "needs attention" list, show global indicator

## Technical Constraints

1. **No new dependencies**: Use native browser APIs and existing libraries only
2. **Performance budget**: Mode transitions must complete in <16ms (60fps)
3. **Mobile-first**: Test on real devices, not just simulators
4. **Accessibility**: Color is not the only indicator - consider screen readers

## References

- Design Philosophy: `docs/design-philosophy.md`
- Existing Glyph System: `docs/vision/glyphs.md`
- Canvas State Management: `web/ts/state/ui.ts`
- Glyph Interaction: `web/ts/components/glyph/glyph-interaction.ts`
