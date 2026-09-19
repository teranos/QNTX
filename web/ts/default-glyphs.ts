/**
 * The default tray: every glyph the node opens with, added to the GlyphRun.
 *
 * The tray is what we use. All palette functionality has been replaced by the
 * tray, fully — the symbol row that sat beside it is gone.
 */

import { tray } from '@teranos/elements';
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
import { createRolesGlyph } from './roles-glyph.ts';
import { createUsersGlyph } from './users-glyph.ts';
import { createMarketGlyph } from './market-glyph.ts';
import { createIGlyph } from './i-glyph.ts';
import { createAmGlyph } from './am-glyph.ts';
import { log, SEG } from './logger.ts';

export { updateDatabaseStats, recordEviction } from './db-glyph';
export { updateSigmaPanel } from './sigma-panel';

// The build and the backends are what the node is, so ≡ holds them. The names
// the socket already calls stay, and forward.
export { updateAmVersion as updateSelfVersion, updateAmCapabilities as updateSelfCapabilities } from './am-glyph.ts';

// Register default system glyphs
export function registerDefaultGlyphs(): void {
    // Canvas Element - Fractal container with spatial grid
    tray.add(createCanvasGlyph());

    // Database Statistics Element
    tray.add(createDbGlyph());

    // Sigma Overview Panel
    tray.add(createSigmaPanel());

    // Embeddings Element
    tray.add(createEmbeddingsGlyph());

    // ⍟ — who is looking
    tray.add(createIGlyph());

    // ≡ — what the node is, and what it was told to be
    tray.add(createAmGlyph());

    // Access Tokens Element — opened from ⍟ (ADR-025)
    tray.add(createTokensGlyph());
    tray.add(createUsersGlyph());

    // Roles Element — every role the lines name, opened from ⍟ (ADR-034)
    tray.add(createRolesGlyph());

    // Stands Element — every stand across markets (ADR-035)
    tray.add(createMarketGlyph());

    // Usage & Cost Chart Element
    // TODO(future): Budget alerting with notifications
    // Implement cost threshold monitoring with user notifications:
    // - Config: User-defined budget limits (daily/weekly/monthly)
    // - Detection: Check total cost vs. budget in chart render
    // - Notification: Toast alert when threshold crossed
    // - Persistence: Store alert state to avoid repeat notifications
    // - UX: Clear visual indication of budget status in chart
    tray.add(createChartGlyph(
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

    // Pulse Panel Element — scheduled jobs dashboard
    tray.add(createPulseGlyph());

    // Plugin Panel Element — panel manifestation
    tray.add(createPluginGlyph());

    // Handlers Panel Element — handler attestation management
    tray.add(createHandlersGlyph());

    // LLM Provider Element — provider selection (replaces ai-provider-window)
    tray.add(createLlmProviderGlyph());

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