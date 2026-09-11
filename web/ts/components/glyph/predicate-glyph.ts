/**
 * Predicate Glyph (=) — the predicate as a thing in its own right.
 *
 * Clicking a predicate spawns the predicate glyph, the same way clicking an
 * attestation spawns the attestation glyph.
 *
 * What it shows, in order: related subjects, related contexts and actors, and
 * attributes — folded out of the attestations filed under this predicate, the
 * way a sigma folds its observations.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { IS } from '../../sym';
import { apiJson } from '../../client';
import { log, SEG } from '../../logger';
import { el } from '../../html-utils';
import { renderTally, type TallyItem } from '../tally';
import { parseAttributes } from './attestation-attrs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';

/** How many attestations the sections are folded out of. */
const READ_LIMIT = 500;

/** One glyph per predicate, so its id says which predicate it is about. */
export function predicateGlyphId(predicate: string): string {
    return `predicate-${predicate}`;
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

/** Exported for tests: the three sections, in the order they are asked for. */
export function renderPredicateStats(container: HTMLElement, predicate: string, attestations: Attestation[]): void {
    container.replaceChildren();

    const title = el('div', {
        text: predicate,
        style: {
            fontSize: '15px', marginBottom: '2px',
            overflowWrap: 'break-word', wordBreak: 'break-word',
        },
    });
    container.appendChild(title);

    const summary = el('div', {
        text: `${attestations.length} filed under this predicate`,
        style: { color: MUTE, marginBottom: '14px' },
    });
    container.appendChild(summary);

    const subjects = el('div', { class: 'predicate-subjects', style: { marginBottom: '18px' } });
    renderTally(subjects, 'Related subjects', tallyOf(attestations, (a) => a.subjects));
    container.appendChild(subjects);

    const contexts = el('div', { class: 'predicate-contexts', style: { marginBottom: '18px' } });
    renderTally(contexts, 'Related contexts', tallyOf(attestations, (a) => a.contexts));
    container.appendChild(contexts);

    const actors = el('div', { class: 'predicate-actors', style: { marginBottom: '18px' } });
    renderTally(actors, 'Related actors', tallyOf(attestations, (a) => a.actors));
    container.appendChild(actors);

    const attributes = el('div', { class: 'predicate-attributes' });
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

/** What is filed under this predicate. */
async function attestationsOf(predicate: string): Promise<Attestation[]> {
    return await apiJson<Attestation[]>(
        `/api/attestations?predicate=${encodeURIComponent(predicate)}&limit=${READ_LIMIT}`);
}

/**
 * Opens one predicate as its own glyph. A glyph renders its content once and
 * keeps that element for its lifetime, so the predicate it is about is fixed
 * when it is made (stand-activity-glyph.ts).
 */
export function openPredicateGlyph(predicate: string): void {
    const glyphId = predicateGlyphId(predicate);
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    glyphRun.add({
        id: glyphId,
        title: predicate,
        symbol: IS,
        onClose: () => { glyphRun.remove(glyphId); },
        renderContent: () => {
            const content = el('div', {
                class: 'predicate-glyph-content',
                style: { fontFamily: FONT, fontSize: SIZE, padding: EDGE },
            });

            content.appendChild(el('div', {
                class: 'glyph-loading',
                text: `Reading what is filed under ${predicate}…`,
            }));

            attestationsOf(predicate)
                .then((attestations) => { renderPredicateStats(content, predicate, attestations); })
                .catch((err: unknown) => {
                    log.warn(SEG.GLYPH, `[PredicateGlyph] could not read what is filed under ${predicate}:`, err);
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
