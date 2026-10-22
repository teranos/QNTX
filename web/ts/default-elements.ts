/**
 * The default tray: every element the node opens with, added to the ElementRun.
 *
 * The tray is what we use. All palette functionality has been replaced by the
 * tray, fully — the symbol row that sat beside it is gone.
 */

import { tray } from '@teranos/elements';
import { forTheViewer, askTheNode, type Audience } from './audience';
import { createCanvasElement, needs as canvasNeeds } from './components/element/canvas/canvas-element';
import { createDbElement, needs as dbNeeds } from './db-element';
import { createEmbeddingsElement, needs as embeddingsNeeds } from './embeddings-element';
import { createSigmaPanel, needs as sigmaNeeds } from './sigma-panel';
import { createPluginElement, needs as pluginNeeds } from './plugin-panel.ts';
import { createPulseElement, needs as pulseNeeds } from './pulse-panel.ts';
import { createHandlersElement, needs as handlersNeeds } from './handlers-panel.ts';
import { createLlmProviderElement, needs as llmNeeds } from './llm-provider-element.ts';
import { createTokensElement, needs as tokensNeeds } from './tokens-element.ts';
import { createRolesElement, needs as rolesNeeds } from './roles-element.ts';
import { createUsersElement, needs as usersNeeds } from './users-element.ts';
import { createMarketElement, needs as marketNeeds } from './market-element.ts';
import { createMailElement, needs as mailNeeds } from './mail-element.ts';
import { createGitHubElement, needs as githubNeeds } from './github-element.ts';
import { createApprovalsElement, needs as approvalsNeeds } from './approvals-element.ts';
import { createObsidianElement, needs as obsidianNeeds } from './obsidian-element.ts';
import { createGroundElement, needs as groundNeeds } from './ground-element.ts';
import { createClaudeElement, needs as claudeNeeds } from './claude-element.ts';
import { createPiElement, needs as piNeeds } from './pi-element.ts';
import { createAgentElement, needs as agentNeeds } from './agent-element.ts';
import { createIElement, needs as iNeeds } from './i-element.ts';
import { createAmElement, needs as amNeeds } from './am-element.ts';
import { createParityElement, needs as parityNeeds } from './parity-element.ts';
import { createDesignElement } from './design-element.ts';
import { log, SEG } from './logger.ts';

export { updateDatabaseStats, recordEviction } from './db-element';
export { updateSigmaPanel } from './sigma-panel';

// The build and the backends are what the node is, so ≡ holds them. The names
// the socket already calls stay, and forward.
export { updateAmVersion as updateSelfVersion, updateAmCapabilities as updateSelfCapabilities } from './am-element.ts';

// Register default system elements. A namespace with no canvas has no canvas
// element either: nothing of it is drawn, not even the dot in the tray.
// "the ui needs to stop suggesting things that arent possible, needs to stop handing out dead birds"
export async function registerDefaultElements(hasCanvas: boolean = true): Promise<void> {
    const offered: Audience[] = [
        // Canvas Element - Fractal container with spatial grid
        ...(hasCanvas ? [{ needs: canvasNeeds, create: createCanvasElement }] : []),
        // Database Statistics Element
        { needs: dbNeeds, create: createDbElement },
        // Sigma Overview Panel
        { needs: sigmaNeeds, create: createSigmaPanel },
        // Embeddings Element
        { needs: embeddingsNeeds, create: createEmbeddingsElement },
        // ⍟ — who is looking
        { needs: iNeeds, create: createIElement },
        // ≡ — what the node is, and what it was told to be
        { needs: amNeeds, create: createAmElement },
        // Access Tokens Element — opened from ⍟ (ADR-025)
        { needs: tokensNeeds, create: createTokensElement },
        { needs: usersNeeds, create: createUsersElement },
        // Roles Element — every role the lines name, opened from ⍟ (ADR-034)
        { needs: rolesNeeds, create: createRolesElement },
        // Stands Element — every stand across markets (ADR-035)
        { needs: marketNeeds, create: createMarketElement },
        // Mail Element — what the node mails on plugins' behalf, opened from ⍟ (ADR-041)
        { needs: mailNeeds, create: createMailElement },
        // GitHub Element — the node's GitHub and its Actions runner, opened from ⍟ (ADR-043)
        { needs: githubNeeds, create: createGitHubElement },
        // Approvals Element — what waits on the human, opened from ⍟ (ADR-052)
        { needs: approvalsNeeds, create: createApprovalsElement },
        // Obsidian Element — the vaults the node keeps, and the folders each holds (ADR-049)
        { needs: obsidianNeeds, create: createObsidianElement },
        // Ground Element — aware of anything Ground; the sessions it recorded, each opened as a Transcript
        { needs: groundNeeds, create: createGroundElement },
        // ✻ — the Node Agent: said to, and read as the session it continues (ADR-048)
        { needs: claudeNeeds, create: createClaudeElement },
        { needs: piNeeds, create: createPiElement },
        // ✦ — the agent of the namespace this person stands in, on its own sign-in (ADR-048)
        { needs: agentNeeds, create: createAgentElement },
        // Pulse Panel Element — scheduled jobs dashboard
        { needs: pulseNeeds, create: createPulseElement },
        // Plugin Panel Element — panel manifestation
        { needs: pluginNeeds, create: createPluginElement },
        // Handlers Panel Element — handler attestation management
        { needs: handlersNeeds, create: createHandlersElement },
        // ≍ — a signum held to a reference it follows, the seam in both sides' words
        { needs: parityNeeds, create: createParityElement },
        // LLM Provider Element — provider selection (replaces ai-provider-window)
        { needs: llmNeeds, create: createLlmProviderElement },
    ];

    const forThem = await forTheViewer(offered, askTheNode);
    for (const each of forThem) tray.add(each.create());

    // ◈ — what the page is drawn with. It asks the node nothing, so there is no path to ask
    // `i reaches` about: it is the page's, and whoever has the page has it.
    tray.add(createDesignElement());

    log.debug(SEG.UI, 'Default elements registered:', {
        offered: offered.length,
        added: forThem.length + 1,
    });
}
