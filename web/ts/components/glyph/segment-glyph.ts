/**
 * A segment of the triple, as a glyph in its own right.
 *
 * A segment has three layers (docs/SYMBOLS.md): the seg is the grammatical
 * unit, the sym is how it looks, the glyph is how you interact with it. The
 * first two were always there. This is the third.
 *
 * Clicking a segment spawns its glyph, the same way clicking an attestation
 * spawns the attestation glyph. Subject draws +, predicate =, context ∈ — the
 * marks the grammar already gives them.
 *
 * What one shows is the other segments and the actors, folded out of what is
 * filed under it, the way a sigma folds its observations. Which segments those
 * are is the only thing that differs between them, so it is the only thing a
 * Segment says.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from '../../client';
import { log, SEG } from '../../logger';
import { el } from '../../html-utils';
import { renderTally, type TallyItem, type NameCell } from '../tally';
import { parseAttributes } from './attestation-attrs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';

/** How many attestations the sections are folded out of. */
export const READ_LIMIT = 500;

/** One section: a field of the attestations, counted. */
export interface SegmentSection {
    /** Heading, and the class its rows are found under */
    label: string;
    className: string;
    pick: (a: Attestation) => string[] | undefined;
    /** How a name is drawn. Text unless the segment makes it pressable. */
    cell?: NameCell;
}

/** What one segment is: its mark, how the store is asked, and what it shows. */
export interface Segment {
    /** Prefixes the glyph id, so the id says which segment it is about */
    kind: string;
    symbol: string;
    /** The query key the node knows this segment by */
    param: string;
    /** Said above the sections, after the count */
    filed: (count: number) => string;
    /** While the node is being read */
    reading: (value: string) => string;
    sections: SegmentSection[];
}

/** One glyph per value of one segment, so its id says what it is about. */
export function segmentGlyphId(segment: Segment, value: string): string {
    return `${segment.kind}-${value}`;
}

/** Count the values of one field across attestations, most-seen first. */
export function tallyOf(
    attestations: Attestation[],
    pick: (a: Attestation) => string[] | undefined,
): TallyItem[] {
    const counts = new Map<string, number>();
    for (const att of attestations) {
        for (const value of pick(att) ?? []) {
            counts.set(value, (counts.get(value) ?? 0) + 1);
        }
    }
    const items = Array.from(counts, ([name, count]) => ({ name, count }));
    items.sort((a, b) => (b.count - a.count) || a.name.localeCompare(b.name));
    return items;
}

/** One tally per attribute key: the values seen under it and how often. */
export function attributeTallies(attestations: Attestation[]): Array<{ key: string; items: TallyItem[] }> {
    const byKey = new Map<string, Map<string, number>>();

    for (const att of attestations) {
        const attrs = parseAttributes(att);
        if (!attrs) continue;
        for (const [key, value] of Object.entries(attrs)) {
            const shown = typeof value === 'string' ? value : JSON.stringify(value);
            if (shown === undefined || shown === '') continue;
            let counts = byKey.get(key);
            if (!counts) {
                counts = new Map();
                byKey.set(key, counts);
            }
            counts.set(shown, (counts.get(shown) ?? 0) + 1);
        }
    }

    const out = Array.from(byKey, ([key, counts]) => {
        const items = Array.from(counts, ([name, count]) => ({ name, count }));
        items.sort((a, b) => (b.count - a.count) || a.name.localeCompare(b.name));
        return { key, items };
    });
    out.sort((a, b) => a.key.localeCompare(b.key));
    return out;
}

/** Exported for tests: the sections a segment shows, in the order it shows them. */
export function renderSegmentStats(
    container: HTMLElement,
    segment: Segment,
    value: string,
    attestations: Attestation[],
): void {
    container.replaceChildren();

    const title = el('div', {
        text: value,
        style: {
            fontSize: '15px', marginBottom: '2px',
            overflowWrap: 'break-word', wordBreak: 'break-word',
        },
    });
    container.appendChild(title);

    const summary = el('div', {
        text: `${attestations.length} ${segment.filed(attestations.length)}`,
        style: { color: MUTE, marginBottom: '14px' },
    });
    container.appendChild(summary);

    for (const section of segment.sections) {
        const box = el('div', { class: section.className, style: { marginBottom: '18px' } });
        renderTally(box, section.label, tallyOf(attestations, section.pick), section.cell);
        container.appendChild(box);
    }

    const attributes = el('div', { class: `${segment.kind}-attributes` });
    const tallies = attributeTallies(attestations);
    if (tallies.length === 0) {
        renderTally(attributes, 'Attributes', []);
    }
    for (const { key, items } of tallies) {
        const section = el('div', { style: { marginBottom: '12px' } });
        renderTally(section, key, items);
        attributes.appendChild(section);
    }
    container.appendChild(attributes);
}

/** What is filed under one value of one segment. */
async function attestationsOf(segment: Segment, value: string): Promise<Attestation[]> {
    return await apiJson<Attestation[]>(
        `/api/attestations?${segment.param}=${encodeURIComponent(value)}&limit=${READ_LIMIT}`);
}

/**
 * Opens one value of one segment as its own glyph. A glyph renders its content once
 * and keeps that element for its lifetime, so what it is about is fixed when it
 * is made (stand-activity-glyph.ts).
 */
export function openSegmentGlyph(segment: Segment, value: string): void {
    const glyphId = segmentGlyphId(segment, value);
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    glyphRun.add({
        id: glyphId,
        title: value,
        symbol: segment.symbol,
        onClose: () => { glyphRun.remove(glyphId); },
        renderContent: () => {
            const content = el('div', {
                class: `${segment.kind}-glyph-content`,
                style: { fontFamily: FONT, fontSize: SIZE, padding: EDGE },
            });

            content.appendChild(el('div', {
                class: 'glyph-loading',
                text: segment.reading(value),
            }));

            attestationsOf(segment, value)
                .then((attestations) => { renderSegmentStats(content, segment, value, attestations); })
                .catch((err: unknown) => {
                    log.warn(SEG.GLYPH, `[SegmentGlyph] could not read what is filed under ${value}:`, err);
                    content.replaceChildren(el('div', {
                        text: err instanceof Error ? err.message : String(err),
                        style: { color: MUTE },
                    }));
                });

            return content;
        },
        initialWidth: '560px',
        initialHeight: '460px',
    } satisfies Glyph);

    glyphRun.openGlyph(glyphId);
}
