/**
 * The default tray: every element the node opens with, added to the ElementRun.
 *
 * The tray is what we use. All palette functionality has been replaced by the
 * tray, fully — the symbol row that sat beside it is gone.
 */

import { tray } from '@teranos/elements';
import { createCanvasElement } from './components/element/canvas/canvas-element';
import { createChartElement } from './components/element/chart-element';
import { createDbElement } from './db-element';
import { createEmbeddingsElement } from './embeddings-element';
import { createSigmaPanel } from './sigma-panel';
import { createPluginElement } from './plugin-panel.ts';
import { createPulseElement } from './pulse-panel.ts';
import { createHandlersElement } from './handlers-panel.ts';
import { createLlmProviderElement } from './llm-provider-element.ts';
import { createTokensElement } from './tokens-element.ts';
import { createRolesElement } from './roles-element.ts';
import { createUsersElement } from './users-element.ts';
import { createMarketElement } from './market-element.ts';
import { createMailElement } from './mail-element.ts';
import { createIElement } from './i-element.ts';
import { createAmElement } from './am-element.ts';
import { log, SEG } from './logger.ts';

export { updateDatabaseStats, recordEviction } from './db-element';
export { updateSigmaPanel } from './sigma-panel';

// The build and the backends are what the node is, so ≡ holds them. The names
// the socket already calls stay, and forward.
export { updateAmVersion as updateSelfVersion, updateAmCapabilities as updateSelfCapabilities } from './am-element.ts';

// Register default system elements. A namespace with no canvas has no canvas
// element either: nothing of it is drawn, not even the dot in the tray.
export function registerDefaultElements(hasCanvas: boolean = true): void {
    // Canvas Element - Fractal container with spatial grid
    if (hasCanvas) tray.add(createCanvasElement());

    // Database Statistics Element
    tray.add(createDbElement());

    // Sigma Overview Panel
    tray.add(createSigmaPanel());

    // Embeddings Element
    tray.add(createEmbeddingsElement());

    // ⍟ — who is looking
    tray.add(createIElement());

    // ≡ — what the node is, and what it was told to be
    tray.add(createAmElement());

    // Access Tokens Element — opened from ⍟ (ADR-025)
    tray.add(createTokensElement());
    tray.add(createUsersElement());

    // Roles Element — every role the lines name, opened from ⍟ (ADR-034)
    tray.add(createRolesElement());

    // Stands Element — every stand across markets (ADR-035)
    tray.add(createMarketElement());

    // Mail Element — what the node mails on plugins' behalf, opened from ⍟ (ADR-041)
    tray.add(createMailElement());

    // Usage & Cost Chart Element
    // TODO(future): Budget alerting with notifications
    // Implement cost threshold monitoring with user notifications:
    // - Config: User-defined budget limits (daily/weekly/monthly)
    // - Detection: Check total cost vs. budget in chart render
    // - Notification: Toast alert when threshold crossed
    // - Persistence: Store alert state to avoid repeat notifications
    // - UX: Clear visual indication of budget status in chart
    tray.add(createChartElement(
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
    tray.add(createPulseElement());

    // Plugin Panel Element — panel manifestation
    tray.add(createPluginElement());

    // Handlers Panel Element — handler attestation management
    tray.add(createHandlersElement());

    // LLM Provider Element — provider selection (replaces ai-provider-window)
    tray.add(createLlmProviderElement());

    log.debug(SEG.UI, 'Default elements registered:', {
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