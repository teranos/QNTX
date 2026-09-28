/**
 * Plugin Panel - Shows installed domain plugins and their status
 *
 * Manifests as an element with 'panel' opensAs — slides in from
 * the opposite edge of the system drawer.
 *
 * Where a plugin is added, configured and enabled: the empty card at the end of
 * the list takes a repository URL, Check says whether it resolves, Add commits
 * it, and the plugin starts disabled.
 *
 * Displays plugin information:
 * - Lists all installed plugins with metadata
 * - Shows health status for each plugin
 * - Color-coded status indicators
 *
 * Uses /api/plugins endpoint from server/handlers.go
 */

import { apiFetch, apiJson, backendPath } from './client';
import { assertOk, jsonBody } from './http-utils.ts';
import { escapeHtml, formatBuildTime } from './html-utils.ts';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';
import { buttonPlaceholder, hydrateButtons, registerButton, type HydrateConfig } from './components/button';
import { tooltip } from './components/tooltip.ts';
import { refusal } from './self-person.ts';
import type { Element } from '@teranos/elements';

interface PluginInfo {
    name: string;
    version: string;
    qntx_version?: string;
    description: string;
    author?: string;
    license?: string;
    healthy: boolean;
    /** Whether the last probe saw it. One that started after the probe is not unhealthy. */
    probed?: boolean;
    message?: string;
    details?: Record<string, unknown>;
    state: 'running' | 'paused' | 'stopped' | 'loading' | 'failed' | 'restarting' | 'disabled';
    pausable: boolean;
    sigils?: SigilRow[];
    signa_refused?: string[];
    /** The repository it was added from. */
    repo?: string;
    /** Whether its record has it switched on. */
    enabled?: boolean;
}

/** One sigil a plugin handed the node (ADR-039), as server/plugin_sigils.go sends it. */
interface SigilRow {
    signum: string;
    sigil: string;
    tool: string;
    method: string;
    path: string;
    does: string;
    takes: { name: string; says: string; required?: boolean; one_of?: string[] }[];
    gives: { name: string; says: string }[];
    reach: Record<'http' | 'mcp', Reached>;
}

/** Who the lines say reaches a sigil over one surface. ROOT is never listed. */
interface Reached {
    anyone: boolean;
    levels: string[];
    roles: string[];
}

interface PluginsResponse {
    plugins: PluginInfo[];
    /** Why the node could not read which plugins it holds, when it could not. */
    records_failure?: string;
}

interface ConfigFieldSchema {
    type: 'string' | 'number' | 'boolean' | 'array';
    description: string;
    default_value: string;
    required: boolean;
    min_value?: string;
    max_value?: string;
    pattern?: string;
    element_type?: string;
}

interface PluginConfigResponse {
    plugin: string;
    config: Record<string, string>;
    schema: Record<string, ConfigFieldSchema> | null;
}

/** The node's error envelope (server/error_envelope.go). */
interface ErrorResponse {
    error: string;
    details?: string[];
}

interface ConfigFormState {
    pluginName: string;
    currentConfig: Record<string, string>;
    newConfig: Record<string, string>;
    schema: Record<string, ConfigFieldSchema>;
    validationErrors: Record<string, string>;
    needsConfirmation: boolean;
    editingFields: Set<string>;
    /** No schema came with the config: the plugin is not running, so keys are written as typed. */
    freeForm: boolean;
    error?: { message: string; details: string; status: number };
}

interface ServerHealth {
    status: string;
    version: string;
    commit: string;
    build_time: string;
    clients: number;
    verbosity: number;
    owner: string;
}

// Module-level state
let plugins: PluginInfo[] = [];
let expandedPlugin: string | null = null;
let configState: ConfigFormState | null = null;
let serverHealth: ServerHealth | null = null;
let lastRefreshed: Date | null = null;

// The content element provided by renderContent()
let contentElement: HTMLElement | null = null;

// Tooltip cleanup function
let tooltipCleanup: (() => void) | null = null;

// Log stream state
let activeLogStream: EventSource | null = null;

// What the node said to the last grant from a sigil row, by signum:sigil, so the
// answer lands on the row it was asked from.
const grantSaid: Record<string, { ok: boolean; text: string }> = {};

// The empty card in progress: whether it is open, what is typed in it, and what
// the node said the typed repo resolves to. Adding is two stages: Check, then Add.
let adding = false;
let addingRepo = '';
let resolved: ResolvedPlugin | null = null;

/** What GitHub said of a repo, as plugins:check answers (server/plugin_check.go). */
interface ResolvedPlugin {
    name: string;
    repo: string;
    repository: string;
    private: boolean;
    ref: string;
    path?: string;
}

// Added and switched off.
const NOT_RUNNING = ['disabled'];

// Why the last list is not what the node holds; empty when it is.
let listFailure = '';

async function fetchServerHealth(): Promise<void> {
    try {
        serverHealth = await apiJson<ServerHealth>('/health');
    } catch (error: unknown) {
        handleError(error, 'Failed to fetch server health', { context: SEG.UI, silent: true });
        serverHealth = null;
    }
}

async function fetchPlugins(): Promise<void> {
    try {
        log.debug(SEG.UI, 'Fetching plugins from /api/plugins...');
        const data = await apiJson<PluginsResponse>('/api/plugins');

        if (!data || !Array.isArray(data.plugins)) {
            throw new Error('Invalid plugins response: missing plugins array');
        }

        plugins = data.plugins;
        listFailure = data.records_failure ?? '';
        lastRefreshed = new Date();
        log.debug(SEG.UI, 'Successfully loaded', plugins.length, 'plugins');
    } catch (error: unknown) {
        handleError(error, 'Failed to fetch plugins', { context: SEG.UI, silent: true });
        plugins = [];
        listFailure = error instanceof Error ? error.message : String(error);
    }
}

/** Why the list is not what the node holds, in full, where the list is. */
function renderListFailure(): string {
    if (!listFailure) return '';
    return `<div class="plugin-list-failure">${escapeHtml(listFailure)}</div>`;
}

function render(): void {
    if (!contentElement) return;
    const typing = contentElement.querySelector('.plugin-add-repo') === document.activeElement;

    if (plugins.length === 0) {
        // Server unreachable — both fetches failed
        if (serverHealth === null) {
            contentElement.innerHTML = `
                <div class="element-content plugin-offline">
                    <div class="plugin-offline-message">
                        <div class="plugin-offline-title">Server offline</div>
                        <p>gRPC plugins require a running QNTX server</p>
                        <div class="plugin-offline-roadmap">WASM plugins are on the roadmap</div>
                    </div>
                </div>
            `;
            return;
        }

        contentElement.innerHTML = `
            <div class="element-content plugin-panel-body">
                ${renderToolbar()}
                ${renderListFailure()}
                <div class="plugin-list">
                    ${listFailure ? '' : renderAddCard()}
                </div>
            </div>
        `;
        hydratePluginButtons(contentElement);
        refreshTooltips();
        if (typing) focusAddField();
        return;
    }

    const serverBuildTime = formatBuildTime(serverHealth?.build_time);

    contentElement.innerHTML = `
        <div class="element-content plugin-panel-body">
            ${renderToolbar()}
            ${renderListFailure()}
            <div class="plugin-summary">
                <div class="plugin-summary-stats">
                    <span class="plugin-count">${plugins.length} plugin${plugins.length !== 1 ? 's' : ''} installed</span>
                    <span class="plugin-health-summary">${getHealthSummary()}</span>
                    ${lastRefreshed ? `<span class="plugin-last-refreshed plugin-mono">${formatTime(lastRefreshed)}</span>` : ''}
                </div>
                ${serverBuildTime ? `
                    <div class="plugin-server-info">
                        <span class="plugin-server-label">QNTX Server Built:</span>
                        <span class="plugin-server-value plugin-mono">${serverBuildTime}</span>
                    </div>
                ` : ''}

            </div>
            <div class="plugin-list">
                ${plugins.map(plugin => renderPlugin(plugin)).join('')}
                ${renderAddCard()}
            </div>
        </div>
    `;

    // Hydrate plugin control buttons
    hydratePluginButtons(contentElement);

    // Rebind tooltips for new DOM content
    refreshTooltips();
    if (typing) focusAddField();
}

function renderToolbar(): string {
    return `
        <div class="plugin-search-container">
            <input type="text" class="plugin-search-input plugin-mono" placeholder="Filter plugins...">
        </div>
    `;
}

/**
 * The empty plugin at the end of the list, ready to become one: a repository
 * URL, checked by the node, then added for real.
 */
export function renderAddCard(): string {
    if (!adding) {
        return `<div class="plugin-add-card plugin-add-card-closed" role="button" tabindex="0" aria-label="Add a plugin by its repository URL">+</div>`;
    }
    const checked = resolved !== null && resolved.repo === addingRepo.trim();
    return `
        <div class="plugin-add-card plugin-add-card-open">
            <input type="text" class="plugin-add-repo plugin-mono" placeholder="https://github.com/owner/repo/tree/main/plugin" value="${escapeHtml(addingRepo)}" autocomplete="off" spellcheck="false">
            ${checked && resolved ? `
            <div class="plugin-add-resolved plugin-mono">
                <span class="plugin-name">${escapeHtml(resolved.name)}</span>
                <span>${escapeHtml(resolved.repository)}${resolved.path ? `/${escapeHtml(resolved.path)}` : ''}</span>
                <span>${escapeHtml(resolved.ref)}</span>
                ${resolved.private ? '<span>private</span>' : ''}
            </div>` : ''}
            <div class="plugin-controls">
                ${checked
                    ? buttonPlaceholder('plugin-add-confirm', 'Add', 'plugin-add-confirm')
                    : buttonPlaceholder('plugin-add-check', 'Check', 'plugin-add-check')}
                ${buttonPlaceholder('plugin-add-cancel', 'Cancel', 'plugin-add-cancel')}
            </div>
        </div>
    `;
}

function closeAddCard(): void {
    adding = false;
    addingRepo = '';
    resolved = null;
}

// Opening the field puts the cursor in it, and a refresh while typing keeps it there.
function focusAddField(): void {
    if (!adding) return;
    contentElement?.querySelector<HTMLInputElement>('.plugin-add-repo')?.focus();
}

/** Stage one: whether GitHub has the repo, asked as the node. A refusal is the button's to show. */
async function checkPlugin(repo: string): Promise<void> {
    if (repo === '') throw new Error('type the repository URL, then press Check');
    const response = await apiFetch('/api/plugins/check', jsonBody('POST', { repo }));
    if (!response.ok) throw new Error(await refusal(response));
    resolved = await response.json() as ResolvedPlugin;
    render();
}

/** Stage two: add the checked plugin for real. It starts disabled; a refusal is the button's to show. */
async function addPlugin(repo: string): Promise<void> {
    const response = await apiFetch('/api/plugins', jsonBody('POST', { repo }));
    if (!response.ok) throw new Error(await refusal(response));
    closeAddCard();
    await fetchPlugins();
    render();
}

/** Switch a plugin on or off in its record. A plugin enabled and not started shows why in the list. */
async function switchPlugin(name: string, verb: 'enable' | 'disable'): Promise<void> {
    const response = await apiFetch(`/api/plugins/${encodeURIComponent(name)}/${verb}`, { method: 'POST' });
    const refused = response.ok ? '' : await refusal(response);
    await fetchPlugins();
    render();
    if (refused) throw new Error(refused);
}

function refreshTooltips(): void {
    if (!contentElement) return;
    if (tooltipCleanup) {
        tooltipCleanup();
    }
    tooltipCleanup = tooltip.attach(contentElement, '.has-tooltip');
}

function attachEventDelegation(): void {
    if (!contentElement) return;

    // Click delegation — attached once, works with dynamic content via .closest()
    contentElement.addEventListener('click', async (e: Event) => {
        const target = e.target as HTMLElement;

        // Grant a sigil to the role typed beside it
        const grantBtn = target.closest('.plugin-sigil-grant-btn') as HTMLElement | null;
        if (grantBtn) {
            e.stopPropagation();
            const key = grantBtn.dataset.sigil ?? '';
            const input = grantBtn.parentElement?.querySelector<HTMLInputElement>('.plugin-sigil-grant-role');
            await grantSigil(key, input?.value.trim() ?? '');
            return;
        }

        // Save config button
        if (target.closest('.plugin-config-save-btn')) {
            e.stopPropagation();
            await savePluginConfig();
            return;
        }

        // Cancel config button
        if (target.closest('.plugin-config-cancel-btn')) {
            e.stopPropagation();
            expandedPlugin = null;
            configState = null;
            disconnectLogStream();
            render();
            return;
        }

        // Click on value display to edit
        const valueDisplay = target.closest('.plugin-config-value-display') as HTMLElement | null;
        if (valueDisplay) {
            e.stopPropagation();
            const fieldName = valueDisplay.dataset.field;
            if (fieldName && configState) {
                configState.editingFields.add(fieldName);
                render();
                setTimeout(() => {
                    const input = contentElement?.querySelector<HTMLInputElement>(`.plugin-config-value-new[data-field="${fieldName}"]`);
                    input?.focus();
                }, 0);
            }
            return;
        }

        // Cancel field edit button
        const cancelFieldBtn = target.closest('.plugin-config-field-cancel') as HTMLElement | null;
        if (cancelFieldBtn) {
            e.stopPropagation();
            const fieldName = cancelFieldBtn.dataset.field;
            if (fieldName && configState) {
                if (configState.freeForm && !(fieldName in configState.currentConfig)) {
                    delete configState.newConfig[fieldName];
                } else {
                    configState.newConfig[fieldName] = configState.currentConfig[fieldName] || configState.schema[fieldName]?.default_value || '';
                }
                configState.editingFields.delete(fieldName);
                delete configState.validationErrors[fieldName];
                render();
            }
            return;
        }

        // Remove a key from a config written as typed
        const removeKeyBtn = target.closest('.plugin-config-key-remove') as HTMLElement | null;
        if (removeKeyBtn) {
            e.stopPropagation();
            const fieldName = removeKeyBtn.dataset.field;
            if (fieldName && configState) {
                delete configState.newConfig[fieldName];
                configState.editingFields.delete(fieldName);
                delete configState.validationErrors[fieldName];
                configState.needsConfirmation = false;
                render();
            }
            return;
        }

        // Add a key to a config written as typed
        if (target.closest('.plugin-config-key-add')) {
            e.stopPropagation();
            addConfigKey();
            return;
        }

        // The empty card opens into the repository field
        if (target.closest('.plugin-add-card-closed')) {
            e.stopPropagation();
            adding = true;
            render();
            focusAddField();
            return;
        }

        // Plugin card click - toggle config expansion
        const card = target.closest('.plugin-card') as HTMLElement | null;
        if (card && !target.closest('button') && !target.closest('input') && !target.closest('a')) {
            const pluginName = card.dataset.plugin;
            if (pluginName) {
                await togglePluginConfig(pluginName);
            }
            return;
        }
    });

    // Config input change handlers
    contentElement.addEventListener('input', (e: Event) => {
        const target = e.target as HTMLInputElement;
        if (target.classList.contains('plugin-config-value-new')) {
            const fieldName = target.dataset.field;
            if (fieldName && configState) {
                configState.newConfig[fieldName] = target.value;
                configState.needsConfirmation = false;
                validateField(fieldName, target.value);
                updateSaveButtonState();
            }
        }

        // Search input filtering
        if (target.classList.contains('plugin-search-input')) {
            filterPlugins(target.value);
        }

        // A repo typed after Check is not the one checked: back to Check.
        if (target.classList.contains('plugin-add-repo')) {
            const wasChecked = resolved !== null && resolved.repo === addingRepo.trim();
            addingRepo = target.value;
            if (wasChecked && resolved?.repo !== addingRepo.trim()) {
                resolved = null;
                render();
            }
        }
    });

    contentElement.addEventListener('keydown', (e: KeyboardEvent) => {
        const target = e.target as HTMLInputElement;
        if (target.classList.contains('plugin-add-repo') && e.key === 'Escape') {
            closeAddCard();
            render();
        }
        if (target.classList.contains('plugin-add-card-closed') && (e.key === 'Enter' || e.key === ' ')) {
            e.preventDefault();
            adding = true;
            render();
            focusAddField();
        }
        if (target.classList.contains('plugin-config-new-key') && e.key === 'Enter') {
            addConfigKey();
        }
    });
}

/** A key typed in the add row becomes a row of its own, open for its value. */
function addConfigKey(): void {
    if (!configState) return;
    const field = contentElement?.querySelector<HTMLInputElement>('.plugin-config-new-key');
    const key = field?.value.trim() ?? '';
    if (key === '') {
        field?.focus();
        return;
    }
    if (!(key in configState.newConfig)) configState.newConfig[key] = '';
    configState.editingFields.add(key);
    configState.needsConfirmation = false;
    render();
    const inputs = contentElement?.querySelectorAll<HTMLInputElement>('.plugin-config-value-new') ?? [];
    Array.from(inputs).find(input => input.dataset.field === key)?.focus();
}

function hydratePluginButtons(container: HTMLElement): void {
    const config: HydrateConfig = {};

    for (const plugin of plugins) {
        if (plugin.pausable) {
            if (plugin.state === 'running') {
                config[`plugin-pause-${plugin.name}`] = {
                    label: '\u275A\u275A Pause',
                    onClick: async () => {
                        await pausePlugin(plugin.name);
                    },
                    variant: 'secondary',
                    size: 'small'
                };
            } else if (plugin.state === 'paused') {
                config[`plugin-resume-${plugin.name}`] = {
                    label: '\u25B6 Resume',
                    onClick: async () => {
                        await resumePlugin(plugin.name);
                    },
                    variant: 'primary',
                    size: 'small'
                };
            }
        }

        if (plugin.state === 'running') {
            config[`plugin-restart-${plugin.name}`] = {
                label: 'Restart',
                onClick: async () => {
                    await restartPlugin(plugin.name);
                },
                variant: 'ghost',
                size: 'small',
                confirmation: {
                    label: 'Confirm'
                }
            };
        }

        if (enables(plugin)) {
            config[`plugin-enable-${plugin.name}`] = {
                label: 'Enable',
                onClick: async () => {
                    await switchPlugin(plugin.name, 'enable');
                },
                variant: 'primary',
                size: 'small'
            };
        }

        if (disables(plugin)) {
            config[`plugin-disable-${plugin.name}`] = {
                label: 'Disable',
                onClick: async () => {
                    await switchPlugin(plugin.name, 'disable');
                },
                variant: 'ghost',
                size: 'small',
                confirmation: {
                    label: 'Confirm'
                }
            };
        }
    }

    if (adding) {
        config['plugin-add-check'] = {
            label: 'Check',
            onClick: async () => {
                const typed = container.querySelector<HTMLInputElement>('.plugin-add-repo')?.value.trim() ?? '';
                await checkPlugin(typed);
            },
            variant: 'secondary',
            size: 'small'
        };
        config['plugin-add-confirm'] = {
            label: 'Add',
            onClick: async () => {
                if (resolved) await addPlugin(resolved.repo);
            },
            variant: 'primary',
            size: 'small'
        };
        config['plugin-add-cancel'] = {
            label: 'Cancel',
            onClick: () => {
                closeAddCard();
                render();
            },
            variant: 'ghost',
            size: 'small'
        };
    }

    const buttons = hydrateButtons(container, config);

    for (const [buttonId, button] of Object.entries(buttons)) {
        registerButton(buttonId, button);
    }
}

/** Switched off, stopped, or failed. */
export function enables(plugin: PluginInfo): boolean {
    return ['disabled', 'stopped', 'failed'].includes(plugin.state);
}

/** Switched on: running, paused, or failed. */
export function disables(plugin: PluginInfo): boolean {
    return ['running', 'paused', 'failed'].includes(plugin.state);
}

function getHealthSummary(): string {
    // A plugin that is not running has no health to count.
    const probed = plugins.filter(p => !NOT_RUNNING.includes(p.state) && p.probed !== false);
    const unhealthy = probed.filter(p => !p.healthy).length;

    if (unhealthy === 0) {
        return '<span class="plugin-health-good">All healthy</span>';
    }
    return `<span class="plugin-health-warning">${unhealthy} unhealthy</span>`;
}

function formatTime(date: Date): string {
    const hh = String(date.getHours()).padStart(2, '0');
    const mm = String(date.getMinutes()).padStart(2, '0');
    const ss = String(date.getSeconds()).padStart(2, '0');
    return `${hh}:${mm}:${ss}`;
}

// Re-exported for plugin-panel.test.ts
export { formatBuildTime } from './html-utils.ts';

function buildVersionTooltip(plugin: PluginInfo): string {
    const parts: string[] = [];

    if (plugin.author) parts.push(`Author: ${plugin.author}`);
    if (plugin.license) parts.push(`License: ${plugin.license}`);
    if (plugin.qntx_version) parts.push(`QNTX Version: \u2265${plugin.qntx_version}`);

    if (parts.length > 0 && plugin.details && Object.keys(plugin.details).length > 0) {
        parts.push('---');
    }

    if (plugin.details) {
        Object.entries(plugin.details).forEach(([key, value]) => {
            let displayValue: string;

            if (key === 'binary_built' && typeof value === 'string') {
                const timestamp = parseInt(value, 10);
                if (!isNaN(timestamp)) {
                    const date = new Date(timestamp * 1000);
                    displayValue = date.toLocaleString();
                } else {
                    displayValue = String(value);
                }
            } else {
                displayValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
            }

            parts.push(`${key}: ${displayValue}`);
        });
    }

    return parts.join('\n');
}

function renderPlugin(plugin: PluginInfo): string {
    const unprobed = plugin.probed === false;
    const statusClass = unprobed ? 'plugin-status-unprobed' : plugin.healthy ? 'plugin-status-healthy' : 'plugin-status-unhealthy';
    const statusIcon = unprobed ? '&#8230;' : plugin.healthy ? '&#10003;' : '&#10007;';
    const statusText = unprobed ? 'Not probed' : plugin.healthy ? 'Healthy' : 'Unhealthy';
    const isExpanded = expandedPlugin === plugin.name;

    const versionTooltip = buildVersionTooltip(plugin);
    const nameTooltip = [
        plugin.description || 'No description available',
        '---',
        `Repo: ${plugin.repo || 'not added from a repository'}`
    ].join('\n');

    const stateClass = getStateClass(plugin.state);
    const stateIcon = getStateIcon(plugin.state);

    let controls = '';
    if (plugin.pausable) {
        if (plugin.state === 'running') {
            controls = buttonPlaceholder(`plugin-pause-${plugin.name}`, '\u275A\u275A Pause', 'plugin-pause-btn');
        } else if (plugin.state === 'paused') {
            controls = buttonPlaceholder(`plugin-resume-${plugin.name}`, '\u25B6 Resume', 'plugin-resume-btn');
        }
    }

    if (enables(plugin)) {
        controls += buttonPlaceholder(`plugin-enable-${plugin.name}`, 'Enable', 'plugin-enable-btn');
    }
    if (disables(plugin)) {
        controls += buttonPlaceholder(`plugin-disable-${plugin.name}`, 'Disable', 'plugin-disable-btn');
    }

    let restartBtn = '';
    if (plugin.state === 'running') {
        restartBtn = buttonPlaceholder(`plugin-restart-${plugin.name}`, 'Restart', 'plugin-restart-btn');
    }

    const notRunning = NOT_RUNNING.includes(plugin.state);

    return `
        <div class="plugin-card ${isExpanded ? 'plugin-card-expanded' : ''}" data-plugin="${escapeHtml(plugin.name)}">
            <div class="plugin-card-header">
                <div class="plugin-name-row">
                    <span class="plugin-name has-tooltip" data-tooltip="${escapeHtml(nameTooltip)}">${escapeHtml(plugin.name)}</span>
                    ${plugin.version ? `<span class="plugin-version has-tooltip plugin-mono" data-tooltip="${escapeHtml(versionTooltip)}">${escapeHtml(plugin.version)}</span>` : ''}
                </div>
                <div class="plugin-badges">
                    <div class="plugin-state ${stateClass}">
                        <span class="plugin-state-icon">${stateIcon}</span>
                        <span class="plugin-state-text">${plugin.state}</span>
                    </div>
                    ${notRunning ? '' : `
                    <div class="plugin-status ${statusClass}">
                        <span class="plugin-status-icon">${statusIcon}</span>
                        <span class="plugin-status-text">${statusText}</span>
                    </div>`}
                    ${renderSigilBadges(plugin)}
                    ${restartBtn}
                </div>
            </div>
            ${plugin.repo ? `<a class="plugin-repo plugin-mono" href="${escapeHtml(plugin.repo)}" target="_blank" rel="noopener noreferrer">${escapeHtml(plugin.repo)}</a>` : ''}
            ${controls ? `<div class="plugin-controls">${controls}</div>` : ''}
            ${renderPluginMessage(plugin)}
            ${isExpanded ? renderExpandedContent(plugin) : ''}
        </div>
    `;
}

/** Why a plugin is not healthy. */
export function renderPluginMessage(plugin: PluginInfo): string {
    if (!plugin.healthy && plugin.message) {
        return `<div class="plugin-message plugin-message-error">${escapeHtml(plugin.message)}</div>`;
    }
    return '';
}

function getStateClass(state: string): string {
    switch (state) {
        case 'running': return 'plugin-state-running';
        case 'paused': return 'plugin-state-paused';
        case 'stopped': return 'plugin-state-stopped';
        case 'restarting': return 'plugin-state-restarting';
        case 'disabled': return 'plugin-state-disabled';
        default: return '';
    }
}

function getStateIcon(state: string): string {
    switch (state) {
        case 'running': return '&#9654;';
        case 'paused': return '&#10074;&#10074;';
        case 'stopped': return '&#9632;';
        case 'restarting': return '&#8635;';
        case 'disabled': return '&#9675;';
        default: return '';
    }
}

async function pausePlugin(name: string): Promise<void> {
    try {
        log.debug(SEG.UI, 'Pausing plugin:', name);
        const response = await apiFetch(`/api/plugins/${name}/pause`, { method: 'POST' });
        await assertOk(response, `Failed to pause ${name}`);

        await fetchPlugins();
        render();
        log.debug(SEG.UI, 'Plugin paused:', name);
    } catch (error: unknown) {
        handleError(error, 'Failed to pause plugin', { context: SEG.UI });
    }
}

async function resumePlugin(name: string): Promise<void> {
    try {
        log.debug(SEG.UI, 'Resuming plugin:', name);
        const response = await apiFetch(`/api/plugins/${name}/resume`, { method: 'POST' });
        await assertOk(response, `Failed to resume ${name}`);

        await fetchPlugins();
        render();
        log.debug(SEG.UI, 'Plugin resumed:', name);
    } catch (error: unknown) {
        handleError(error, 'Failed to resume plugin', { context: SEG.UI });
    }
}

async function restartPlugin(name: string): Promise<void> {
    log.debug(SEG.UI, 'Restarting plugin:', name);
    const response = await apiFetch(`/api/plugins/${name}/restart`, { method: 'POST' });
    await assertOk(response, `Failed to restart ${name}`);

    await fetchPlugins();
    render();
    log.debug(SEG.UI, 'Plugin restarted:', name);
}

async function togglePluginConfig(pluginName: string): Promise<void> {
    if (expandedPlugin === pluginName) {
        expandedPlugin = null;
        configState = null;
        disconnectLogStream();
    } else {
        expandedPlugin = pluginName;
        await fetchPluginConfig(pluginName);
    }
    render();

    // Connect log stream after render so the container DOM exists
    if (expandedPlugin) {
        connectLogStream(expandedPlugin);
    }
}

async function fetchPluginConfig(pluginName: string): Promise<void> {
    try {
        const response = await apiFetch(`/api/plugins/${pluginName}/config`);
        if (!response.ok) {
            try {
                const errorData: ErrorResponse = await response.json();
                configState = {
                    pluginName,
                    currentConfig: {},
                    newConfig: {},
                    schema: {},
                    validationErrors: {},
                    needsConfirmation: false,
                    editingFields: new Set(),
                    freeForm: false,
                    error: { message: errorData.error, details: (errorData.details ?? []).join('\n'), status: response.status }
                };
            } catch (jsonUnreadable) {
                // The raw text below is the fallback; nothing is dropped.
                const errorText = await response.text();
                configState = {
                    pluginName,
                    currentConfig: {},
                    newConfig: {},
                    schema: {},
                    validationErrors: {},
                    needsConfirmation: false,
                    editingFields: new Set(),
                    freeForm: false,
                    error: { message: errorText || response.statusText, details: '', status: response.status }
                };
            }
            render();
            return;
        }

        const data: PluginConfigResponse = await response.json();

        configState = {
            pluginName,
            currentConfig: { ...data.config },
            newConfig: { ...data.config },
            schema: data.schema || {},
            validationErrors: {},
            needsConfirmation: false,
            editingFields: new Set(),
            freeForm: data.schema === null
        };
    } catch (error: unknown) {
        handleError(error, `Failed to fetch config for ${pluginName}`, { context: SEG.UI, silent: true });
        configState = {
            pluginName,
            currentConfig: {},
            newConfig: {},
            schema: {},
            validationErrors: {},
            needsConfirmation: false,
            editingFields: new Set(),
            freeForm: false,
            error: { message: `Failed to load configuration: ${error}`, details: '', status: 0 }
        };
        render();
    }
}

/** How many sigils the plugin serves, and whether it handed one the node refused. */
export function renderSigilBadges(plugin: PluginInfo): string {
    const sigils = plugin.sigils ?? [];
    const refused = plugin.signa_refused ?? [];
    let badges = '';
    if (sigils.length > 0) {
        badges += `<span class="plugin-sigil-count plugin-mono">${sigils.length} sigil${sigils.length !== 1 ? 's' : ''}</span>`;
    }
    if (refused.length > 0) {
        badges += `<span class="plugin-sigil-refused has-tooltip" data-tooltip="${escapeHtml(refused.join('\n'))}">signum refused</span>`;
    }
    return badges;
}

/** Who reaches a sigil over one surface, in words. ROOT reaches everything. */
export function reachedInWords(who: Reached): string {
    if (who.anyone) return 'anyone';
    const named = [...who.levels, ...who.roles];
    return named.length === 0 ? 'ROOT only' : `ROOT, ${named.join(', ')}`;
}

/** What the plugin does, a row per sigil, each with who reaches it and a way to grant it. */
export function renderSigils(plugin: PluginInfo): string {
    const sigils = plugin.sigils ?? [];
    const refused = plugin.signa_refused ?? [];
    if (sigils.length === 0 && refused.length === 0) return '';

    const rows = sigils.map(row => {
        const key = `${row.signum}:${row.sigil}`;
        const takes = row.takes.map(param => {
            const says = param.one_of && param.one_of.length > 0
                ? `${param.says}\nOne of: ${param.one_of.join(', ')}`
                : param.says;
            return `<span class="plugin-sigil-param has-tooltip ${param.required ? 'plugin-sigil-param-required' : ''}" data-tooltip="${escapeHtml(says)}">${escapeHtml(param.name)}${param.required ? '*' : ''}</span>`;
        }).join('');
        const said = grantSaid[key];
        return `
            <div class="plugin-sigil-row" data-sigil="${escapeHtml(key)}">
                <div class="plugin-sigil-head">
                    <span class="plugin-sigil-tool plugin-mono">${escapeHtml(row.tool)}</span>
                    <span class="plugin-sigil-endpoint plugin-mono">${escapeHtml(row.method)} ${escapeHtml(row.path)}</span>
                </div>
                <div class="plugin-sigil-does">${escapeHtml(row.does)}</div>
                ${takes ? `<div class="plugin-sigil-takes">${takes}</div>` : ''}
                <div class="plugin-sigil-reach plugin-mono">
                    <span>http: ${escapeHtml(reachedInWords(row.reach.http))}</span>
                    <span>mcp: ${escapeHtml(reachedInWords(row.reach.mcp))}</span>
                </div>
                <div class="plugin-sigil-grant">
                    <input type="text" class="plugin-sigil-grant-role plugin-mono" data-sigil="${escapeHtml(key)}" placeholder="role">
                    <button class="plugin-sigil-grant-btn" data-sigil="${escapeHtml(key)}">Grant</button>
                </div>
                ${said ? `<div class="plugin-sigil-said ${said.ok ? '' : 'plugin-sigil-said-error'}">${escapeHtml(said.text)}</div>` : ''}
            </div>
        `;
    }).join('');

    return `
        <div class="plugin-sigils">
            <div class="plugin-log-header">Sigils</div>
            ${refused.map(why => `<div class="plugin-message plugin-message-error">${escapeHtml(why)}</div>`).join('')}
            ${rows}
        </div>
    `;
}

/**
 * Grant one sigil to a role by writing a reach line (the reach signum's grant).
 * What the node answered is shown on the row, whatever it was.
 */
async function grantSigil(key: string, role: string): Promise<void> {
    if (!role) {
        grantSaid[key] = { ok: false, text: 'Name the role to grant it to' };
        render();
        return;
    }
    try {
        const response = await apiFetch('/api/reach', jsonBody('POST', { path: key, to: role }));
        if (!response.ok) {
            const body = await response.text();
            grantSaid[key] = { ok: false, text: `${response.status}: ${body}` };
        } else {
            grantSaid[key] = { ok: true, text: `Granted to ${role}` };
            await fetchPlugins();
        }
    } catch (error: unknown) {
        handleError(error, `Failed to grant ${key}`, { context: SEG.UI, silent: true });
        grantSaid[key] = { ok: false, text: `The grant did not reach the node: ${error}` };
    }
    render();
}

function renderExpandedContent(plugin: PluginInfo): string {
    return `
        ${renderSigils(plugin)}
        <div class="plugin-log-viewer">
            <div class="plugin-log-header">Activity Log</div>
            <div class="plugin-log-container" data-plugin="${escapeHtml(plugin.name)}"></div>
        </div>
        ${renderConfigForm()}
    `;
}

function renderConfigForm(): string {
    if (!configState) return '';

    if (configState.error) {
        const errorTitle = configState.error.status >= 500 ? 'Internal Server Error' : 'Error';

        return `
            <div class="panel-error">
                <div class="panel-error-title">${errorTitle}</div>
                <div class="panel-error-message">${escapeHtml(configState.error.message)}</div>
                ${configState.error.details ? `
                    <div class="plugin-config-error-details">
                        <div class="panel-error-details-header">Error Details</div>
                        <pre>${escapeHtml(configState.error.details)}</pre>
                    </div>
                ` : ''}
            </div>
        `;
    }

    const state = configState;
    const freeForm = state.freeForm;
    const fieldNames = freeForm ? Object.keys(state.newConfig).sort() : Object.keys(state.schema);

    const fields = fieldNames.map(fieldName => {
        const schema: ConfigFieldSchema | undefined = state.schema[fieldName];
        const defaultValue = schema?.default_value ?? '';
        const currentValue = state.currentConfig[fieldName] || defaultValue;
        const newValue = state.newConfig[fieldName] || defaultValue;
        const error = state.validationErrors[fieldName];
        const hasChanged = currentValue !== newValue || (freeForm && !(fieldName in state.currentConfig));
        const isEditing = state.editingFields.has(fieldName);
        const removeBtn = freeForm
            ? `<button class="plugin-config-key-remove has-tooltip" data-field="${escapeHtml(fieldName)}" data-tooltip="Remove this key">&#8722;</button>`
            : '';

        let valueCellContent: string;
        if (isEditing) {
            valueCellContent = `
                <div class="plugin-config-edit-container">
                    <input type="${getInputType(schema?.type ?? 'string')}"
                           value="${escapeHtml(newValue)}"
                           data-field="${escapeHtml(fieldName)}"
                           class="plugin-config-value-new plugin-mono"
                           ${schema?.min_value ? `min="${escapeHtml(schema.min_value)}"` : ''}
                           ${schema?.max_value ? `max="${escapeHtml(schema.max_value)}"` : ''}
                           ${schema?.pattern ? `pattern="${escapeHtml(schema.pattern)}"` : ''}
                           ${schema?.required ? 'required' : ''}>
                    <button class="plugin-config-field-cancel has-tooltip" data-field="${escapeHtml(fieldName)}" data-tooltip="Cancel">&#10005;</button>
                </div>
            `;
        } else {
            valueCellContent = `
                <span class="plugin-config-value-display ${hasChanged ? 'plugin-config-value-changed' : ''}" data-field="${escapeHtml(fieldName)}">${escapeHtml(newValue)}</span>
            `;
        }

        return `
            <div class="plugin-config-row ${error ? 'plugin-config-row-error' : ''} ${hasChanged ? 'plugin-config-row-changed' : ''}">
                <label class="plugin-config-label ${schema ? 'has-tooltip' : ''}" ${schema ? `data-tooltip="${escapeHtml(schema.description)}"` : ''}>
                    ${escapeHtml(fieldName)}${schema?.required ? '<span class="plugin-config-required">*</span>' : ''}
                </label>
                <div class="plugin-config-value-cell">
                    ${valueCellContent}
                    ${removeBtn}
                </div>
                ${error ? `<div class="plugin-config-row-error-msg">${escapeHtml(error)}</div>` : ''}
            </div>
        `;
    }).join('');

    // With no schema the keys are the ones typed here, and one more can be added.
    const addRow = freeForm ? `
        <div class="plugin-config-row plugin-config-add-row">
            <input type="text" class="plugin-config-new-key plugin-mono" placeholder="key" autocomplete="off" spellcheck="false">
            <div class="plugin-config-value-cell">
                <button class="plugin-config-key-add">Add key</button>
            </div>
        </div>
    ` : '';

    const hasErrors = Object.keys(state.validationErrors).length > 0;
    const hasChanges = configChanged(state);
    const isEditing = state.editingFields.size > 0;
    const running = plugins.find(p => p.name === state.pluginName)?.state === 'running';

    return `
        <div class="plugin-config-form">
            ${freeForm ? '<div class="plugin-config-free-note">Not running, so the plugin says no schema: keys are written as typed.</div>' : ''}
            <div class="plugin-config-table">
                <div class="plugin-config-header">
                    <div class="plugin-config-header-label">Setting</div>
                    <div class="plugin-config-header-value">Value</div>
                </div>
                ${fields}
                ${addRow}
            </div>
            ${(hasChanges || isEditing) ? `
                <div class="plugin-config-actions">
                    <div class="plugin-config-actions-buttons">
                        <button class="plugin-config-cancel-btn">Cancel</button>
                        <button class="${state.needsConfirmation ? 'panel-btn-warning' : ''} plugin-config-save-btn"
                                ${hasErrors ? 'disabled' : ''}>
                            ${state.needsConfirmation ? (running ? 'Confirm Restart' : 'Confirm Save') : 'Save Changes'}
                        </button>
                    </div>
                    ${state.needsConfirmation ? `
                        <div class="plugin-config-warning">
                            ${running ? 'This will apply your changes and reinitialize the plugin.' : 'This writes the config; the plugin starts with it once enabled.'}
                        </div>
                    ` : ''}
                </div>
            ` : ''}
        </div>
    `;
}

/** Whether the config differs from what the node holds: a value, or with no schema a key added or removed. */
export function configChanged(state: Pick<ConfigFormState, 'currentConfig' | 'newConfig' | 'schema' | 'freeForm'>): boolean {
    const keys = new Set([...Object.keys(state.currentConfig), ...Object.keys(state.newConfig)]);
    for (const key of keys) {
        if (state.freeForm && (key in state.newConfig) !== (key in state.currentConfig)) return true;
        const defaultValue = state.schema[key]?.default_value ?? '';
        if ((state.newConfig[key] || defaultValue) !== (state.currentConfig[key] || defaultValue)) return true;
    }
    return false;
}

function getInputType(schemaType: string): string {
    switch (schemaType) {
        case 'number': return 'number';
        case 'boolean': return 'checkbox';
        default: return 'text';
    }
}

function validateField(fieldName: string, value: string): void {
    if (!configState) return;

    const schema = configState.schema[fieldName];
    if (!schema) return;

    delete configState.validationErrors[fieldName];

    if (schema.required && !value) {
        configState.validationErrors[fieldName] = 'This field is required';
        return;
    }

    if (schema.type === 'number') {
        const num = parseFloat(value);
        if (isNaN(num)) {
            configState.validationErrors[fieldName] = 'Must be a valid number';
            return;
        }
        if (schema.min_value && num < parseFloat(schema.min_value)) {
            configState.validationErrors[fieldName] = `Must be at least ${schema.min_value}`;
            return;
        }
        if (schema.max_value && num > parseFloat(schema.max_value)) {
            configState.validationErrors[fieldName] = `Must be at most ${schema.max_value}`;
            return;
        }
    }

    if (schema.pattern && value) {
        const regex = new RegExp(schema.pattern);
        if (!regex.test(value)) {
            configState.validationErrors[fieldName] = 'Invalid format';
            return;
        }
    }
}

function updateSaveButtonState(): void {
    const saveBtn = contentElement?.querySelector('.plugin-config-save-btn') as HTMLButtonElement | null;
    if (!saveBtn || !configState) return;

    const hasErrors = Object.keys(configState.validationErrors).length > 0;
    saveBtn.disabled = hasErrors || !configChanged(configState);
}

async function savePluginConfig(): Promise<void> {
    if (!configState) return;

    if (!configState.needsConfirmation) {
        configState.needsConfirmation = true;
        render();
        return;
    }

    try {
        const requestPayload = { config: configState.newConfig };
        log.debug(SEG.UI, 'Saving config for', configState.pluginName);
        log.debug(SEG.UI, 'Request payload:', requestPayload);
        log.debug(SEG.UI, 'Payload JSON:', JSON.stringify(requestPayload, null, 2));

        const response = await apiFetch(`/api/plugins/${configState.pluginName}/config`, jsonBody('PUT', requestPayload));

        log.debug(SEG.UI, 'Response status:', response.status);

        if (!response.ok) {
            const errorData = await response.json().catch((err: unknown) => ({ message: `${response.statusText} (unreadable body: ${err})` }));
            log.debug(SEG.UI, 'Error response:', errorData);

            let errorDetails = Array.isArray(errorData.details) ? errorData.details.join('\n') : (errorData.details || '');
            if (errorData.errors && Object.keys(errorData.errors).length > 0) {
                errorDetails = 'Field-specific validation errors:\n\n';
                for (const [field, error] of Object.entries(errorData.errors)) {
                    errorDetails += `\u2022 ${field}: ${error}\n`;
                }
            }

            if (!errorDetails) {
                errorDetails = JSON.stringify(errorData, null, 2);
            }

            configState.error = {
                // A refusal from the record says itself in the error envelope's `error`.
                message: errorData.message || errorData.error || 'Failed to save configuration',
                details: errorDetails,
                status: response.status
            };
            configState.needsConfirmation = false;
            render();
            return;
        }

        expandedPlugin = null;
        configState = null;
        await fetchPlugins();
        render();
    } catch (error: unknown) {
        handleError(error, 'Failed to save config', { context: SEG.UI, silent: true });

        if (configState) {
            configState.error = {
                message: `Failed to save configuration: ${error}`,
                details: '',
                status: 0
            };
            configState.needsConfirmation = false;
            render();
        }
    }
}

interface LogEntryData {
    timestamp: string;
    level: string;
    line: string;
    source: string;
}

function connectLogStream(pluginName: string): void {
    disconnectLogStream();

    const url = backendPath(`/api/plugins/${pluginName}/logs`);
    const es = new EventSource(url, { withCredentials: true });

    es.onmessage = (event: MessageEvent) => {
        try {
            const entry: LogEntryData = JSON.parse(event.data);
            appendLogEntry(pluginName, entry);
        } catch (err) {
            // This pane exists to show a plugin's output; an entry it cannot
            // parse must not vanish from it silently.
            log.warn(SEG.UI, `Malformed log entry from ${pluginName} dropped from the stream:`, err);
        }
    };

    es.onerror = () => {
        // EventSource auto-reconnects; nothing to do
    };

    activeLogStream = es;
}

function disconnectLogStream(): void {
    if (activeLogStream) {
        activeLogStream.close();
        activeLogStream = null;
    }
}

function appendLogEntry(pluginName: string, entry: LogEntryData): void {
    if (!contentElement) return;

    const container = contentElement.querySelector(`.plugin-log-container[data-plugin="${pluginName}"]`);
    if (!container) return;

    const row = document.createElement('div');
    row.className = `plugin-log-entry plugin-log-level-${entry.level}`;

    const time = document.createElement('span');
    time.className = 'plugin-log-time';
    const date = new Date(entry.timestamp);
    const hh = String(date.getHours()).padStart(2, '0');
    const mm = String(date.getMinutes()).padStart(2, '0');
    const ss = String(date.getSeconds()).padStart(2, '0');
    time.textContent = `${hh}:${mm}:${ss}`;

    const level = document.createElement('span');
    level.className = 'plugin-log-level';
    level.textContent = entry.level.toUpperCase();

    const line = document.createElement('span');
    line.className = 'plugin-log-line';
    line.textContent = entry.line;

    row.appendChild(time);
    row.appendChild(level);
    row.appendChild(line);
    container.appendChild(row);

    // Auto-scroll if near bottom
    const scrollThreshold = 40;
    const isNearBottom = container.scrollHeight - container.scrollTop - container.clientHeight < scrollThreshold;
    if (isNearBottom) {
        container.scrollTop = container.scrollHeight;
    }
}

function filterPlugins(searchText: string): void {
    if (!contentElement) return;
    const cards = contentElement.querySelectorAll('.plugin-card');
    const search = searchText.toLowerCase();

    cards.forEach(card => {
        const htmlCard = card as HTMLElement;
        const name = card.querySelector('.plugin-name')?.textContent || '';
        const desc = card.querySelector('.plugin-description')?.textContent || '';
        const matches = name.toLowerCase().includes(search) || desc.toLowerCase().includes(search);
        if (matches) {
            htmlCard.classList.remove('u-hidden');
            htmlCard.classList.add('u-block');
        } else {
            htmlCard.classList.remove('u-block');
            htmlCard.classList.add('u-hidden');
        }
    });
}

/**
 * Create a Element definition for the plugin panel
 */
export function createPluginElement(): Element {
    return {
        id: 'plugin-element',
        title: 'Domain Plugins',
        symbol: '\u2699',
        opensAs: 'panel',
        renderContent: () => {
            const content = document.createElement('div');
            contentElement = content;

            // Attach delegated listeners once — they survive innerHTML replacements
            attachEventDelegation();

            // Live-update when health polling detects a change
            const onHealthChange = () => {
                fetchPlugins().then(() => render()).catch((err: unknown) => log.error(SEG.UI, '[PluginPanel] refresh failed:', err));
            };
            document.addEventListener('plugin-health-change', onHealthChange);

            // Auto-refresh every 10s while the panel is open
            const refreshInterval = setInterval(() => {
                if (!contentElement?.isConnected) {
                    clearInterval(refreshInterval);
                    document.removeEventListener('plugin-health-change', onHealthChange);
                    return;
                }
                fetchPlugins().then(() => render()).catch((err: unknown) => log.error(SEG.UI, '[PluginPanel] refresh failed:', err));
            }, 10_000);

            // Show loading, then fetch data
            content.innerHTML = '<div class="element-loading">Loading plugins...</div>';

            Promise.all([
                fetchPlugins(),
                fetchServerHealth()
            ]).then(() => {
                render();
                // Focus search input after render
                setTimeout(() => {
                    const searchInput = contentElement?.querySelector<HTMLInputElement>('.plugin-search-input');
                    searchInput?.focus();
                }, 100);
            }).catch((err: unknown) => log.error(SEG.UI, '[PluginPanel] initial load failed:', err));

            return content;
        }
    };
}
