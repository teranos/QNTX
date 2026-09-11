/**
 * Renders the "subject is predicate of context" triple for attestations.
 *
 * Used by ax-glyph (result items) and attestation-glyph (title bars).
 * Watcher eye (⏿) is opt-in via watcherMap parameter.
 *
 * renderTriple is three things, and the predicate part is used on its own
 * wherever we already show the predicate.
 */

import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { AX, Watcher } from '@generated/sym.js';
import { getWatchersByPredicate, eyeStyle } from '../../watcher-predicates';
import { preventDrag } from '@qntx/glyphs';
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
    /** Prefix with "as" keyword (makes attestation store query explicit) */
    showAsPrefix?: boolean;
    /**
     * When provided, "as", "is", and "of" keywords become clickable.
     * The callback receives the AX query fragment for that keyword:
     *   "as" → subject (e.g. "batch")
     *   "is" → "is crawl-timeout"
     *   "of" → "of levi:batch"
     */
    onKeywordClick?: (axQuery: string, event: MouseEvent) => void;
}

export interface PredicateOptions {
    /** Tag for the element */
    tag?: 'div' | 'span';
    /** Font size. Inherited when omitted. */
    fontSize?: string;
    /** Colour of the predicate text */
    color: string;
    /** Shown in place of the joined predicates. The predicates stay the identity. */
    label?: string;
    /** Pass to show ⏿ when the predicate is watched; omit for no eye */
    showWatcherEyes?: boolean;
    /** When provided, the predicate becomes pressable and hands back what it is */
    onPress?: (predicates: string[], event: MouseEvent) => void;
}

/**
 * Build the predicate part on its own. Carries its ax segment and its watcher
 * eye, which is what makes it the same thing here as inside a triple.
 */
export function renderPredicate(predicates: string[], options: PredicateOptions): HTMLElement {
    const joined = predicates.join(', ') || 'N/A';
    const style: Partial<CSSStyleDeclaration> = { color: options.color };
    if (options.fontSize !== undefined) style.fontSize = options.fontSize;
    if (options.onPress) style.cursor = 'pointer';

    const span: HTMLElement = el(options.tag || 'span', { text: options.label ?? joined, style });
    span.dataset.axSegment = `is ${joined}`;

    if (options.showWatcherEyes) {
        const eye = buildWatcherEye(predicates);
        if (eye) span.appendChild(eye);
    }

    if (options.onPress) {
        preventDrag(span);
        span.addEventListener('click', (e) => {
            e.stopPropagation();
            options.onPress!(predicates, e);
        });
    }

    return span;
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
    const clickable = !!options.onKeywordClick;

    const wrapper = el(tag, {
        style: { fontSize, fontFamily: 'monospace', lineHeight: '1.4', wordBreak: 'break-word', overflowWrap: 'break-word' },
    });

    const keywordStyle = clickable
        ? { color: keyword, cursor: 'pointer', transition: 'color 0.1s' }
        : { color: keyword };

    const wireKeyword = (span: HTMLElement, query: string) => {
        if (!options.onKeywordClick) return;
        preventDrag(span);
        span.style.position = 'relative';
        let axHint: HTMLElement | null = null;
        let hintTimer: ReturnType<typeof setTimeout> | null = null;

        span.addEventListener('mouseenter', () => {
            span.style.color = value;
            // Fade in AX symbol to the left after 400ms
            if (!axHint) {
                axHint = el('span', {
                    text: AX,
                    style: {
                        position: 'absolute', right: '100%', top: '0',
                        color: keyword, fontSize: 'inherit', fontFamily: 'inherit',
                        opacity: '0', transition: 'opacity 0.3s ease',
                        pointerEvents: 'none', whiteSpace: 'nowrap',
                    },
                });
                span.appendChild(axHint);
            }
            hintTimer = setTimeout(() => {
                if (axHint) axHint.style.opacity = '1';
            }, 400);
        });
        span.addEventListener('mouseleave', () => {
            span.style.color = keyword;
            if (hintTimer) { clearTimeout(hintTimer); hintTimer = null; }
            if (axHint) { axHint.remove(); axHint = null; }
        });
        span.addEventListener('click', (e) => {
            e.stopPropagation();
            if (hintTimer) { clearTimeout(hintTimer); hintTimer = null; }
            if (axHint) { axHint.remove(); axHint = null; }
            options.onKeywordClick!(query, e);
        });
    };

    if (options.showAsPrefix) {
        const asSpan = el('span', { text: 'as ', style: keywordStyle });
        wireKeyword(asSpan, subjects);
        wrapper.appendChild(asSpan);
    }

    const subjectSpan = el('span', { text: subjects, style: { color: value } });
    subjectSpan.dataset.axSegment = subjects;
    const isSpan = el('span', { text: ' is ', style: keywordStyle });
    wireKeyword(isSpan, `is ${predicates}`);
    const predSpan = renderPredicate(attestation.predicates || [], {
        color: value,
        showWatcherEyes: options.showWatcherEyes,
    });

    wrapper.append(subjectSpan, isSpan, predSpan);

    const ofSpan = el('span', { text: ' of ', style: keywordStyle });
    wireKeyword(ofSpan, `of ${contexts}`);
    const ctxSpan = el('span', { text: contexts, style: { color: value } });
    ctxSpan.dataset.axSegment = `of ${contexts}`;
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
