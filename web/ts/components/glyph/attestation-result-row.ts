/**
 * The result row: one attestation, as a row you can look at and open.
 *
 * ax-glyph and semantic-glyph each grew their own copy — same tint, same
 * radius, same double-click to spawn — and drifted the way two copies drift.
 */

import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { el } from '../../html-utils';
import { spawnAttestationGlyph } from './attestation-glyph';

/** The green a result row is, wherever it is shown. */
export const RESULT_ROW_TINT = 'rgba(31, 61, 31, 0.35)';

/** The palette renderTriple is given inside a result row. */
export const RESULT_ROW_PALETTE = { value: '#d4f0d4', keyword: '#6b7b6b' };

export interface ResultRowOptions {
    /** Distinguishes one surface's rows from another's, for CSS and tests. */
    className: string;
    /** What the row says. The caller builds it, because that is what differs. */
    body: HTMLElement;
    /** Hover text. The ASUID when there is nothing better to say. */
    tooltip?: string;
    /** Sits at the end of the row — a score, a time. */
    trailing?: HTMLElement;
    padding?: string;
    marginBottom?: string;
}

/**
 * Build one result row. Double-click spawns the attestation glyph, which is
 * how a row stops being a summary and becomes the thing itself.
 */
export function attestationResultRow(attestation: Attestation, options: ResultRowOptions): HTMLElement {
    const item = el('div', {
        class: `${options.className} has-tooltip`,
        style: {
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: options.padding || '8px',
            marginBottom: options.marginBottom || '4px',
            backgroundColor: RESULT_ROW_TINT,
            borderRadius: '2px',
            cursor: 'pointer',
        },
    });

    if (attestation.id) {
        item.dataset.attestationId = attestation.id;
    }
    // The whole attestation rides on the row, so opening it needs no lookup.
    item.dataset.attestation = JSON.stringify(attestation);
    item.dataset.tooltip = options.tooltip || attestation.id || 'unknown';

    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnAttestationGlyph(attestation, e.clientX, e.clientY);
    });

    options.body.style.flex = '1';
    item.appendChild(options.body);
    if (options.trailing) {
        item.appendChild(options.trailing);
    }
    return item;
}
