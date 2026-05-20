/**
 * Attestation Glyph (+) — view a single attestation on canvas
 *
 * Opened via double-click on attestation result items in AX or SE glyphs.
 * Title bar IS the triple (subjects is predicates of contexts).
 * Attributes shown below title bar only when present.
 * No attributes → compact title-bar-only glyph.
 * Metadata (actors, source, timestamps, id) hidden by default —
 * revealed via hover pill at bottom center of title bar.
 */

import type { Glyph } from '@qntx/glyphs';
import { wireExpandToWindow, teardownWindowDrag, removeWindowControls, isInWindowState, setWindowState, glyphRun } from '@qntx/glyphs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { AS } from '@generated/sym.js';
import { renderTriple } from './attestation-triple';
import { isFastaAttribute, buildFastaViewer, isAminoAcidSequence, renderAminoAcidSequence } from './fasta-renderer';
import { log, SEG } from '../../logger';
import { canvasPlaced } from '@qntx/glyphs';
import { preventDrag, makeDraggable, makeResizable, storeCleanup } from '@qntx/glyphs';
import { screenToCanvas } from './canvas/canvas-pan';
import { uiState } from '../../state/ui';
import { getGlyphTypeBySymbol } from './glyph-registry';

// Muted azure — low contrast, easy on eyes
const AZURE = '#8a969b';
const AZURE_KEYWORD = '#6b7175';
const AZURE_VALUE = '#a0a8ad';
const AZURE_BORDER = 'rgba(255,255,255,0.06)';

/**
 * Try to extract an array from a JSON string value.
 * Checks: direct array, or object with a single array-valued leaf
 * (e.g. { resultList: { result: [...] } }).
 */
export function extractArray(value: string): unknown[] | null {
    let parsed: unknown;
    try { parsed = JSON.parse(value); } catch { return null; }
    if (Array.isArray(parsed) && parsed.length > 1) return parsed;
    // Don't walk into objects — extractObject handles those.
    return null;
}

/**
 * Try to parse a JSON string as an object with flat key-value pairs.
 * Returns the object if it has at least 2 string/number/boolean fields, null otherwise.
 */
export function extractObject(value: string): Record<string, unknown> | null {
    let parsed: unknown;
    try { parsed = JSON.parse(value); } catch { return null; }
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return null;
    const obj = parsed as Record<string, unknown>;
    return Object.keys(obj).length >= 2 ? obj : null;
}

/**
 * Check if a string contains HTML tags (has < followed by a letter).
 */
function containsHtml(s: string): boolean {
    let i = s.indexOf('<');
    while (i !== -1 && i < s.length - 1) {
        const next = s.charCodeAt(i + 1);
        // a-z or A-Z or /
        if ((next >= 65 && next <= 90) || (next >= 97 && next <= 122) || next === 47) return true;
        i = s.indexOf('<', i + 1);
    }
    return false;
}

/**
 * Render a scalar text value. URLs become clickable links.
 */
function renderScalar(text: string): HTMLElement {
    if (text.startsWith('http://') || text.startsWith('https://')) {
        const a = document.createElement('a');
        a.href = text;
        a.textContent = text;
        a.target = '_blank';
        a.rel = 'noopener';
        a.style.color = '#5a8faa';
        a.style.fontSize = '12px';
        a.style.wordBreak = 'break-word';
        return a;
    }
    const el = document.createElement('span');
    el.style.color = AZURE_VALUE;
    el.style.fontSize = '12px';
    el.style.wordBreak = 'break-word';
    el.textContent = text;
    return el;
}

/**
 * Schema.org-based file type classification.
 * Extension -> semantic type -> color. The type determines future renderers.
 * https://schema.org/encodingFormat
 */
export type FileType = '3DModel' | 'Dataset' | 'ImageObject' | 'SequenceData' | 'Document' | 'Unknown';

const EXT_TYPE: Record<string, FileType> = {
    // 3DModel — molecular structures
    pdb: '3DModel', cif: '3DModel', bcif: '3DModel', mmcif: '3DModel', sdf: '3DModel', mol2: '3DModel',
    // Dataset — tabular, scores, alignments
    csv: 'Dataset', tsv: 'Dataset', json: 'Dataset', xml: 'Dataset', a3m: 'Dataset', sto: 'Dataset',
    // ImageObject — raster/vector
    png: 'ImageObject', jpg: 'ImageObject', jpeg: 'ImageObject', svg: 'ImageObject', gif: 'ImageObject',
    // SequenceData — BioChemEntity / Protein
    fasta: 'SequenceData', fa: 'SequenceData', fastq: 'SequenceData', gff: 'SequenceData', bed: 'SequenceData',
    // Document
    pdf: 'Document', txt: 'Document', md: 'Document', html: 'Document',
};

const TYPE_COLOR: Record<FileType, string> = {
    '3DModel':      'rgba(90,130,138,0.25)',
    'Dataset':      'rgba(122,138,90,0.25)',
    'ImageObject':  'rgba(138,100,106,0.25)',
    'SequenceData': 'rgba(106,90,138,0.25)',
    'Document':     'rgba(138,122,90,0.25)',
    'Unknown':      'rgba(90,106,122,0.20)',
};

function isUrlValue(v: unknown): v is string {
    return typeof v === 'string' && (v.startsWith('https://') || v.startsWith('http://'));
}

function getExtension(url: string): string {
    const lastSlash = url.lastIndexOf('/');
    const filename = lastSlash >= 0 ? url.slice(lastSlash + 1) : url;
    const dot = filename.lastIndexOf('.');
    return dot >= 0 ? filename.slice(dot + 1).toLowerCase() : '';
}

function buildUrlPill(key: string, url: string): HTMLElement {
    const ext = getExtension(url);
    const fileType = EXT_TYPE[ext] || 'Unknown';
    const color = TYPE_COLOR[fileType];

    const pill = document.createElement('a');
    pill.href = url;
    pill.target = '_blank';
    pill.rel = 'noopener';
    pill.title = url;
    // Extract filename from URL
    const lastSlash = url.lastIndexOf('/');
    const filename = lastSlash >= 0 ? url.slice(lastSlash + 1) : url;

    pill.style.display = 'inline-flex';
    pill.style.alignItems = 'center';
    pill.style.gap = '4px';
    pill.style.padding = '2px 6px';
    pill.style.backgroundColor = color;
    pill.style.color = AZURE_VALUE;
    pill.style.fontSize = '10px';
    pill.style.fontFamily = 'monospace';
    pill.style.textDecoration = 'none';
    pill.style.cursor = 'pointer';

    const keySpan = document.createElement('span');
    keySpan.style.opacity = '0.5';
    keySpan.textContent = key;

    const nameSpan = document.createElement('span');
    nameSpan.textContent = filename;

    pill.append(keySpan, nameSpan);
    return pill;
}

/**
 * Render a single array item as structured key-value pairs.
 */
export function renderItem(item: unknown): HTMLElement {
    const container = document.createElement('div');
    if (typeof item === 'object' && item !== null && !Array.isArray(item)) {
        const entries = Object.entries(item as Record<string, unknown>);
        // Collect URL entries for pill rendering
        const urlEntries: [string, string][] = [];
        const otherEntries: [string, unknown][] = [];
        for (const [k, v] of entries) {
            if (v === '' || v === null || v === undefined) continue;
            if (isUrlValue(v)) {
                urlEntries.push([k, v]);
            } else {
                otherEntries.push([k, v]);
            }
        }

        // Render non-URL entries first
        for (const [k, v] of otherEntries) {
            const row = document.createElement('div');
            row.style.marginBottom = '2px';
            const keyEl = document.createElement('span');
            keyEl.style.color = AZURE_KEYWORD;
            keyEl.style.fontSize = '11px';
            keyEl.textContent = k + ': ';
            // Nested objects/arrays: render recursively
            if (typeof v === 'object' && v !== null) {
                const nested = document.createElement('div');
                nested.style.marginLeft = '12px';
                nested.style.borderLeft = `1px solid ${AZURE_BORDER}`;
                nested.style.paddingLeft = '8px';
                nested.appendChild(renderItem(v));
                row.append(keyEl, nested);
                container.appendChild(row);
                continue;
            }
            const text = String(v);
            const hasHtml = containsHtml(text);
            if (hasHtml) {
                const valEl = document.createElement('div');
                valEl.style.color = AZURE_VALUE;
                valEl.style.fontSize = '12px';
                valEl.style.wordBreak = 'break-word';
                valEl.style.marginTop = '4px';
                const doc = new DOMParser().parseFromString(text, 'text/html');
                const renderNodes = (parent: Node, target: HTMLElement) => {
                    for (const child of Array.from(parent.childNodes)) {
                        if (child.nodeType === Node.TEXT_NODE) {
                            const span = document.createElement('span');
                            span.textContent = child.textContent || '';
                            target.appendChild(span);
                        } else if (child.nodeType === Node.ELEMENT_NODE) {
                            const tag = (child as Element).tagName.toLowerCase();
                            if (tag === 'br') {
                                target.appendChild(document.createElement('br'));
                            } else if (tag.startsWith('h')) {
                                const heading = document.createElement('div');
                                heading.style.fontWeight = '600';
                                heading.style.marginTop = '8px';
                                heading.style.marginBottom = '2px';
                                heading.style.color = AZURE_VALUE;
                                heading.textContent = (child as Element).textContent || '';
                                target.appendChild(heading);
                            } else if (tag === 'p') {
                                const p = document.createElement('div');
                                p.style.marginBottom = '4px';
                                renderNodes(child, p);
                                target.appendChild(p);
                            } else {
                                renderNodes(child, target);
                            }
                        }
                    }
                };
                renderNodes(doc.body, valEl);
                row.append(keyEl, valEl);
            } else if (isAminoAcidSequence(text)) {
                row.append(keyEl, renderAminoAcidSequence(text));
            } else {
                row.append(keyEl, renderScalar(text));
            }
            container.appendChild(row);
        }

        // Render URL entries as pill group
        if (urlEntries.length > 0) {
            const pillGroup = document.createElement('div');
            pillGroup.style.display = 'flex';
            pillGroup.style.flexWrap = 'wrap';
            pillGroup.style.gap = '4px';
            pillGroup.style.marginTop = '4px';
            for (const [k, url] of urlEntries) {
                pillGroup.appendChild(buildUrlPill(k, url));
            }
            container.appendChild(pillGroup);
        }
    } else if (Array.isArray(item)) {
        const hasObjects = item.length > 0 && typeof item[0] === 'object' && item[0] !== null;
        // Large arrays of objects: sub-pager (page size 5)
        if (hasObjects && item.length > 9) {
            container.appendChild(buildArrayPager(item));
        } else if (hasObjects) {
            // Small arrays of objects: render inline with separators
            for (const elem of item) {
                const itemEl = document.createElement('div');
                itemEl.style.borderBottom = `1px solid ${AZURE_BORDER}`;
                itemEl.style.paddingBottom = '4px';
                itemEl.style.marginBottom = '4px';
                itemEl.appendChild(renderItem(elem));
                container.appendChild(itemEl);
            }
        } else {
            // Scalar arrays: compact list
            for (const elem of item) {
                const el = document.createElement('div');
                el.appendChild(renderScalar(String(elem)));
                container.appendChild(el);
            }
        }
    } else {
        container.appendChild(renderScalar(typeof item === 'string' ? item : String(item)));
    }
    return container;
}

/**
 * Render any attribute value — handles strings containing JSON, native arrays/objects, and scalars.
 * Works for both lit:annotated (array of objects) and apt:protein-info (nested object).
 */
export function renderAttributeValue(value: unknown): HTMLElement {
    // String values: try to parse as JSON array or object
    if (typeof value === 'string') {
        const arr = extractArray(value);
        if (arr) return renderItem(arr);
        const obj = extractObject(value);
        if (obj) return renderItem(obj);
        // Plain string
        const el = document.createElement('div');
        el.style.fontSize = '12px';
        el.style.color = AZURE_VALUE;
        el.style.whiteSpace = 'pre-wrap';
        el.style.wordBreak = 'break-word';
        el.style.lineHeight = '1.5';
        el.textContent = value;
        return el;
    }
    // Already-parsed array
    if (Array.isArray(value) && value.length > 1) return renderItem(value);
    // Already-parsed object
    if (typeof value === 'object' && value !== null) return renderItem(value);
    // Scalar fallback
    const el = document.createElement('div');
    el.style.fontSize = '12px';
    el.style.color = AZURE_VALUE;
    el.textContent = String(value);
    return el;
}

/**
 * Build a pager for an array: shows one item at a time with < N/M > navigation.
 */
function buildArrayPager(items: unknown[]): HTMLElement {
    const wrapper = document.createElement('div');
    let index = 0;

    const nav = document.createElement('div');
    nav.style.display = 'flex';
    nav.style.alignItems = 'center';
    nav.style.gap = '8px';
    nav.style.marginBottom = '6px';

    const prevBtn = document.createElement('button');
    prevBtn.textContent = '\u25C0'; // ◀
    prevBtn.style.background = 'none';
    prevBtn.style.border = '1px solid var(--border)';
    prevBtn.style.color = AZURE_VALUE;
    prevBtn.style.cursor = 'pointer';
    prevBtn.style.padding = '2px 6px';
    prevBtn.style.fontSize = '11px';
    prevBtn.style.borderRadius = '3px';

    const nextBtn = document.createElement('button');
    nextBtn.textContent = '\u25B6'; // ▶
    nextBtn.style.background = 'none';
    nextBtn.style.border = '1px solid var(--border)';
    nextBtn.style.color = AZURE_VALUE;
    nextBtn.style.cursor = 'pointer';
    nextBtn.style.padding = '2px 6px';
    nextBtn.style.fontSize = '11px';
    nextBtn.style.borderRadius = '3px';

    const counter = document.createElement('span');
    counter.style.color = AZURE_KEYWORD;
    counter.style.fontSize = '11px';
    counter.style.fontFamily = 'monospace';

    preventDrag(prevBtn);
    preventDrag(nextBtn);
    nav.append(prevBtn, counter, nextBtn);
    wrapper.appendChild(nav);

    const itemContainer = document.createElement('div');
    wrapper.appendChild(itemContainer);

    const show = () => {
        counter.textContent = `${index + 1} / ${items.length}`;
        prevBtn.style.opacity = index === 0 ? '0.3' : '1';
        nextBtn.style.opacity = index === items.length - 1 ? '0.3' : '1';
        itemContainer.replaceChildren(renderItem(items[index]));
    };

    prevBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (index > 0) { index--; show(); }
    });
    nextBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (index < items.length - 1) { index++; show(); }
    });

    // Arrow key navigation when pager or its parent glyph has focus
    wrapper.tabIndex = 0;
    wrapper.style.outline = 'none';
    wrapper.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowLeft' && index > 0) {
            index--; show(); e.preventDefault(); e.stopPropagation();
        } else if (e.key === 'ArrowRight' && index < items.length - 1) {
            index++; show(); e.preventDefault(); e.stopPropagation();
        }
    });

    show();
    return wrapper;
}

/**
 * Parse attributes from attestation, returns non-empty object or null.
 */
function parseAttributes(attestation: Attestation): Record<string, unknown> | null {
    if (!attestation.attributes) return null;
    try {
        const attrs = typeof attestation.attributes === 'string'
            ? JSON.parse(attestation.attributes)
            : attestation.attributes;
        if (typeof attrs === 'object' && attrs !== null && Object.keys(attrs).length > 0) {
            return attrs;
        }
    } catch { /* ignore */ }
    return null;
}

/**
 * Build metadata lines from attestation fields.
 */
function buildMetaLines(attestation: Attestation): string[] {
    const lines: string[] = [];
    if (attestation.actors && attestation.actors.length > 0) {
        lines.push(`actors: ${attestation.actors.join(', ')}`);
    }
    if (attestation.source) {
        lines.push(`source: ${attestation.source}`);
    }
    if (attestation.timestamp) {
        lines.push(`timestamp: ${formatTimestamp(attestation.timestamp)}`);
    }
    if (attestation.created_at) {
        lines.push(`created: ${formatTimestamp(attestation.created_at)}`);
    }
    if (attestation.signer_did) {
        // Cyan color for signer (between green and purple)
        lines.push(`<span style="color: #00d4aa">signer: ${attestation.signer_did}</span>`);
    }
    if (attestation.signature && attestation.signature.length > 0) {
        lines.push(`signature: ${attestation.signature.length} bytes`);
    }
    if (attestation.id) {
        lines.push(`id: ${attestation.id}`);
    }
    return lines;
}

/**
 * Create an Attestation glyph.
 * Title bar = triple. Attributes below if present. Metadata behind hover pill.
 */
export function createAttestationGlyph(glyph: Glyph): HTMLElement {
    let attestation: Attestation | null = null;
    try {
        if (glyph.content) {
            attestation = JSON.parse(glyph.content);
        }
    } catch (err) {
        log.warn(SEG.GLYPH, `[AsGlyph] Failed to parse attestation content for ${glyph.id}:`, err);
    }

    const attrs = attestation ? parseAttributes(attestation) : null;

    // Title bar: + symbol + triple + expand button + metadata pill
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar glyph-title-bar--auto';
    titleBar.style.position = 'relative';

    const symbol = document.createElement('span');
    symbol.textContent = AS;
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = AZURE;
    titleBar.appendChild(symbol);

    if (attestation) {
        const tripleText = renderTriple(attestation, {
            palette: { value: AZURE_VALUE, keyword: AZURE_KEYWORD },
            showWatcherEyes: true,
        });
        titleBar.appendChild(tripleText);
    }

    // Expand/place button
    const expandBtn = document.createElement('button');
    expandBtn.className = 'titlebar-btn';
    expandBtn.textContent = '\u2B06'; // ⬆
    expandBtn.title = 'Expand to window';
    expandBtn.style.flexShrink = '0';
    expandBtn.style.marginLeft = 'auto';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    // Metadata pill — appears on hover at bottom center of title bar
    if (attestation) {
        const metaLines = buildMetaLines(attestation);
        if (metaLines.length > 0) {
            const pill = document.createElement('div');
            pill.className = 'as-meta-pill';

            const metaPopover = document.createElement('div');
            metaPopover.className = 'as-meta-popover';
            metaPopover.innerHTML = metaLines.join('<br>');

            pill.appendChild(metaPopover);
            titleBar.appendChild(pill);
        }
    }

    // Compact when no attributes, expanded when attributes present
    const hasContent = !!attrs;

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-attestation-glyph',
        defaults: { x: 200, y: 200, width: 420, height: hasContent ? 200 : 28 },
        resizable: hasContent,
        useMinHeight: true,
        logLabel: 'AsGlyph',
    });
    element.style.minWidth = '200px';

    element.appendChild(titleBar);

    // Attributes content — only when there are attributes to show
    if (attestation && attrs) {
        const content = document.createElement('div');
        content.className = 'glyph-content-area';
        content.style.padding = '4px 8px';
        content.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
        content.style.borderTop = '1px solid var(--border)';
        content.style.fontSize = '12px';
        content.style.fontFamily = 'monospace';

        for (const [key, value] of Object.entries(attrs)) {
            // FASTA: skip 'format' key, render 'data' with viewer
            if (isFastaAttribute(attrs, key)) {
                if (key === 'data' && typeof value === 'string') {
                    const attrRow = document.createElement('div');
                    attrRow.style.marginBottom = '4px';
                    attrRow.appendChild(buildFastaViewer(value));
                    content.appendChild(attrRow);
                }
                continue;
            }

            const attrRow = document.createElement('div');
            attrRow.style.marginBottom = '4px';

            const keyLabel = document.createElement('div');
            keyLabel.style.fontSize = '10px';
            keyLabel.style.color = 'var(--text-secondary)';
            keyLabel.style.marginBottom = '1px';
            keyLabel.textContent = key;
            attrRow.appendChild(keyLabel);

            attrRow.appendChild(renderAttributeValue(value));
            content.appendChild(attrRow);
        }

        element.appendChild(content);
    }

    // Morph wiring: canvas ↔ window ↔ tray
    const title = attestation
        ? `${attestation.subjects?.join(', ') || '?'} is ${attestation.predicates?.join(', ') || '?'}`
        : 'Attestation';

    wireExpandToWindow({
        element,
        expandBtn,
        glyphId: glyph.id,
        title,
        symbol: AS,
        renderContent: () => buildAttestationContent(attestation, attrs),
        logLabel: 'AsGlyph',
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Created attestation glyph ${glyph.id} (attrs: ${hasContent})`);

    return element;
}

/**
 * Spawn an attestation glyph on the canvas from an attestation object.
 */
export function spawnAttestationGlyph(attestation: Attestation, mouseX?: number, mouseY?: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, '[AsGlyph] Cannot spawn: no canvas-content-layer found');
        return;
    }

    const glyphId = `as-${crypto.randomUUID()}`;
    const attrs = parseAttributes(attestation);

    const layerRect = contentLayer.getBoundingClientRect();
    const x = mouseX !== undefined ? Math.round(mouseX - layerRect.left + 20) : Math.round(window.innerWidth / 2 - 210);
    const y = mouseY !== undefined ? Math.round(mouseY - layerRect.top - 20) : Math.round(window.innerHeight / 2 - 150);

    const glyph: Glyph = {
        id: glyphId,
        title: 'Attestation',
        symbol: AS,
        x,
        y,
        content: JSON.stringify(attestation),
        renderContent: () => document.createElement('div'),
    };

    const entry = getGlyphTypeBySymbol(AS);
    if (!entry) {
        log.error(SEG.GLYPH, '[AsGlyph] AS not found in glyph registry');
        return;
    }

    const glyphElement = entry.render(glyph) as HTMLElement;
    contentLayer.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: AS,
        x,
        y,
        width: Math.round(rect.width) || 420,
        height: Math.round(rect.height) || (attrs ? 200 : 28),
        content: JSON.stringify(attestation),
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Spawned attestation glyph ${glyphId} at (${x}, ${y})`);
}

/**
 * Spawn an attestation directly as a window via glyphRun (tray→window path).
 * No canvas detour — the element starts as a tray dot and immediately morphs to window.
 * The window includes a "place on canvas" button for the window→canvas transition.
 */
export function spawnAttestationAsWindow(attestation: Attestation): void {
    const glyphId = `as-${attestation.id || crypto.randomUUID()}`;

    // Dedup: check if this attestation already exists in any state
    const existing = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (existing) {
        if (isInWindowState(existing)) {
            // Already a window — bring to front
            existing.style.zIndex = '1001';
            setTimeout(() => { existing.style.zIndex = '1000'; }, 2000);
        } else {
            // On canvas — fade the panel, pulse the glyph
            revealGlyphOnCanvas(existing);
        }
        log.debug(SEG.GLYPH, `[AsGlyph] Attestation ${glyphId} already exists, highlighting`);
        return;
    }
    if (glyphRun.has(glyphId)) {
        // In tray (minimized) — open as window
        glyphRun.openGlyph(glyphId);
        return;
    }

    const attrs = parseAttributes(attestation);
    const subjects = attestation.subjects?.join(', ') || '?';
    const predicates = attestation.predicates?.join(', ') || '?';
    const title = `${subjects} is ${predicates}`;

    glyphRun.add({
        id: glyphId,
        title,
        symbol: AS,
        initialWidth: '420px',
        initialHeight: attrs ? '300px' : '200px',
        onClose: () => {
            glyphRun.remove(glyphId);
            log.debug(SEG.GLYPH, `[AsGlyph] Closed window ${glyphId}`);
        },
        renderTitleBar: () => buildAttestationTitleBar(attestation, glyphId),
        renderContent: () => buildAttestationContent(attestation, attrs),
    });

    glyphRun.openGlyph(glyphId);
    log.debug(SEG.GLYPH, `[AsGlyph] Spawned attestation ${glyphId} as window`);
}

/**
 * Build the attestation title bar for the window manifestation.
 * Includes: AS symbol, triple text, place-on-canvas button, metadata pill.
 */
function buildAttestationTitleBar(attestation: Attestation, glyphId: string): HTMLElement {
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar glyph-title-bar--auto';
    titleBar.style.position = 'relative';

    const symbol = document.createElement('span');
    symbol.textContent = AS;
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = AZURE;
    titleBar.appendChild(symbol);

    const tripleText = renderTriple(attestation, {
        palette: { value: AZURE_VALUE, keyword: AZURE_KEYWORD },
        showWatcherEyes: true,
    });
    titleBar.appendChild(tripleText);

    // Place-on-canvas button
    const placeBtn = document.createElement('button');
    placeBtn.className = 'titlebar-btn';
    placeBtn.textContent = '\u2B07'; // ⬇
    placeBtn.title = 'Place on canvas';
    placeBtn.style.flexShrink = '0';
    placeBtn.style.marginLeft = 'auto';
    preventDrag(placeBtn);
    titleBar.appendChild(placeBtn);

    placeBtn.addEventListener('click', (e) => {
        // Stop propagation — glyphRun has a click handler on the element that would
        // re-trigger morphToWindow if the click bubbles up
        e.stopPropagation();
        const element = placeBtn.closest('[data-glyph-id]') as HTMLElement | null;
        if (!element) return;
        placeAttestationWindowOnCanvas(element, attestation, glyphId, placeBtn);
    });

    // Metadata pill
    const metaLines = buildMetaLines(attestation);
    if (metaLines.length > 0) {
        const pill = document.createElement('div');
        pill.className = 'as-meta-pill';

        const metaPopover = document.createElement('div');
        metaPopover.className = 'as-meta-popover';
        metaPopover.innerHTML = metaLines.join('<br>');

        pill.appendChild(metaPopover);
        titleBar.appendChild(pill);
    }

    return titleBar;
}

/**
 * Place an attestation window onto the canvas.
 * Transitions from tray-originated window to canvas-placed element.
 */
function placeAttestationWindowOnCanvas(
    element: HTMLElement,
    attestation: Attestation,
    glyphId: string,
    placeBtn: HTMLElement,
): void {
    if (!isInWindowState(element)) return;

    const canvasEl = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasEl) {
        log.warn(SEG.GLYPH, `[AsGlyph] No canvas workspace found, cannot place ${glyphId}`);
        return;
    }
    const canvasId = canvasEl.dataset.canvasId ?? 'canvas-workspace';
    const contentLayer = canvasEl.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, `[AsGlyph] No content layer in canvas ${canvasId}`);
        return;
    }

    // Capture window position before teardown
    const windowRect = element.getBoundingClientRect();
    const canvasRect = canvasEl.getBoundingClientRect();

    // Convert window screen position to canvas-local coordinates
    const relX = windowRect.left - canvasRect.left;
    const relY = windowRect.top - canvasRect.top;
    const canvasPos = screenToCanvas(canvasId, relX, relY);

    // Tear down window state
    teardownWindowDrag(element);
    const resizeObserver = (element as any).__resizeObserver as ResizeObserver | undefined;
    if (resizeObserver) {
        resizeObserver.disconnect();
        delete (element as any).__resizeObserver;
    }
    const titleBar = element.querySelector('.glyph-title-bar') as HTMLElement | null;
    if (titleBar) removeWindowControls(titleBar);

    // Unwrap .canvas-window-content if morphToWindow wrapped children
    const contentDiv = element.querySelector('.canvas-window-content');
    if (contentDiv) {
        while (contentDiv.firstChild) {
            element.appendChild(contentDiv.firstChild);
        }
        contentDiv.remove();
    }

    // Clear window state
    setWindowState(element, false);

    // Remove from body, clear all inline styles
    element.remove();
    element.style.cssText = '';

    // Untrack from glyphRun — element is leaving tray management for canvas.
    // Called while detached so glyphRun.remove()'s element.remove() is a no-op.
    // If the user later minimizes to tray, glyphRun.adopt() will re-add it.
    if (glyphRun.has(glyphId)) {
        glyphRun.remove(glyphId);
    }

    // Set canvas-placed positioning
    const width = 420;
    const attrs = parseAttributes(attestation);
    const height = attrs ? 200 : 28;
    element.style.position = 'absolute';
    element.style.left = `${Math.round(canvasPos.x)}px`;
    element.style.top = `${Math.round(canvasPos.y)}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;
    element.style.minWidth = '200px';
    element.classList.add('canvas-glyph', 'canvas-attestation-glyph');

    // Reparent to canvas
    contentLayer.appendChild(element);

    // Build glyph object for drag/resize handlers
    const title = `${attestation.subjects?.join(', ') || '?'} is ${attestation.predicates?.join(', ') || '?'}`;
    const glyph: Glyph = {
        id: glyphId,
        title,
        symbol: AS,
        x: Math.round(canvasPos.x),
        y: Math.round(canvasPos.y),
        content: JSON.stringify(attestation),
        renderContent: () => buildAttestationContent(attestation, attrs),
    };

    // Add drag/resize handlers
    if (titleBar) {
        const cleanupDrag = makeDraggable(element, titleBar, glyph, { logLabel: 'AsGlyph' });
        storeCleanup(element, cleanupDrag);
    }
    if (attrs) {
        const resizeHandle = document.createElement('div');
        resizeHandle.className = 'glyph-resize-handle';
        element.appendChild(resizeHandle);
        const cleanupResize = makeResizable(element, resizeHandle, glyph, { logLabel: 'AsGlyph' });
        storeCleanup(element, cleanupResize);
    }

    // Track in uiState
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: AS,
        x: Math.round(canvasPos.x),
        y: Math.round(canvasPos.y),
        width,
        height,
        content: JSON.stringify(attestation),
    });

    // Swap button to expand (canvas→window direction)
    placeBtn.textContent = '\u2B06'; // ⬆
    placeBtn.title = 'Expand to window';

    // Re-wire button for canvas→window morph
    const newBtn = placeBtn.cloneNode(true) as HTMLElement;
    placeBtn.replaceWith(newBtn);
    preventDrag(newBtn);

    wireExpandToWindow({
        element,
        expandBtn: newBtn,
        glyphId,
        title,
        symbol: AS,
        renderContent: () => buildAttestationContent(attestation, attrs),
        logLabel: 'AsGlyph',
        stopPropagation: true,
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Placed ${glyphId} on canvas at (${Math.round(canvasPos.x)}, ${Math.round(canvasPos.y)})`);
}

/**
 * Reveal a glyph on canvas by fading the panel and pulsing the glyph border.
 * Panel fades to near-transparent for 2.5s, glyph pulses for 1.2s.
 */
function revealGlyphOnCanvas(glyphElement: HTMLElement): void {
    // Fade any open panel to reveal the canvas behind it
    const panel = document.querySelector('[data-glyph-id="embeddings-glyph"]') as HTMLElement | null;
    if (panel) {
        panel.style.transition = 'opacity 200ms ease-out';
        panel.style.opacity = '0.1';
        setTimeout(() => {
            panel.style.transition = 'opacity 400ms ease-in';
            panel.style.opacity = '1';
        }, 2500);
    }

    // Pulse the glyph border after a short delay (let the panel fade first)
    setTimeout(() => {
        glyphElement.classList.add('glyph-pulse');
        glyphElement.addEventListener('animationend', () => {
            glyphElement.classList.remove('glyph-pulse');
        }, { once: true });
    }, 250);
}

/**
 * Build attestation content for tray restoration.
 */
function buildAttestationContent(
    attestation: Attestation | null,
    attrs: Record<string, unknown> | null,
): HTMLElement {
    const outer = document.createElement('div');
    const wrapper = document.createElement('div');
    wrapper.className = 'glyph-content';
    outer.appendChild(wrapper);

    if (attestation) {
        // Triple
        const triple = document.createElement('div');
        triple.style.padding = '8px';
        triple.style.fontSize = '12px';
        triple.style.fontFamily = 'monospace';
        triple.style.color = AZURE_VALUE;
        triple.style.wordBreak = 'break-word';
        const s = attestation.subjects?.join(', ') || 'N/A';
        const p = attestation.predicates?.join(', ') || 'N/A';
        const c = attestation.contexts?.join(', ') || 'N/A';
        triple.textContent = `${s} is ${p} of ${c}`;
        wrapper.appendChild(triple);

        // Metadata
        const metaLines = buildMetaLines(attestation);
        if (metaLines.length > 0) {
            const meta = document.createElement('div');
            meta.style.padding = '4px 8px';
            meta.style.fontSize = '11px';
            meta.style.color = 'var(--text-secondary)';
            meta.innerHTML = metaLines.join('<br>');
            wrapper.appendChild(meta);
        }

        // Attributes
        if (attrs) {
            const attrDiv = document.createElement('div');
            attrDiv.style.padding = '4px 8px';
            attrDiv.style.borderTop = '1px solid var(--border)';
            attrDiv.style.fontSize = '12px';
            attrDiv.style.fontFamily = 'monospace';
            for (const [key, value] of Object.entries(attrs)) {
                // FASTA: skip 'format' key, render 'data' with viewer
                if (isFastaAttribute(attrs, key)) {
                    if (key === 'data' && typeof value === 'string') {
                        const row = document.createElement('div');
                        row.style.marginBottom = '4px';
                        row.appendChild(buildFastaViewer(value));
                        attrDiv.appendChild(row);
                    }
                    continue;
                }

                const row = document.createElement('div');
                row.style.marginBottom = '4px';
                const keyEl = document.createElement('div');
                keyEl.style.fontSize = '10px';
                keyEl.style.color = 'var(--text-secondary)';
                keyEl.textContent = key;
                row.appendChild(keyEl);

                row.appendChild(renderAttributeValue(value));
                attrDiv.appendChild(row);
            }
            wrapper.appendChild(attrDiv);
        }
    }

    return outer;
}

function formatTimestamp(value: unknown): string {
    if (!value) return 'N/A';
    try {
        if (typeof value === 'string') {
            return new Date(value).toLocaleString();
        }
        if (typeof value === 'number') {
            // Unix seconds or milliseconds — if < 1e12, assume seconds
            const ms = value < 1e12 ? value * 1000 : value;
            return new Date(ms).toLocaleString();
        }
        return String(value);
    } catch {
        return String(value);
    }
}
