/**
 * The default tray: every glyph the node opens with, added to the GlyphRun.
 *
 * The tray is what we use. All palette functionality has been replaced by the
 * tray, fully — the symbol row that sat beside it is gone.
 */

import { glyphRun } from '@qntx/glyphs';
import { createCanvasGlyph } from './components/glyph/canvas/canvas-glyph';
import { createChartGlyph } from './components/glyph/chart-glyph';
import { createDbGlyph } from './db-glyph';
import { createEmbeddingsGlyph } from './embeddings-glyph';
import { createSigmaPanel } from './sigma-panel';
import { createPluginGlyph } from './plugin-panel.ts';
import { createPulseGlyph } from './pulse-panel.ts';
import { createHandlersGlyph } from './handlers-panel.ts';
import { createLlmProviderGlyph } from './llm-provider-glyph.ts';
import { createTokensGlyph } from './tokens-glyph.ts';
import { createUsersGlyph } from './users-glyph.ts';
import { createMarketGlyph } from './market-glyph.ts';
import { createIGlyph } from './i-glyph.ts';
import { createAmGlyph } from './am-glyph.ts';
import { createAmConfigGlyph } from './config-panel.ts';
import { log, SEG } from './logger.ts';

export { updateDatabaseStats, recordEviction } from './db-glyph';
export { updateSigmaPanel } from './sigma-panel';

// The build and the backends are what the node is, so ≡ holds them. The names
// the socket already calls stay, and forward.
export { updateAmVersion as updateSelfVersion, updateAmCapabilities as updateSelfCapabilities } from './am-glyph.ts';

// Register default system glyphs
export function registerDefaultGlyphs(): void {
    // Canvas Glyph - Fractal container with spatial grid
    glyphRun.add(createCanvasGlyph());

    // Database Statistics Glyph
    glyphRun.add(createDbGlyph());

    // Sigma Overview Panel
    glyphRun.add(createSigmaPanel());

    // Embeddings Glyph
    glyphRun.add(createEmbeddingsGlyph());

    // ⍟ — who is looking
    glyphRun.add(createIGlyph());

    // ≡ — what the node is, and what it was told to be
    glyphRun.add(createAmGlyph());

    // ≡'s settings — the part of ≡ that does not fit in a window
    glyphRun.add(createAmConfigGlyph());

    // Access Tokens Glyph — opened from ⍟ (ADR-025)
    glyphRun.add(createTokensGlyph());
    glyphRun.add(createUsersGlyph());

    // Stands Glyph — every stand across markets (ADR-035)
    glyphRun.add(createMarketGlyph());

    // Usage & Cost Chart Glyph
    // TODO(future): Budget alerting with notifications
    // Implement cost threshold monitoring with user notifications:
    // - Config: User-defined budget limits (daily/weekly/monthly)
    // - Detection: Check total cost vs. budget in chart render
    // - Notification: Toast alert when threshold crossed
    // - Persistence: Store alert state to avoid repeat notifications
    // - UX: Clear visual indication of budget status in chart
    glyphRun.add(createChartGlyph(
        'usage-chart',
        'Usage & Costs',
        '/api/timeseries/usage',
        {
            primaryField: 'cost',
            secondaryField: 'requests',
            primaryLabel: 'Cost',
            secondaryLabel: 'Requests',
            primaryColor: '#4ade80',
            secondaryColor: '#60a5fa',
            chartType: 'area',
            formatValue: (v) => `$${v.toFixed(2)}`,
            defaultRange: 'month'
        },
        '$'
    ));

    // Pulse Panel Glyph — scheduled jobs dashboard
    glyphRun.add(createPulseGlyph());

    // Plugin Panel Glyph — panel manifestation
    glyphRun.add(createPluginGlyph());

    // Handlers Panel Glyph — handler attestation management
    glyphRun.add(createHandlersGlyph());

    // LLM Provider Glyph — provider selection (replaces ai-provider-window)
    glyphRun.add(createLlmProviderGlyph());

    log.debug(SEG.UI, 'Default glyphs registered:', {
        canvas: 'Spatial canvas grid',
        database: 'Database statistics',
        embeddings: 'Embedding service status',
        i: 'Who is looking',
        am: 'What the node is',
        usage: 'API usage and costs',
        plugins: 'Domain plugin panel',
        llm: 'LLM provider selection'
    });
}