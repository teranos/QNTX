/**
 * A segment of the triple, as an element in its own right.
 *
 * A segment has three layers (docs/SYMBOLS.md): the seg is the grammatical
 * unit, the sym is how it looks, the element is how you interact with it. The
 * first two were always there. This is the third.
 *
 * Clicking a segment spawns its element, the same way clicking an attestation
 * spawns the attestation element. Subject draws +, predicate =, context ∈ — the
 * marks the grammar already gives them.
 *
 * What one shows is the other segments and the actors, folded out of what is
 * filed under it, the way a sigma folds its observations. Which segments those
 * are is the only thing that differs between them, so it is the only thing a
 * Segment says.
 */

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from '../../client';
import { log, SEG } from '../../logger';
import { el } from '../../html-utils';
import { renderSparklines, windowOf, type Seen, type NameCell } from '../sparkline';
import { parseAttributes } from './attestation-attrs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';

/** How many attestations the sections are folded out of. */
export const READ_LIMIT = 500;

/** One section: a field of the attestations, over time. */
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
    /** Prefixes the element id, so the id says which segment it is about */
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

/** One element per value of one segment, so its id says what it is about. */
export function segmentElementId(segment: Segment, value: string): string {
    return `${segment.kind}-${value}`;
}

/** When each value of one field was seen across attestations. */
export function seenOf(
    attestations: Attestation[],
    pick: (a: Attestation) => string[] | undefined,
): Seen[] {
    const times = new Map<string, number[]>();
    for (const att of attestations) {
        for (const value of pick(att) ?? []) {
            const at = times.get(value) ?? [];
            at.push(att.timestamp);
            times.set(value, at);
        }
    }
    return Array.from(times, ([name, t]) => ({ name, times: t }));
}

/** Per attribute key: the values seen under it, and when. */
export function attributesSeen(attestations: Attestation[]): Array<{ key: string; items: Seen[] }> {
    const byKey = new Map<string, Map<string, number[]>>();

    for (const att of attestations) {
        const attrs = parseAttributes(att);
        if (!attrs) continue;
        for (const [key, value] of Object.entries(attrs)) {
            const shown = typeof value === 'string' ? value : JSON.stringify(value);
            if (shown === undefined || shown === '') continue;
            let values = byKey.get(key);
            if (!values) {
                values = new Map();
                byKey.set(key, values);
            }
            const at = values.get(shown) ?? [];
            at.push(att.timestamp);
            values.set(shown, at);
        }
    }

    const out = Array.from(byKey, ([key, values]) => ({
        key,
        items: Array.from(values, ([name, t]) => ({ name, times: t })),
    }));
    out.sort((a, b) => a.key.localeCompare(b.key));
    return out;
}

/** Exported for tests: the sections a segment shows, in the order it shows them. */
export function renderSegmentStats(
    container: HTMLElement,
    segment: Segment,
    value: string,
    attestations: Attestation[],
    now: number = Date.now(),
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

    // One window for the whole element, so every line is drawn on the same axis.
    const w = windowOf(attestations.map((a) => a.timestamp), now);

    for (const section of segment.sections) {
        const box = el('div', { class: section.className, style: { marginBottom: '18px' } });
        renderSparklines(box, section.label, seenOf(attestations, section.pick), w, section.cell);
        container.appendChild(box);
    }

    const attributes = el('div', { class: `${segment.kind}-attributes` });
    const seen = attributesSeen(attestations);
    if (seen.length === 0) {
        renderSparklines(attributes, 'Attributes', [], w);
    }
    for (const { key, items } of seen) {
        const section = el('div', { style: { marginBottom: '12px' } });
        renderSparklines(section, key, items, w);
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
 * Opens one value of one segment as its own element. An element renders its content once
 * and keeps that element for its lifetime, so what it is about is fixed when it
 * is made (stand-activity-element.ts).
 */
export function openSegmentElement(segment: Segment, value: string): void {
    const elementId = segmentElementId(segment, value);
    if (tray.has(elementId)) {
        tray.open(elementId);
        return;
    }

    tray.add({
        id: elementId,
        title: value,
        symbol: segment.symbol,
        onClose: () => { tray.remove(elementId); },
        renderContent: () => {
            const content = el('div', {
                class: `${segment.kind}-element-content`,
                style: { fontFamily: FONT, fontSize: SIZE, padding: EDGE },
            });

            content.appendChild(el('div', {
                class: 'element-loading',
                text: segment.reading(value),
            }));

            attestationsOf(segment, value)
                .then((attestations) => { renderSegmentStats(content, segment, value, attestations); })
                .catch((err: unknown) => {
                    log.warn(SEG.ELEMENT, `[SegmentElement] could not read what is filed under ${value}:`, err);
                    content.replaceChildren(el('div', {
                        text: err instanceof Error ? err.message : String(err),
                        style: { color: MUTE },
                    }));
                });

            return content;
        },
    } satisfies Element);

    tray.open(elementId);
}
