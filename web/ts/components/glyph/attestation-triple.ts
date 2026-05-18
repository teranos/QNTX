/**
 * Renders the "subject is predicate of context" triple for attestations.
 *
 * Used by ax-glyph (result items) and attestation-glyph (title bars).
 * Watcher eye (⏿) is opt-in via watcherMap parameter.
 */

import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Watcher } from '@generated/sym.js';
import { getWatchersByPredicate, eyeStyle } from '../../watcher-predicates';
import { el } from '../../html-utils';

export interface TriplePalette {
    value: string;
    keyword: string;
}

export interface TripleOptions {
    /** Tag for the wrapper element */
    tag?: 'div' | 'span';
    /** Font size (default: '12px') */
    fontSize?: string;
    /** Color palette for value spans and keyword spans */
    palette: TriplePalette;
    /** Pass to show ⏿ next to watched predicates; omit for no eyes */
    showWatcherEyes?: boolean;
}

/**
 * Build a "subject is predicate of context" element from an attestation.
 */
export function renderTriple(attestation: Attestation, options: TripleOptions): HTMLElement {
    const subjects = attestation.subjects?.join(', ') || 'N/A';
    const predicates = attestation.predicates?.join(', ') || 'N/A';
    const contexts = attestation.contexts?.join(', ') || 'N/A';
    const tag = options.tag || 'span';
    const fontSize = options.fontSize || '12px';
    const { value, keyword } = options.palette;

    const wrapper = el(tag, {
        style: { fontSize, fontFamily: 'monospace', lineHeight: '1.4', wordBreak: 'break-word', overflowWrap: 'break-word' },
    });

    const subjectSpan = el('span', { text: subjects, style: { color: value } });
    const isSpan = el('span', { text: ' is ', style: { color: keyword } });
    const predSpan = el('span', { text: predicates, style: { color: value } });

    wrapper.append(subjectSpan, isSpan, predSpan);

    // Opt-in watcher eye after predicate
    if (options.showWatcherEyes) {
        const eyeSpan = buildWatcherEye(attestation.predicates || []);
        if (eyeSpan) wrapper.append(eyeSpan);
    }

    const ofSpan = el('span', { text: ' of ', style: { color: keyword } });
    const ctxSpan = el('span', { text: contexts, style: { color: value } });
    wrapper.append(ofSpan, ctxSpan);

    return wrapper;
}

function buildWatcherEye(predicates: string[]): HTMLSpanElement | null {
    const watcherMap = getWatchersByPredicate();
    const watched = predicates.filter(p => watcherMap.has(p));
    if (watched.length === 0) return null;

    const allInfos = watched.map(p => watcherMap.get(p)!);
    const allNames = allInfos.flatMap(i => i.names);
    const totalFires = allInfos.reduce((sum, i) => sum + i.totalFires, 0);
    const s = eyeStyle({ names: allNames, totalFires });

    const span = el('span', {
        text: Watcher.repeat(allNames.length),
        style: { color: s.color, textShadow: s.shadow, cursor: 'default', marginLeft: '3px' },
    });
    span.title = allNames.join(', ');
    return span;
}
