/**
 * Attestation attribute rendering — shared by attestation glyph (+),
 * triplet glyph (⫶), and any future container that displays attestation attributes.
 *
 * Handles: JSON arrays/objects, FASTA, structure viewers, AlphaFold CIF,
 * amino acid sequences, HTML content, URL pills, nested objects, array paging.
 */

import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { isFastaAttribute, buildFastaViewer, isAminoAcidSequence, renderAminoAcidSequence } from './bioviz/fasta-renderer';
import { isStructureItem, buildStructureViewer } from './bioviz/structure-renderer';
import { buildAlphaFoldViewer } from './bioviz/alphafold-viewer';
import { isPdbData, buildPdbViewer } from './bioviz/pdb-viewer';
import { isGenbankData, buildGenbankViewer } from './bioviz/genbank-renderer';
import { preventDrag } from '@qntx/glyphs';

// Muted azure palette — shared with attestation-glyph.ts
export const AZURE = '#8a969b';
export const AZURE_KEYWORD = '#6b7175';
export const AZURE_VALUE = '#a0a8ad';
export const AZURE_BORDER = 'rgba(255,255,255,0.06)';

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

function isAlphaFoldCif(url: string): boolean {
    return url.includes('alphafold.ebi.ac.uk/files/AF-') && url.endsWith('.cif');
}

function buildUrlPill(key: string, url: string): HTMLElement {
    const ext = getExtension(url);
    const fileType = EXT_TYPE[ext] || 'Unknown';
    const color = TYPE_COLOR[fileType];
    const lastSlash = url.lastIndexOf('/');
    const filename = lastSlash >= 0 ? url.slice(lastSlash + 1) : url;

    const pill = document.createElement('a');
    pill.href = url;
    pill.target = '_blank';
    pill.rel = 'noopener';
    pill.title = url;

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
        const rec = item as Record<string, unknown>;
        // Structure item: render arc diagram above key-value pairs
        if (isStructureItem(rec)) {
            container.appendChild(buildStructureViewer(
                rec['sequence'] as string,
                rec['structure'] as string,
            ));
        }

        const entries = Object.entries(rec);
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

            // AlphaFold CIF viewers render as cards below the pill group
            for (const [k, url] of urlEntries) {
                if (!isAlphaFoldCif(url)) continue;
                const fname = url.split('/').pop() || '';
                const parts = fname.split('-');
                const fIdx = parts.findIndex(p => p.startsWith('F'));
                if (fIdx < 0 || parts[0] !== 'AF') continue;
                const structureId = parts.slice(0, fIdx + 1).join('-');
                const accession = parts.slice(1, fIdx).join('-');

                const card = document.createElement('div');
                card.style.maxWidth = '66%';
                card.style.marginTop = '4px';
                card.style.borderRadius = '4px';
                card.style.overflow = 'hidden';
                card.style.backgroundColor = 'rgba(90,130,138,0.25)';

                const labelRow = document.createElement('a');
                labelRow.href = url;
                labelRow.target = '_blank';
                labelRow.rel = 'noopener';
                labelRow.style.display = 'flex';
                labelRow.style.alignItems = 'center';
                labelRow.style.gap = '4px';
                labelRow.style.padding = '2px 6px';
                labelRow.style.color = AZURE_VALUE;
                labelRow.style.fontSize = '10px';
                labelRow.style.fontFamily = 'monospace';
                labelRow.style.textDecoration = 'none';
                labelRow.style.backgroundColor = 'transparent';

                const keySpan = document.createElement('span');
                keySpan.style.opacity = '0.5';
                keySpan.textContent = k;
                const nameSpan = document.createElement('span');
                nameSpan.textContent = fname;
                labelRow.append(keySpan, nameSpan);
                card.appendChild(labelRow);

                card.appendChild(buildAlphaFoldViewer(structureId, accession, url));
                preventDrag(card);
                container.appendChild(card);
            }
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
 * Render attributes for a single attestation with full rich rendering
 * (FASTA viewer, structure viewer, amino acid sequences, HTML, nested objects, arrays with pager).
 * Reusable by triplet-glyph and any other container that shows attestation attributes.
 */
export function renderAttestationAttrs(attrs: Record<string, unknown>): HTMLElement {
    const attrDiv = document.createElement('div');
    attrDiv.style.fontSize = '12px';
    attrDiv.style.fontFamily = 'monospace';
    for (const [key, value] of Object.entries(attrs)) {
        if (isFastaAttribute(attrs, key)) {
            if (key === 'data' && typeof value === 'string') {
                const row = document.createElement('div');
                row.style.marginBottom = '4px';
                row.appendChild(buildFastaViewer(value));
                attrDiv.appendChild(row);
            }
            continue;
        }

        // GenBank format: render linear cassette map
        if (typeof value === 'string' && isGenbankData(value)) {
            const row = document.createElement('div');
            row.style.marginBottom = '4px';
            const keyEl = document.createElement('div');
            keyEl.style.fontSize = '10px';
            keyEl.style.color = 'var(--text-secondary)';
            keyEl.style.marginBottom = '1px';
            keyEl.textContent = key;
            row.appendChild(keyEl);
            row.appendChild(buildGenbankViewer(value));
            attrDiv.appendChild(row);
            continue;
        }

        // Inline PDB data: render 3D viewer instead of raw text
        if (typeof value === 'string' && isPdbData(value)) {
            const row = document.createElement('div');
            row.style.marginBottom = '4px';
            const keyEl = document.createElement('div');
            keyEl.style.fontSize = '10px';
            keyEl.style.color = 'var(--text-secondary)';
            keyEl.style.marginBottom = '1px';
            keyEl.textContent = key;
            row.appendChild(keyEl);
            row.appendChild(buildPdbViewer(value, key));
            attrDiv.appendChild(row);
            continue;
        }

        const row = document.createElement('div');
        row.style.marginBottom = '4px';
        const keyEl = document.createElement('div');
        keyEl.style.fontSize = '10px';
        keyEl.style.color = 'var(--text-secondary)';
        keyEl.style.marginBottom = '1px';
        keyEl.textContent = key;
        row.appendChild(keyEl);

        row.appendChild(renderAttributeValue(value));
        attrDiv.appendChild(row);
    }
    return attrDiv;
}

/**
 * Parse attributes from attestation, returns non-empty object or null.
 */
export function parseAttributes(attestation: Attestation): Record<string, unknown> | null {
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
