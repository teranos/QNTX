/**
 * Handlers Panel - Python handler management
 *
 * Manifests as a panel glyph. Displays handler attestations
 * (predicate=handler) as code cards with syntax highlighting.
 */

import { apiFetch, apiJson } from './client';
import { jsonBody } from './http-utils';
import { escapeHtml } from './html-utils';
import { docComment, declaredWatch, declaredSchedule, declaredHandler, isDoused } from './handlers-doc';
import { attestationResultRow, RESULT_ROW_PALETTE } from './components/glyph/attestation-result-row';
import { renderTriple } from './components/glyph/attestation-triple';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';
import { formatInterval } from './pulse/types';
import { log, SEG } from './logger.ts';
import type { Glyph } from '@qntx/glyphs';

interface HandlerAttestation {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    timestamp: string;
    attributes: Record<string, string>;
}

interface Schedule {
    handler_name?: string;
    interval_seconds?: number;
    last_run_at?: string;
    next_run_at?: string;
    state: string;
}

// One thing that happened to a watcher: when, and which attestation caused it.
// `attestation` is the whole of it when the store could still resolve the id.
export interface Fire {
    at_ms: number;
    attestation_id?: string;
    attestation?: Attestation;
    error?: string;
}

export interface Watcher {
    id: string;
    action_type: string;
    // JSON PluginExecuteAction: plugin_name and handler_name, both named here.
    action_data: string;
    // What this watcher is watching for. The card leads with it.
    predicates?: string[];
    fire_count: number;
    last_fired_at?: string;
    error_count: number;
    last_error?: string;
    recent_fires?: Fire[];
}

interface ExecutionResult {
    running: boolean;
    success?: boolean;
    stdout?: string;
    error?: string;
    duration_ms?: number;
}

interface HandlerGroup {
    name: string;
    context: string;
    versions: HandlerAttestation[];
    selectedVersion: number; // index into versions array
    // 0 what it is for, 1 what it is, 2 how it does it
    openness: 0 | 1 | 2;
}

// How many result rows a card shows at each end. Three is enough to see a
// rhythm and few enough that twenty cards stay readable.
const RESULT_ROWS = 3;

// Module-level state
let contentElement: HTMLElement | null = null;
let handlers: HandlerAttestation[] = [];
let groups: HandlerGroup[] = [];
let editorViews: any[] = [];
let codeStore: Map<string, string> = new Map();
let execResults: Map<number, ExecutionResult> = new Map();
let schedules: Schedule[] = [];
let watchers: Watcher[] = [];

// Unreachable schedules and watchers are not the same as none, so the card says
// which it is rather than claiming a handler nothing fires.
let firingFailure = '';

// Why the failure is kept rather than logged and dropped: an empty list and a
// request that never succeeded rendered the same sentence, so "no handlers"
// could mean the node has none or that nobody could ask it.
let fetchFailure = '';

async function fetchHandlers(): Promise<void> {
    try {
        handlers = await apiJson<HandlerAttestation[]>('/api/attestations?predicate=handler&limit=100');
        fetchFailure = '';
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Handlers] Failed to fetch handlers:', error);
        handlers = [];
        fetchFailure = error instanceof Error ? error.message : String(error);
    }
}

// A handler's code says what it wants to be wired to; only these say whether it
// ever went off. A failure here must not read as "nothing fires this".
async function fetchFiring(): Promise<void> {
    try {
        schedules = (await apiJson<{ jobs: Schedule[] }>('/api/pulse/schedules')).jobs || [];
        watchers = await apiJson<Watcher[]>(`/api/watchers?fires=${RESULT_ROWS}`);
        firingFailure = '';
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Handlers] Failed to read what fires handlers:', error);
        schedules = [];
        watchers = [];
        firingFailure = error instanceof Error ? error.message : String(error);
    }
}

// PluginExecuteAction, as the engine writes it: the plugin and the handler,
// both named. Reading them is what makes this a join rather than a guess.
export function watchAction(actionData: string): { plugin: string; handler: string } {
    try {
        const parsed = JSON.parse(actionData);
        return { plugin: parsed.plugin_name || '', handler: parsed.handler_name || '' };
    } catch (err) {
        // A join that is wrong is wrong quietly: every card reads as unwired.
        // This line is the difference between unwired and unreadable.
        log.warn(SEG.UI, 'Watcher action data unparseable; its card reads as unwired:', err);
        return { plugin: '', handler: '' };
    }
}

// Exported because a join that is wrong is wrong quietly: every card reads as
// unwired and nothing says a match was attempted and missed.
export function findWatcher(all: Watcher[], plugin: string, handler: string): Watcher | undefined {
    return all.find(w => {
        if (w.action_type !== 'plugin_execute') return false;
        const action = watchAction(w.action_data);
        return action.handler === handler && action.plugin === plugin;
    });
}

// A schedule stores PluginHandlerName — "<plugin>/<handler>" — while the
// attestation carries the two apart, as subject and context.
function scheduleOf(g: HandlerGroup): Schedule | undefined {
    const key = `${g.context}/${g.name}`;
    return schedules.find(s => s.handler_name === key);
}

// The action names both, so both are matched. Reading the plugin off the
// watcher id assumed a naming convention the engine never promised.
function watcherOf(g: HandlerGroup): Watcher | undefined {
    return findWatcher(watchers, g.context, g.name);
}

function groupHandlers(): void {
    // Results are keyed by position, so regrouping is the one moment they stop
    // meaning anything. Clearing them anywhere else discards an answer somebody
    // asked for and watched arrive.
    execResults.clear();

    const map = new Map<string, HandlerAttestation[]>();
    for (const h of handlers) {
        const key = h.subjects[0] || '';
        const list = map.get(key);
        if (list) {
            list.push(h);
        } else {
            map.set(key, [h]);
        }
    }
    groups = [];
    for (const [name, versions] of map) {
        // Sort newest first
        versions.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
        // A doused handler is not gone — every stratum of it is still in the
        // store — it just no longer burns, and the panel is what burns.
        if (isDoused(versions[0].attributes?.code || '')) continue;
        groups.push({
            name,
            context: versions[0].contexts[0] || '',
            versions,
            selectedVersion: 0,
            openness: 0,
        });
    }
}

function formatDate(timestamp: string): string {
    const d = new Date(timestamp);
    return `${String(d.getFullYear()).slice(2)}-${d.getMonth() + 1}-${d.getDate()}`;
}

function formatDateTime(timestamp: string): string {
    const d = new Date(timestamp);
    return `${String(d.getFullYear()).slice(2)}-${d.getMonth() + 1}-${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

async function executeHandler(index: number): Promise<void> {
    const code = codeStore.get(`handler-editor-${index}`);
    if (!code) return;

    execResults.set(index, { running: true });
    renderOutput(index);

    try {
        const response = await apiFetch('/api/python/execute', jsonBody('POST', { content: code }));
        const data = await response.json();
        execResults.set(index, {
            running: false,
            success: data.success,
            stdout: data.stdout || '',
            error: data.error || '',
            duration_ms: data.duration_ms,
        });
    } catch (err: unknown) {
        execResults.set(index, {
            running: false,
            success: false,
            error: err instanceof Error ? err.message : String(err),
        });
    }
    renderOutput(index);
}

function renderOutput(index: number): void {
    if (!contentElement) return;
    const container = contentElement.querySelector<HTMLElement>(`#handler-output-${index}`);
    if (!container) return;

    const result = execResults.get(index);
    if (!result) {
        container.innerHTML = '';
        return;
    }

    if (result.running) {
        container.innerHTML = '<div class="handlers-output-running">Running...</div>';
        return;
    }

    const parts: string[] = [];
    if (result.stdout) {
        parts.push(`<pre class="handlers-output-text">${escapeHtml(result.stdout)}</pre>`);
    }
    if (result.error) {
        parts.push(`<pre class="handlers-output-error">${escapeHtml(result.error)}</pre>`);
    }
    if (result.duration_ms !== undefined) {
        parts.push(`<span class="handlers-output-duration">${result.duration_ms}ms</span>`);
    }
    container.innerHTML = parts.join('');
}

function destroyEditors(): void {
    for (const view of editorViews) {
        view.destroy();
    }
    editorViews = [];
}

async function mountEditors(): Promise<void> {
    if (!contentElement) return;

    const { EditorView } = await import('@codemirror/view');
    const { EditorState } = await import('@codemirror/state');
    const { python } = await import('@codemirror/lang-python');
    const { oneDark } = await import('@codemirror/theme-one-dark');

    const containers = contentElement.querySelectorAll<HTMLElement>('.handlers-card-editor[id]');
    for (const container of containers) {
        const code = codeStore.get(container.id) || '';
        const view = new EditorView({
            state: EditorState.create({
                doc: code,
                extensions: [
                    python(),
                    oneDark,
                    EditorView.lineWrapping,
                    EditorState.readOnly.of(true),
                    EditorView.editable.of(false),
                    EditorView.theme({
                        '&': { fontSize: '12px', maxHeight: '300px' },
                        '.cm-scroller': { overflow: 'auto' },
                        '.cm-gutters': { display: 'none' },
                        '.cm-content': { padding: '8px 0' },
                    }),
                ],
            }),
            parent: container,
        });
        editorViews.push(view);
    }
}

// A handler that says nothing about itself is a fact about it, not a blank.
function doc(code: string): string {
    const text = docComment(code);
    if (text === '') return `<span class="handlers-card-nodoc">no doc comment</span>`;
    return escapeHtml(text);
}

function agoOf(atMs: number): string {
    const d = new Date(atMs);
    return `${String(d.getFullYear()).slice(2)}-${d.getMonth() + 1}-${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

// What set this handler off, above the card, in the shape AX uses for a result.
// The declaration is read from the handler's own code: a node with no watcher
// registered does not make an @watch untrue.
function triggers(g: HandlerGroup, code: string, index: number): string {
    const watching = declaredWatch(code);
    const every = declaredSchedule(code);
    const s = scheduleOf(g);
    const w = watcherOf(g);
    if (watching === null && every === null && !s && !w) return '';

    // The rows are AX result rows and those are elements, so the slot is left
    // empty here and mountResultRows fills it after the card is in the DOM.
    const fires = (w?.recent_fires || []).slice(0, RESULT_ROWS);
    const quiet = fires.length === 0 ? nothingYet(g, watching, w) : '';
    const rows = quiet !== ''
        ? `<div class="handlers-result-quiet">${escapeHtml(quiet)}</div>`
        : `<div class="handlers-result-slot" data-caused="${index}"></div>`;

    const pills: string[] = [];
    if (watching !== null) {
        pills.push(pill('watch', watching || 'everything'));
    }
    if (every !== null) {
        pills.push(pill('schedule', readableEvery(every)));
    } else if (s) {
        pills.push(pill('schedule', formatInterval(s.interval_seconds || 0)));
    }
    const named = declaredHandler(code);
    if (named !== null) {
        pills.push(pill('handler', named || g.name));
    }

    return `<div class="handlers-card-triggers">${rows}<div class="handlers-pills">${pills.join('')}</div></div>`;
}

// The other end of the flow: what this handler filed. An attestation it writes
// carries the handler as its context — the "of" in subject is predicate of
// context — so asking for that context is asking what this handler produced.
function produced(index: number): string {
    return `<div class="handlers-card-produced" data-produced="${index}"></div>`;
}

// AX result rows, mounted after innerHTML because they are elements. Three at
// each end, which is enough to see a rhythm.
function mountResultRows(): void {
    if (!contentElement) return;
    for (const slot of contentElement.querySelectorAll<HTMLElement>('.handlers-result-slot[data-caused]')) {
        const g = groups[Number(slot.dataset.caused)];
        if (!g) continue;
        const fires = (watcherOf(g)?.recent_fires || []).slice(0, RESULT_ROWS);
        for (const f of fires) {
            // A failed fire is the row that matters most — a failing handler
            // is a fix-now kind of event. Drawn even when no attestation
            // exists (a queue write that failed before one did).
            if (f.error) {
                slot.appendChild(errorFireRow(f));
                continue;
            }
            if (!f.attestation) continue;
            slot.appendChild(attestationResultRow(f.attestation, {
                className: 'handlers-result-item',
                padding: '4px 8px',
                marginBottom: '2px',
                tooltip: `${f.attestation.id || ''} — fired ${agoOf(f.at_ms)}`,
                body: renderTriple(f.attestation, {
                    tag: 'div',
                    fontSize: '11px',
                    palette: RESULT_ROW_PALETTE,
                }),
                trailing: whenBadge(f.at_ms),
            }));
        }
    }
}

// What each handler filed, asked for by context. One request per handler, so
// it runs after the cards are up rather than holding the panel closed.
async function fetchProduced(): Promise<void> {
    if (!contentElement) return;
    for (const slot of contentElement.querySelectorAll<HTMLElement>('.handlers-card-produced')) {
        const g = groups[Number(slot.dataset.produced)];
        if (!g) continue;
        let filed: Attestation[] = [];
        try {
            filed = await apiJson<Attestation[]>(
                `/api/attestations?context=${encodeURIComponent(g.name)}&limit=${RESULT_ROWS}`);
        } catch (error: unknown) {
            log.warn(SEG.UI, `[Handlers] could not read what ${g.name} filed:`, error);
            slot.innerHTML = `<div class="handlers-result-quiet">could not read what this filed</div>`;
            continue;
        }
        // A handler that has filed nothing is a handler that has filed nothing.
        if (filed.length === 0) {
            slot.innerHTML = `<div class="handlers-result-quiet">nothing is filed of ${escapeHtml(g.name)}</div>`;
            continue;
        }
        slot.innerHTML = '';
        for (const as of filed.slice(0, RESULT_ROWS)) {
            slot.appendChild(attestationResultRow(as, {
                className: 'handlers-result-item',
                padding: '4px 8px',
                marginBottom: '2px',
                body: renderTriple(as, { tag: 'div', fontSize: '11px', palette: RESULT_ROW_PALETTE }),
                trailing: whenBadge(new Date(as.timestamp || 0).getTime()),
            }));
        }
    }
}

// A failed fire, in the shape the CSS anticipated and nothing drew: the error
// where the triple would be, when on the right. Exported for tests.
export function errorFireRow(f: Fire): HTMLElement {
    const row = document.createElement('div');
    row.className = 'handlers-result-item handlers-result-row handlers-result-error';
    row.style.padding = '4px 8px';
    row.style.marginBottom = '2px';
    if (f.attestation_id) row.title = `${f.attestation_id} — failed ${agoOf(f.at_ms)}`;

    const msg = document.createElement('div');
    msg.className = 'handlers-result-id';
    msg.textContent = f.error ?? 'failed';
    row.appendChild(msg);

    row.appendChild(whenBadge(f.at_ms));
    return row;
}

function whenBadge(atMs: number): HTMLElement {
    const badge = document.createElement('span');
    badge.style.fontSize = '10px';
    badge.style.fontFamily = 'monospace';
    badge.style.color = 'var(--text-secondary)';
    badge.style.flexShrink = '0';
    badge.textContent = agoOf(atMs);
    return badge;
}

// An interval in seconds reads as a duration. Anything else was written by a
// person and is left as they wrote it.
function readableEvery(every: string): string {
    const seconds = Number(every);
    if (every !== '' && Number.isFinite(seconds) && seconds > 0) {
        return formatInterval(seconds);
    }
    return every || 'unstated';
}

function pill(kind: string, text: string): string {
    return `<span class="handlers-pill handlers-pill-${kind}">@${kind}<span class="handlers-pill-arg">${escapeHtml(text)}</span></span>`;
}

// Why there is nothing to show. A handler that declares no @watch is not
// missing a watcher, so saying it has none is noise about a thing it never
// asked for.
function nothingYet(g: HandlerGroup, watching: string | null, w: Watcher | undefined): string {
    if (firingFailure !== '') return `could not read what fires this: ${firingFailure}`;
    if (w) {
        return w.fire_count > 0
            ? `fired ${w.fire_count}× — none of them kept what caused it`
            : 'has not fired yet';
    }
    if (watching === null) return '';
    return `nothing on this node watches ${watching || 'anything'} for ${g.name}`;
}

// What fires this, if anything. A handler with neither only runs by hand.
function wiring(g: HandlerGroup): string {
    if (firingFailure !== '') return `<span class="handlers-card-wiring">wiring unknown</span>`;
    const out: string[] = [];
    const s = scheduleOf(g);
    if (s) out.push(`<span class="handlers-card-wiring">every ${formatInterval(s.interval_seconds || 0)}</span>`);
    const w = watcherOf(g);
    if (w) out.push(`<span class="handlers-card-wiring">watch ${w.fire_count}×</span>`);
    return out.join('');
}

function facts(g: HandlerGroup, h: HandlerAttestation): string {
    const rows = [`filed ${formatDateTime(h.timestamp)}`, h.id];
    if (firingFailure !== '') {
        rows.push(`could not read what fires this: ${firingFailure}`);
        return `<div class="handlers-card-facts">${rows.map(r => `<div>${escapeHtml(r)}</div>`).join('')}</div>`;
    }
    const s = scheduleOf(g);
    if (s) rows.push(`schedule ${s.state} — last ran ${s.last_run_at || 'never'}, next ${s.next_run_at || 'unset'}`);
    const w = watcherOf(g);
    if (w) rows.push(`watch fired ${w.fire_count}× — last ${w.last_fired_at || 'never'}, ${w.error_count} errors`);
    if (w?.last_error) rows.push(`last error: ${w.last_error}`);
    return `<div class="handlers-card-facts">${rows.map(r => `<div>${escapeHtml(r)}</div>`).join('')}</div>`;
}

function renderCards(): string {
    if (fetchFailure !== '') {
        return `<div class="handlers-empty">Could not read handlers: ${escapeHtml(fetchFailure)}</div>`;
    }
    if (groups.length === 0) {
        return `<div class="handlers-empty">No handlers found</div>`;
    }

    codeStore.clear();
    const cards = groups.map((g, i) => {
        const h = g.versions[g.selectedVersion];
        const name = escapeHtml(g.name || '(unnamed)');
        const context = g.context ? escapeHtml(g.context) : '';
        const code = h.attributes?.code || '';
        const editorId = `handler-editor-${i}`;
        codeStore.set(editorId, code);
        const label = context ? `${name} <span class="handlers-card-context">${context}</span>` : name;

        let dateHtml: string;
        if (g.versions.length > 1) {
            const options = g.versions.map((v, vi) =>
                `<option value="${vi}"${vi === g.selectedVersion ? ' selected' : ''}>${formatDateTime(v.timestamp)}</option>`
            ).join('');
            dateHtml = `<select class="handlers-version-select" data-group-index="${i}">${options}</select>`;
        } else {
            dateHtml = `<span class="handlers-card-date">${formatDate(h.timestamp)}</span>`;
        }

        return `<div class="handlers-card" data-group="${i}" data-openness="${g.openness}">
            ${triggers(g, code, i)}
            <div class="handlers-card-header">
                <span class="handlers-card-label">${label}</span>
                ${wiring(g)}
                ${dateHtml}
                ${g.openness >= 2 ? `<button class="handlers-play-btn" data-action="execute" data-index="${i}" title="Run this code by hand">▶</button>` : ''}
            </div>
            <div class="handlers-card-doc" data-action="open" data-index="${i}">${doc(code)}</div>
            ${g.openness >= 1 ? facts(g, h) : ''}
            ${g.openness >= 2 ? `<div class="handlers-card-editor" id="${editorId}"></div>` : ''}
            <div class="handlers-card-output" id="handler-output-${i}"></div>
            ${produced(i)}
        </div>`;
    }).join('');

    return `<div class="handlers-grid">${cards}</div>`;
}

// Rebuild the groups first, then draw. The two used to be separate functions
// with identical bodies, which is how one of them quietly reset every card.
function render(): void {
    groupHandlers();
    renderWithGroups();
}

function renderWithGroups(): void {
    if (!contentElement) return;
    destroyEditors();
    contentElement.innerHTML = `
        <div class="handlers-panel">
            <div class="handlers-header">
                <span class="handlers-count">${groups.length}</span>
            </div>
            ${renderCards()}
        </div>
    `;
    mountEditors();
    mountResultRows();
    fetchProduced().catch(error => log.warn(SEG.UI, '[Handlers] produced rows:', error));

    // The output divs are new, so results already held have to be written back
    // or they vanish with the div that was showing them.
    for (let i = 0; i < groups.length; i++) {
        renderOutput(i);
    }
}

function attachEventDelegation(el: HTMLElement): void {
    el.addEventListener('change', (e) => {
        const target = e.target as HTMLElement;
        if (target.classList.contains('handlers-version-select')) {
            const select = target as HTMLSelectElement;
            const groupIndex = parseInt(select.dataset.groupIndex || '', 10);
            if (!isNaN(groupIndex) && groups[groupIndex]) {
                groups[groupIndex].selectedVersion = parseInt(select.value, 10);
                renderWithGroups();
            }
        }
    });

    el.addEventListener('click', async (e) => {
        const target = e.target as HTMLElement;
        const action = target.closest<HTMLElement>('[data-action]')?.dataset.action;
        if (!action) return;

        if (action === 'execute') {
            const index = parseInt(target.closest<HTMLElement>('[data-index]')?.dataset.index || '', 10);
            if (!isNaN(index)) {
                executeHandler(index);
            }
            return;
        }

        // One card opens further rather than opening a second thing, so the
        // question "what is this" and "how does it do it" stay one object.
        if (action === 'open') {
            const index = parseInt(target.closest<HTMLElement>('[data-index]')?.dataset.index || '', 10);
            const g = groups[index];
            if (g) {
                g.openness = ((g.openness + 1) % 3) as 0 | 1 | 2;
                renderWithGroups();
            }
        }
    });
}

export function createHandlersGlyph(): Glyph {
    return {
        id: 'handlers-glyph',
        title: 'Handlers',
        manifestationType: 'panel',
        renderContent: () => {
            const content = document.createElement('div');
            contentElement = content;

            attachEventDelegation(content);
            render();
            fetchHandlers().then(() => render());
            // Not render(): that regroups, and regrouping resets every card to
            // closed. What fires a handler does not change what the handlers are.
            fetchFiring().then(() => renderWithGroups());

            const cleanupInterval = setInterval(() => {
                if (!contentElement?.isConnected) {
                    clearInterval(cleanupInterval);
                    destroyEditors();
                    contentElement = null;
                }
            }, 2000);

            return content;
        },
    };
}
