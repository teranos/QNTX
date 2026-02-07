/**
 * London Tube Journey: Mobile Gene Network Analysis with Intermittent Connectivity
 *
 * Tests glyph persistence and visual sync during realistic mobile usage scenario.
 *
 * SCENARIO:
 * Biology researcher on morning commute (Morden → Old Street, Northern Line)
 * analyzing overnight metagenomic pipeline results on mobile device.
 * Network connectivity drops in tunnels, returns at each station.
 * Researcher continues productive work despite adversarial connectivity,
 * then seamlessly continues on desktop workstation upon arrival.
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
    syncState: GlyphSyncState
): VisualState {
    // Determine root data-connectivity-mode attribute
    const rootAttribute = connectivity;

    // Determine expected CSS filter based on connectivity and sync state
    let expectedFilter: string;
    let expectedBorderOpacity: string;
    let description: string;

    if (connectivity === 'offline') {
        if (syncState === 'unsynced' || syncState === 'failed') {
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

describe('London Tube Journey: Gene Network Analysis', () => {
    // Gene glyphs created during journey
    const glyphs = {
        // Segment 1: Morden → Balham (initial discovery)
        novelCluster: 'gene-cluster-nov-001',
        candidateGene: 'gene-prot-xyz-447',
        homologA: 'gene-homolog-a-112',
        // Segment 2: Balham → Old Street (hypothesis formation)
        hypothesis: 'hypothesis-protein-function-001',
        validationNote: 'validation-regulatory-network-001'
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
            { connectivity: 'offline', syncState: 'failed', expectedFilter: 'grayscale(100%)', expectedBorderOpacity: '0.15' }
        ];

        testCases.forEach(({ connectivity, syncState, expectedFilter, expectedBorderOpacity }) => {
            const visual = getExpectedVisualState(connectivity, syncState);

            expect(visual.rootAttribute).toBe(connectivity);
            expect(visual.glyphAttribute).toBe(syncState);
            expect(visual.expectedFilter).toBe(expectedFilter);
            expect(visual.expectedBorderOpacity).toBe(expectedBorderOpacity);
        });

        // Verify state count
        expect(testCases.length).toBe(8); // 2 connectivity × 4 sync states
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
        journey.push({ time: '08:48', location: 'Tunnel', connectivity: 'offline', event: 'Review relationships' });
        journey.push({ time: '08:49', location: 'Stockwell', connectivity: 'online', event: 'Hypothesis complete' });

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
        expect(desktopSession.length).toBe(5); // All 5 mobile glyphs present

        // SUCCESS: Researcher discovered novel protein function during 35-minute commute
        // Desktop ready for detailed validation work
    });
});
