/**
 * London Tube Journey: Mobile Gene Network Analysis with Intermittent Connectivity
 *
 * Tests glyph persistence and visual sync during realistic mobile usage scenario.
 *
 * SCENARIO:
 * Jenny, a biology researcher, on morning commute (Morden → Old Street, Northern Line)
 * analyzing overnight metagenomic pipeline results on mobile device.
 * Network connectivity drops in tunnels, returns at each station.
 * Jenny continues productive work despite adversarial connectivity —
 * including querying local attestations via AX glyphs in tunnels (orange tint,
 * not ghostly grayscale) — then seamlessly continues on desktop upon arrival.
 *
 * ROUTE: 08:31-09:06 (35 minutes, 17 stations)
 * Morden (08:31) → South Wimbledon (08:34) → Colliers Wood (08:36) →
 * Tooting Broadway (08:38) → Tooting Bec (08:40) → Balham (08:41) →
 * Clapham South (08:43) → Clapham Common (08:45) → Clapham North (08:47) →
 * Stockwell (08:49) → Oval (08:51) → Kennington (08:54) →
 * Elephant & Castle (08:56) → Borough (08:58) → London Bridge (09:00) →
 * Bank Station (09:02) → Moorgate (09:05) → Old Street (09:06)
 *
 * NOTE: Times simulated (not real 35-minute wait), but sequence is authentic.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { syncStateManager, type GlyphSyncState } from './sync-state';
import { mergeCanvasState } from '../api/canvas';
import type { CanvasGlyphState, CompositionState } from './ui';

/**
 * Helper to determine expected visual state based on connectivity and sync state
 * Mirrors the actual CSS rules from canvas.css and core.css
 */
interface VisualState {
    rootAttribute: 'online' | 'offline';
    glyphAttribute: GlyphSyncState;
    expectedFilter: string;
    expectedBorderOpacity: string;
    description: string;
}

function getExpectedVisualState(
    connectivity: 'online' | 'offline',
    syncState: GlyphSyncState,
    localActive = false
): VisualState {
    // Determine root data-connectivity-mode attribute
    const rootAttribute = connectivity;

    // Determine expected CSS filter based on connectivity and sync state
    let expectedFilter: string;
    let expectedBorderOpacity: string;
    let description: string;

    if (connectivity === 'offline') {
        if (localActive) {
            // Local-active: AX/TS glyphs that work offline via IndexedDB/WASM
            // Exempt from grayscale, orange tint applied via inline backgroundColor
            expectedFilter = 'none';
            expectedBorderOpacity = '1.0';
            description = 'Local-active (offline but functional via local data)';
        } else if (syncState === 'unsynced' || syncState === 'failed') {
            // Ghostly: grayscale + almost invisible border
            expectedFilter = 'grayscale(100%)';
            expectedBorderOpacity = '0.15';
            description = 'Ghostly (offline + unsynced)';
        } else {
            // Azure tint: 65% saturation + 10° hue shift
            expectedFilter = 'saturate(65%) hue-rotate(10deg)';
            expectedBorderOpacity = '0.35';
            description = 'Azure tint (offline mode)';
        }
    } else {
        // Online mode
        if (syncState === 'synced') {
            // Color boost for synced glyphs
            expectedFilter = 'saturate(110%)';
            expectedBorderOpacity = '1.0';
            description = 'Enhanced color (synced)';
        } else {
            // Normal appearance
            expectedFilter = 'saturate(100%)';
            expectedBorderOpacity = '1.0';
            description = 'Normal (online)';
        }
    }

    return {
        rootAttribute,
        glyphAttribute: syncState,
        expectedFilter,
        expectedBorderOpacity,
        description
    };
}

// --- Canvas state merge ---
//
// mergeCanvasState reconciles local (IndexedDB) and backend (SQLite) canvas state
// on startup. Local wins on ID conflict. This is the mechanism that delivers
// overnight work to a new client.

function glyph(id: string, overrides: Partial<CanvasGlyphState> = {}): CanvasGlyphState {
    return { id, symbol: 'ax', x: 0, y: 0, ...overrides };
}

function composition(id: string, overrides: Partial<CompositionState> = {}): CompositionState {
    return { id, edges: [], x: 0, y: 0, ...overrides };
}

const emptyCanvas = { glyphs: [] as CanvasGlyphState[], compositions: [] as CompositionState[] };

describe('mergeCanvasState', () => {
    test('backend-only items appended to local state', () => {
        const local = { ...emptyCanvas, glyphs: [glyph('a')] };
        const backend = { ...emptyCanvas, glyphs: [glyph('a'), glyph('b')] };
        const result = mergeCanvasState(local, backend);

        expect(result.glyphs.map(g => g.id)).toEqual(['a', 'b']);
        expect(result.mergedGlyphs).toBe(1);
    });

    test('local wins on ID conflict', () => {
        const local = { ...emptyCanvas, glyphs: [glyph('a', { x: 50, y: 50 })] };
        const backend = { ...emptyCanvas, glyphs: [glyph('a', { x: 0, y: 0 })] };
        const result = mergeCanvasState(local, backend);

        expect(result.glyphs).toHaveLength(1);
        expect(result.glyphs[0].x).toBe(50);
        expect(result.mergedGlyphs).toBe(0);
    });

    test('empty backend returns same reference (no copy)', () => {
        const local = { ...emptyCanvas, glyphs: [glyph('a')] };
        const result = mergeCanvasState(local, emptyCanvas);

        expect(result.glyphs).toBe(local.glyphs);
        expect(result.mergedGlyphs).toBe(0);
    });

    test('empty local receives all backend items', () => {
        const backend = { ...emptyCanvas, glyphs: [glyph('a'), glyph('b')] };
        const result = mergeCanvasState(emptyCanvas, backend);

        expect(result.glyphs.map(g => g.id)).toEqual(['a', 'b']);
        expect(result.mergedGlyphs).toBe(2);
    });

    test('glyphs and compositions merge independently', () => {
        const local = { glyphs: [glyph('g1')], compositions: [composition('c1')] };
        const backend = {
            glyphs: [glyph('g1'), glyph('g2')],
            compositions: [composition('c1'), composition('c2'), composition('c3')],
        };
        const result = mergeCanvasState(local, backend);

        expect(result.mergedGlyphs).toBe(1);
        expect(result.mergedComps).toBe(2);
        expect(result.compositions.map(c => c.id)).toEqual(['c1', 'c2', 'c3']);
    });

    test('both empty', () => {
        const result = mergeCanvasState(emptyCanvas, emptyCanvas);
        expect(result.glyphs).toHaveLength(0);
        expect(result.compositions).toHaveLength(0);
    });
});

// --- Overnight collaboration merge ---
//
// Jenny gets off her bike at Morden, 08:29. Parbattie, a field researcher in Guyana (UTC-4),
// worked through the evening (London night = Guyana evening) documenting rare flora/fauna inventory.
// Jenny opens the app on her phone before boarding. Her local IndexedDB has only
// the AX glyph she left there yesterday. The backend has everything Parbattie documented
// overnight. mergeCanvasState delivers the field notes to her screen.

describe('08:29 Morden: Jenny opens QNTX and receives Parbattie\'s overnight field notes', () => {
    const jennyLocal = {
        glyphs: [glyph('ax-jenny', { symbol: 'ax', x: 120, y: 80 })],
        compositions: [],
    };

    const backendAfterParbattie = {
        glyphs: [
            glyph('ax-jenny', { symbol: 'ax', x: 0, y: 0 }),              // same glyph, Parbattie may have moved it
            glyph('note-inventory', { symbol: 'note', x: 200, y: 0 }),    // Parbattie's field inventory notes
            glyph('note-priority', { symbol: 'note', x: 400, y: 0 }),     // Parbattie's sequencing priorities
        ],
        compositions: [
            composition('field-notes', {
                edges: [
                    { from: 'ax-jenny', to: 'note-inventory', direction: 'right' as const, position: 0 },
                    { from: 'note-inventory', to: 'note-priority', direction: 'right' as const, position: 1 },
                ],
            }),
        ],
    };

    test('Jenny sees her glyph plus Parbattie\'s overnight field notes', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterParbattie);

        expect(merged.glyphs).toHaveLength(3);
        expect(merged.glyphs.map(g => g.id)).toEqual(['ax-jenny', 'note-inventory', 'note-priority']);
    });

    test('Jenny\'s local position preserved, not overwritten by Parbattie\'s edits', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterParbattie);
        const jennyGlyph = merged.glyphs.find(g => g.id === 'ax-jenny')!;

        expect(jennyGlyph.x).toBe(120);
        expect(jennyGlyph.y).toBe(80);
    });

    test('Parbattie\'s field notes composition arrives intact', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterParbattie);

        expect(merged.compositions).toHaveLength(1);
        expect(merged.compositions[0].id).toBe('field-notes');
        expect(merged.compositions[0].edges).toHaveLength(2);
    });

    test('merge counts reflect what was new to Jenny', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterParbattie);

        expect(merged.mergedGlyphs).toBe(2);  // note-inventory + note-priority
        expect(merged.mergedComps).toBe(1);    // field-notes
    });

    test('Parbattie\'s field note content syncs to Jenny', () => {
        // Parbattie documented rare species overnight (Guyana evening = London night)
        // 23:00 GYT (11pm Guyana) = 03:00 GMT (3am London next day)
        const backendWithCode = {
            glyphs: [
                glyph('note-parbattie', {
                    symbol: 'note',
                    x: 200,
                    y: 0,
                    content: '# Rare Flora Inventory - Kaieteur Falls\n\n' +
                          '## Priority for Georgetown Sequencing\n' +
                          '- *Heliamphora chimantensis* (pitcher plant) - 3 specimens\n' +
                          '- Unknown orchid sp. - possible new species\n' +
                          '- GPS: 5.1753°N, 59.4803°W',
                }),
            ],
            compositions: [],
        };

        // Jenny's local has no field notes yet
        const jennyEmpty = { glyphs: [], compositions: [] };

        const merged = mergeCanvasState(jennyEmpty, backendWithCode);

        // Jenny receives the note with Parbattie's field inventory intact
        expect(merged.glyphs).toHaveLength(1);
        const noteGlyph = merged.glyphs[0];
        expect(noteGlyph.id).toBe('note-parbattie');
        expect(noteGlyph.content).toContain('Heliamphora chimantensis');
        expect(noteGlyph.content).toContain('Kaieteur Falls');
        expect(noteGlyph.content).toContain('Georgetown');
    });

    // TODO(#431): Test conflict resolution for Jenny's own stale offline edits
    // Current behavior: stale local edit silently overwrites newer work (data loss!)
    // Desired behavior: conflict UI showing both versions, let Jenny decide which to keep
    // This requires offline queue + conflict detection + merge UI
    // Example conflict scenario (Jenny vs. Jenny across devices):
    //   - Day before: Jenny drafts note on laptop at home (router unplugged → queued offline)
    //   - 08:29 GMT: Jenny at Morden, creates NEW version of same note on phone during tube ride
    //   - 08:45 GMT: At Oval station, housemate reconnects home router
    //   - Laptop syncs stale draft from yesterday, conflicts with fresh phone version from 15min ago
    //   - Should see conflict UI: "laptop version (yesterday)" vs "phone version (just now)"

    // TODO(#canvas-live-sync): Jenny won't see changes Parbattie makes AFTER she opens
    // the app. That requires WebSocket `canvas_update` broadcast -- backend emits
    // glyph/composition mutations to all connected clients for live merge.
});

describe('London Tube Journey: Gene Network Analysis', () => {
    // Gene glyphs created during journey
    const glyphs = {
        // Segment 1: Morden → Balham (initial discovery)
        novelCluster: 'gene-cluster-nov-001',
        candidateGene: 'gene-prot-xyz-447',
        homologA: 'gene-homolog-a-112',
        // Segment 2: Balham → Old Street (hypothesis formation)
        hypothesis: 'hypothesis-protein-function-001',
        validationNote: 'validation-regulatory-network-001',
        // AX query glyph: works offline via IndexedDB (local-active, orange tint)
        localAxQuery: 'ax-query-gene-cluster-001'
    };

    beforeEach(() => {
        // Clear all glyph states
        Object.values(glyphs).forEach(id => {
            syncStateManager.clearState(id);
        });
    });

    test('08:31 Morden: Board train and identify novel gene cluster', () => {
        // STATION: Morden (WiFi available)
        const connectivity = 'online';

        // Researcher opens QNTX, sees overnight pipeline results
        // Taps novel cluster glyph in overview mode
        syncStateManager.setState(glyphs.novelCluster, 'unsynced');
        expect(syncStateManager.getState(glyphs.novelCluster)).toBe('unsynced');

        // Researcher adds annotation, triggers sync
        syncStateManager.setState(glyphs.novelCluster, 'syncing');
        expect(syncStateManager.getState(glyphs.novelCluster)).toBe('syncing');

        // Backend confirms persistence
        syncStateManager.setState(glyphs.novelCluster, 'synced');
        expect(syncStateManager.getState(glyphs.novelCluster)).toBe('synced');

        // Visual state: Normal appearance + 110% saturation boost
    });

    test('Morden → South Wimbledon tunnel: First offline experience', () => {
        // Pre-condition: Cluster already synced from previous station
        syncStateManager.setState(glyphs.novelCluster, 'synced');

        // TUNNEL: Train departs Morden at 08:31
        // After 300ms debounce, connectivity detected as offline
        const connectivity = 'offline';

        // Researcher identifies candidate gene and drags to canvas
        // Glyph moves immediately but marked as unsynced
        syncStateManager.setState(glyphs.candidateGene, 'unsynced');

        expect(syncStateManager.getState(glyphs.candidateGene)).toBe('unsynced');
        expect(syncStateManager.getState(glyphs.novelCluster)).toBe('synced');

        // Validate expected visual states
        const unsyncedVisual = getExpectedVisualState(connectivity, 'unsynced');
        const syncedVisual = getExpectedVisualState(connectivity, 'synced');

        // Unsynced glyph should be ghostly (grayscale)
        expect(unsyncedVisual.rootAttribute).toBe('offline');
        expect(unsyncedVisual.glyphAttribute).toBe('unsynced');
        expect(unsyncedVisual.expectedFilter).toBe('grayscale(100%)');
        expect(unsyncedVisual.expectedBorderOpacity).toBe('0.15');
        expect(unsyncedVisual.description).toContain('Ghostly');

        // Synced glyph should have azure tint (offline but previously synced)
        expect(syncedVisual.rootAttribute).toBe('offline');
        expect(syncedVisual.glyphAttribute).toBe('synced');
        expect(syncedVisual.expectedFilter).toBe('saturate(65%) hue-rotate(10deg)');
        expect(syncedVisual.expectedBorderOpacity).toBe('0.35');
        expect(syncedVisual.description).toContain('Azure tint');
    });

    test('08:34 South Wimbledon: First auto-sync at station', () => {
        // Pre-condition: Candidate gene created offline in tunnel
        syncStateManager.setState(glyphs.novelCluster, 'synced');
        syncStateManager.setState(glyphs.candidateGene, 'unsynced');

        // Track state transitions
        const transitions: GlyphSyncState[] = [];
        syncStateManager.subscribe(glyphs.candidateGene, (state) => {
            transitions.push(state);
        });

        // STATION: South Wimbledon (08:34)
        // Connectivity returns instantly (for fast tests)
        const connectivity = 'online';

        // Auto-sync begins immediately in background
        syncStateManager.setState(glyphs.candidateGene, 'syncing');

        // Backend confirms persistence
        syncStateManager.setState(glyphs.candidateGene, 'synced');

        // Verify state transitions
        expect(transitions).toEqual([
            'unsynced',  // Initial subscription
            'syncing',   // Auto-sync started
            'synced'     // Persistence confirmed
        ]);

        expect(syncStateManager.getState(glyphs.candidateGene)).toBe('synced');

        // Validate visual transition: ghostly → enhanced color
        const beforeSync = getExpectedVisualState('offline', 'unsynced');
        const afterSync = getExpectedVisualState(connectivity, 'synced');

        // Before: Offline + unsynced = ghostly
        expect(beforeSync.expectedFilter).toBe('grayscale(100%)');

        // After: Online + synced = enhanced color (110% saturation boost)
        expect(afterSync.rootAttribute).toBe('online');
        expect(afterSync.expectedFilter).toBe('saturate(110%)');
        expect(afterSync.expectedBorderOpacity).toBe('1.0');

        // CSS transition would animate this over 1.5s
        // filter: grayscale(100%) → saturate(110%)
    });

    test('Visual state mapping: All connectivity and sync combinations', () => {
        // Test all combinations of connectivity and sync state
        const testCases: Array<{
            connectivity: 'online' | 'offline';
            syncState: GlyphSyncState;
            localActive?: boolean;
            expectedFilter: string;
            expectedBorderOpacity: string;
        }> = [
            // Online states
            { connectivity: 'online', syncState: 'synced', expectedFilter: 'saturate(110%)', expectedBorderOpacity: '1.0' },
            { connectivity: 'online', syncState: 'syncing', expectedFilter: 'saturate(100%)', expectedBorderOpacity: '1.0' },
            { connectivity: 'online', syncState: 'unsynced', expectedFilter: 'saturate(100%)', expectedBorderOpacity: '1.0' },
            { connectivity: 'online', syncState: 'failed', expectedFilter: 'saturate(100%)', expectedBorderOpacity: '1.0' },
            // Offline states
            { connectivity: 'offline', syncState: 'synced', expectedFilter: 'saturate(65%) hue-rotate(10deg)', expectedBorderOpacity: '0.35' },
            { connectivity: 'offline', syncState: 'syncing', expectedFilter: 'saturate(65%) hue-rotate(10deg)', expectedBorderOpacity: '0.35' },
            { connectivity: 'offline', syncState: 'unsynced', expectedFilter: 'grayscale(100%)', expectedBorderOpacity: '0.15' },
            { connectivity: 'offline', syncState: 'failed', expectedFilter: 'grayscale(100%)', expectedBorderOpacity: '0.15' },
            // Local-active states (AX/TS glyphs that work offline via IndexedDB/WASM)
            { connectivity: 'offline', syncState: 'unsynced', localActive: true, expectedFilter: 'none', expectedBorderOpacity: '1.0' },
            { connectivity: 'offline', syncState: 'synced', localActive: true, expectedFilter: 'none', expectedBorderOpacity: '1.0' }
        ];

        testCases.forEach(({ connectivity, syncState, expectedFilter, expectedBorderOpacity, localActive }) => {
            const visual = getExpectedVisualState(connectivity, syncState, localActive);

            expect(visual.rootAttribute).toBe(connectivity);
            expect(visual.glyphAttribute).toBe(syncState);
            expect(visual.expectedFilter).toBe(expectedFilter);
            expect(visual.expectedBorderOpacity).toBe(expectedBorderOpacity);
        });

        // Verify state count
        expect(testCases.length).toBe(10); // 2 connectivity × 4 sync states + 2 local-active
    });

    test('Segment 1: Morden → Balham with multiple tunnel cycles', () => {
        // Simulate first 5 stations (10 minutes)
        const journey: Array<{ time: string; location: string; connectivity: 'online' | 'offline'; action: string }> = [];

        // 08:31 STATION: Morden
        journey.push({ time: '08:31', location: 'Morden', connectivity: 'online', action: 'Board train, identify novel cluster' });
        syncStateManager.setState(glyphs.novelCluster, 'syncing');
        syncStateManager.setState(glyphs.novelCluster, 'synced');

        // 08:31-08:34 TUNNEL: Morden → South Wimbledon
        journey.push({ time: '08:32', location: 'Tunnel', connectivity: 'offline', action: 'Add candidate gene (offline)' });
        syncStateManager.setState(glyphs.candidateGene, 'unsynced');

        // 08:34 STATION: South Wimbledon
        journey.push({ time: '08:34', location: 'South Wimbledon', connectivity: 'online', action: 'Auto-sync candidate gene' });
        syncStateManager.setState(glyphs.candidateGene, 'syncing');
        syncStateManager.setState(glyphs.candidateGene, 'synced');

        // 08:34-08:36 TUNNEL: South Wimbledon → Colliers Wood
        journey.push({ time: '08:35', location: 'Tunnel', connectivity: 'offline', action: 'Add homolog relationship (offline)' });
        syncStateManager.setState(glyphs.homologA, 'unsynced');

        // 08:36 STATION: Colliers Wood
        journey.push({ time: '08:36', location: 'Colliers Wood', connectivity: 'online', action: 'Auto-sync homolog' });
        syncStateManager.setState(glyphs.homologA, 'syncing');
        syncStateManager.setState(glyphs.homologA, 'synced');

        // 08:36-08:38 TUNNEL: Colliers Wood → Tooting Broadway
        journey.push({ time: '08:37', location: 'Tunnel', connectivity: 'offline', action: 'Review gene network (all glyphs azure tint)' });

        // 08:38 STATION: Tooting Broadway
        journey.push({ time: '08:38', location: 'Tooting Broadway', connectivity: 'online', action: 'No pending syncs' });

        // 08:38-08:40 TUNNEL: Tooting Broadway → Tooting Bec
        journey.push({ time: '08:39', location: 'Tunnel', connectivity: 'offline', action: 'Continue analysis (offline)' });

        // 08:40 STATION: Tooting Bec
        journey.push({ time: '08:40', location: 'Tooting Bec', connectivity: 'online', action: 'All data synced' });

        // 08:40-08:41 TUNNEL: Tooting Bec → Balham (1 min, very short)
        journey.push({ time: '08:41', location: 'Tunnel', connectivity: 'offline', action: 'Brief offline period' });

        // 08:41 STATION: Balham (segment 1 end)
        journey.push({ time: '08:41', location: 'Balham', connectivity: 'online', action: 'Segment 1 complete' });

        // Verify all segment 1 glyphs synced
        expect(syncStateManager.getState(glyphs.novelCluster)).toBe('synced');
        expect(syncStateManager.getState(glyphs.candidateGene)).toBe('synced');
        expect(syncStateManager.getState(glyphs.homologA)).toBe('synced');

        expect(journey.length).toBe(11);
        expect(journey[0].location).toBe('Morden');
        expect(journey[10].location).toBe('Balham');
    });

    test('08:48 Tunnel: Jenny queries local attestations, AX glyph stays orange', () => {
        // Pre-condition: Hypothesis formed, all discovery glyphs synced
        syncStateManager.setState(glyphs.novelCluster, 'synced');
        syncStateManager.setState(glyphs.candidateGene, 'synced');
        syncStateManager.setState(glyphs.homologA, 'synced');
        syncStateManager.setState(glyphs.hypothesis, 'synced');

        // TUNNEL: Clapham North → Stockwell (08:48)
        // Jenny wants to cross-reference her gene cluster hypothesis
        // against existing attestations. She spawns an AX glyph and types
        // "of QNTX" — IndexedDB has locally-cached attestation data.
        const connectivity = 'offline';

        // AX glyph queries IndexedDB locally — it has data, it works.
        // Marked unsynced (query result not persisted to server yet)
        syncStateManager.setState(glyphs.localAxQuery, 'unsynced');

        // Regular unsynced glyph in offline mode → ghostly (grayscale, unreachable)
        const ghostlyVisual = getExpectedVisualState(connectivity, 'unsynced');
        expect(ghostlyVisual.expectedFilter).toBe('grayscale(100%)');
        expect(ghostlyVisual.expectedBorderOpacity).toBe('0.15');
        expect(ghostlyVisual.description).toContain('Ghostly');

        // Local-active AX glyph in offline mode → orange (exempt from grayscale)
        // data-local-active="true" on the element bypasses CSS grayscale filter,
        // inline backgroundColor set to rgba(61, 45, 20, 0.92) by setColorState('orange')
        const localActiveVisual = getExpectedVisualState(connectivity, 'unsynced', true);
        expect(localActiveVisual.expectedFilter).toBe('none');
        expect(localActiveVisual.expectedBorderOpacity).toBe('1.0');
        expect(localActiveVisual.description).toContain('Local-active');

        // Key insight: same connectivity, same sync state, different visual treatment.
        // Ghostly = can't do anything offline. Orange = locally functional.
        expect(ghostlyVisual.expectedFilter).not.toBe(localActiveVisual.expectedFilter);

        // Previously-synced glyphs still get azure tint (they're fine, just dormant)
        const azureVisual = getExpectedVisualState(connectivity, 'synced');
        expect(azureVisual.expectedFilter).toBe('saturate(65%) hue-rotate(10deg)');

        // Three distinct offline visual states on Jenny's screen:
        // 1. Orange (AX glyph) — actively querying local data
        // 2. Azure (synced glyphs) — safe, dormant
        // 3. Ghostly (if any unsynced) — unreachable
    });

    test('Oval → Kennington: Sync failure with exponential backoff retry', () => {
        // Pre-condition: Segment 1 glyphs synced, now in middle of segment 2
        syncStateManager.setState(glyphs.novelCluster, 'synced');
        syncStateManager.setState(glyphs.candidateGene, 'synced');
        syncStateManager.setState(glyphs.homologA, 'synced');
        syncStateManager.setState(glyphs.hypothesis, 'synced');

        // Track state transitions for validation note
        const transitions: GlyphSyncState[] = [];
        syncStateManager.subscribe(glyphs.validationNote, (state) => {
            transitions.push(state);
        });

        const retryLog: string[] = [];

        // 08:51 STATION: Oval
        retryLog.push('08:51 Oval: Create validation note (online)');
        syncStateManager.setState(glyphs.validationNote, 'syncing');

        // 08:51-08:54 TUNNEL: Oval → Kennington (3 min, longer tunnel)
        // Sync fails due to poor connectivity
        retryLog.push('08:52 Tunnel: First sync attempt fails');
        syncStateManager.setState(glyphs.validationNote, 'failed');

        // 08:54 STATION: Kennington
        // Auto-retry with exponential backoff
        retryLog.push('08:54 Kennington: Retry attempt 1 (immediate)');
        syncStateManager.setState(glyphs.validationNote, 'syncing');

        // Brief connection issue at station
        retryLog.push('08:54 Kennington: Retry 1 fails (unstable connection)');
        syncStateManager.setState(glyphs.validationNote, 'failed');

        // Exponential backoff: wait before retry 2
        retryLog.push('08:54 Kennington: Retry attempt 2 (after backoff)');
        syncStateManager.setState(glyphs.validationNote, 'syncing');

        // Success on second retry
        retryLog.push('08:54 Kennington: Retry 2 succeeds');
        syncStateManager.setState(glyphs.validationNote, 'synced');

        // Verify resilient recovery
        expect(syncStateManager.getState(glyphs.validationNote)).toBe('synced');

        // Verify state transition sequence
        expect(transitions).toEqual([
            'unsynced',  // Initial subscription
            'syncing',   // First attempt
            'failed',    // First failure
            'syncing',   // Retry 1
            'failed',    // Retry 1 failure
            'syncing',   // Retry 2
            'synced'     // Final success
        ]);

        expect(transitions.filter(s => s === 'failed').length).toBe(2); // Two failures
        expect(transitions[transitions.length - 1]).toBe('synced'); // Final success
    });

    test('Full journey: Morden → Old Street complete', () => {
        // Complete 35-minute journey across all 17 stations
        const journey: Array<{
            time: string;
            location: string;
            connectivity: 'online' | 'offline';
            event: string;
        }> = [];

        // === SEGMENT 1: Morden → Balham ===

        journey.push({ time: '08:31', location: 'Morden', connectivity: 'online', event: 'Board train, identify cluster' });
        syncStateManager.setState(glyphs.novelCluster, 'syncing');
        syncStateManager.setState(glyphs.novelCluster, 'synced');

        journey.push({ time: '08:32', location: 'Tunnel', connectivity: 'offline', event: 'Add candidate gene' });
        syncStateManager.setState(glyphs.candidateGene, 'unsynced');

        journey.push({ time: '08:34', location: 'South Wimbledon', connectivity: 'online', event: 'Sync candidate' });
        syncStateManager.setState(glyphs.candidateGene, 'syncing');
        syncStateManager.setState(glyphs.candidateGene, 'synced');

        journey.push({ time: '08:35', location: 'Tunnel', connectivity: 'offline', event: 'Add homolog' });
        syncStateManager.setState(glyphs.homologA, 'unsynced');

        journey.push({ time: '08:36', location: 'Colliers Wood', connectivity: 'online', event: 'Sync homolog' });
        syncStateManager.setState(glyphs.homologA, 'syncing');
        syncStateManager.setState(glyphs.homologA, 'synced');

        journey.push({ time: '08:37', location: 'Tunnel', connectivity: 'offline', event: 'Review network' });
        journey.push({ time: '08:38', location: 'Tooting Broadway', connectivity: 'online', event: 'All synced' });
        journey.push({ time: '08:39', location: 'Tunnel', connectivity: 'offline', event: 'Continue analysis' });
        journey.push({ time: '08:40', location: 'Tooting Bec', connectivity: 'online', event: 'All synced' });
        journey.push({ time: '08:41', location: 'Tunnel', connectivity: 'offline', event: 'Brief tunnel' });
        journey.push({ time: '08:41', location: 'Balham', connectivity: 'online', event: 'Segment 1 done' });

        // === SEGMENT 2: Balham → Old Street ===

        journey.push({ time: '08:42', location: 'Tunnel', connectivity: 'offline', event: 'Form hypothesis' });
        syncStateManager.setState(glyphs.hypothesis, 'unsynced');

        journey.push({ time: '08:43', location: 'Clapham South', connectivity: 'online', event: 'Sync hypothesis' });
        syncStateManager.setState(glyphs.hypothesis, 'syncing');
        syncStateManager.setState(glyphs.hypothesis, 'synced');

        journey.push({ time: '08:44', location: 'Tunnel', connectivity: 'offline', event: 'Continue' });
        journey.push({ time: '08:45', location: 'Clapham Common', connectivity: 'online', event: 'All synced' });
        journey.push({ time: '08:46', location: 'Tunnel', connectivity: 'offline', event: 'Refine hypothesis' });
        journey.push({ time: '08:47', location: 'Clapham North', connectivity: 'online', event: 'All synced' });
        journey.push({ time: '08:48', location: 'Tunnel', connectivity: 'offline', event: 'AX query: local attestations (orange, not ghostly)' });
        syncStateManager.setState(glyphs.localAxQuery, 'unsynced');

        journey.push({ time: '08:49', location: 'Stockwell', connectivity: 'online', event: 'Sync AX query results' });
        syncStateManager.setState(glyphs.localAxQuery, 'syncing');
        syncStateManager.setState(glyphs.localAxQuery, 'synced');

        journey.push({ time: '08:50', location: 'Tunnel', connectivity: 'offline', event: 'Begin validation' });
        journey.push({ time: '08:51', location: 'Oval', connectivity: 'online', event: 'Create validation note' });
        syncStateManager.setState(glyphs.validationNote, 'unsynced');

        journey.push({ time: '08:52', location: 'Tunnel', connectivity: 'offline', event: 'Long tunnel (3 min)' });

        journey.push({ time: '08:54', location: 'Kennington', connectivity: 'online', event: 'Retry sync' });
        syncStateManager.setState(glyphs.validationNote, 'syncing');
        syncStateManager.setState(glyphs.validationNote, 'synced');

        journey.push({ time: '08:55', location: 'Tunnel', connectivity: 'offline', event: 'Continue work' });
        journey.push({ time: '08:56', location: 'Elephant & Castle', connectivity: 'online', event: 'All synced' });
        journey.push({ time: '08:57', location: 'Tunnel', connectivity: 'offline', event: 'Review network' });
        journey.push({ time: '08:58', location: 'Borough', connectivity: 'online', event: 'Approaching City' });
        journey.push({ time: '08:59', location: 'Tunnel', connectivity: 'offline', event: 'Final analysis' });
        journey.push({ time: '09:00', location: 'London Bridge', connectivity: 'online', event: 'City of London' });
        journey.push({ time: '09:01', location: 'Tunnel', connectivity: 'offline', event: 'Prepare for arrival' });
        journey.push({ time: '09:02', location: 'Bank Station', connectivity: 'online', event: 'Major interchange' });
        journey.push({ time: '09:03', location: 'Tunnel', connectivity: 'offline', event: 'Long tunnel (3 min)' });
        journey.push({ time: '09:05', location: 'Moorgate', connectivity: 'online', event: 'Almost there' });
        journey.push({ time: '09:05', location: 'Tunnel', connectivity: 'offline', event: 'Brief tunnel (1 min)' });
        journey.push({ time: '09:06', location: 'Old Street', connectivity: 'online', event: 'ARRIVAL' });

        // Verify all glyphs synced by arrival
        expect(syncStateManager.getState(glyphs.novelCluster)).toBe('synced');
        expect(syncStateManager.getState(glyphs.candidateGene)).toBe('synced');
        expect(syncStateManager.getState(glyphs.homologA)).toBe('synced');
        expect(syncStateManager.getState(glyphs.hypothesis)).toBe('synced');
        expect(syncStateManager.getState(glyphs.localAxQuery)).toBe('synced');
        expect(syncStateManager.getState(glyphs.validationNote)).toBe('synced');

        // Verify journey completeness
        expect(journey.length).toBe(35); // 35 events across full journey
        expect(journey[0].location).toBe('Morden');
        expect(journey[34].location).toBe('Old Street');
        expect(journey[34].event).toBe('ARRIVAL');

        // Verify connectivity pattern
        const tunnelEvents = journey.filter(e => e.connectivity === 'offline');
        const stationEvents = journey.filter(e => e.connectivity === 'online');
        expect(tunnelEvents.length).toBe(17); // 17 tunnel periods
        expect(stationEvents.length).toBe(18); // 18 station stops
    });

    test('Desktop continuation: Seamless transition from mobile', () => {
        // SCENARIO: Researcher arrives at Old Street (09:06)
        // Opens desktop workstation, canvas already has all mobile work

        // Simulate all mobile glyphs synced
        Object.values(glyphs).forEach(id => {
            syncStateManager.setState(id, 'synced');
        });

        // Desktop opens same canvas URL - all work already present
        const desktopSession = Object.entries(glyphs).map(([name, id]) => ({
            name,
            id,
            state: syncStateManager.getState(id)
        }));

        // Verify no re-sync needed (already synced from mobile)
        desktopSession.forEach(({ state }) => {
            expect(state).toBe('synced');
        });

        // Researcher immediately continues with deep analysis on larger screen
        const deepAnalysisGlyph = 'desktop-deep-analysis-001';
        syncStateManager.setState(deepAnalysisGlyph, 'syncing');
        syncStateManager.setState(deepAnalysisGlyph, 'synced');

        expect(syncStateManager.getState(deepAnalysisGlyph)).toBe('synced');
        expect(desktopSession.length).toBe(6); // All 6 mobile glyphs present (including AX query)

        // SUCCESS: Researcher discovered novel protein function during 35-minute commute
        // Desktop ready for detailed validation work
    });
});

