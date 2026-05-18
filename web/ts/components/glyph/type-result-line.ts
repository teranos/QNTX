/**
 * Type Result Line — renders type attestations grouped by subject
 *
 * Type attestations are always subject-grouped: multiple actors attest
 * "[subject] is type" for the same subject. Instead of rendering each
 * individually, we consolidate them into a single result line per subject
 * showing the turnstile symbol, type color, label, and field count.
 */

import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Type } from '@generated/sym.js';
import { el } from '../../html-utils';

/** Check if an attestation is a type attestation */
export function isTypeAttestation(attestation: Attestation): boolean {
    return attestation.predicates?.[0] === 'type';
}

interface TypeGroup {
    subject: string;
    attestations: Attestation[];
    label: string;
    color: string;
    richFieldCount: number;
    arrayFieldCount: number;
    deprecated: boolean;
}

/** Parse type attributes from an attestation */
function parseTypeAttrs(attestation: Attestation): Record<string, unknown> | null {
    if (!attestation.attributes) return null;
    if (typeof attestation.attributes === 'string') {
        try { return JSON.parse(attestation.attributes as string); } catch { return null; }
    }
    return attestation.attributes as Record<string, unknown>;
}

/**
 * Group type attestations by subject. Takes the latest attestation's
 * attributes as the canonical view (attestations come sorted by timestamp DESC).
 */
export function groupTypeAttestations(attestations: Attestation[]): TypeGroup[] {
    const groups = new Map<string, TypeGroup>();

    for (const att of attestations) {
        const subject = att.subjects?.[0];
        if (!subject) continue;

        if (groups.has(subject)) {
            groups.get(subject)!.attestations.push(att);
            continue;
        }

        const attrs = parseTypeAttrs(att);
        const richFields = attrs?.rich_string_fields;
        const arrayFields = attrs?.array_fields;

        groups.set(subject, {
            subject,
            attestations: [att],
            label: (attrs?.display_label as string) || subject,
            color: (attrs?.display_color as string) || '#666666',
            richFieldCount: Array.isArray(richFields) ? richFields.length : 0,
            arrayFieldCount: Array.isArray(arrayFields) ? arrayFields.length : 0,
            deprecated: attrs?.deprecated === true,
        });
    }

    return Array.from(groups.values());
}

/** Render a grouped type as a single result line */
export function renderTypeResultLine(group: TypeGroup): HTMLElement {
    const item = el('div', {
        class: 'ax-glyph-result-item has-tooltip',
        style: {
            padding: '8px', marginBottom: '4px',
            backgroundColor: 'rgba(60, 50, 80, 0.35)',
            borderRadius: '2px', cursor: 'pointer',
        },
    });

    // Store first attestation for tooltip/spawn
    item.dataset.attestation = JSON.stringify(group.attestations[0]);
    item.dataset.tooltip = group.attestations[0].id || group.subject;

    const text = el('div', {
        style: {
            fontSize: '11px', fontFamily: 'monospace',
            wordBreak: 'break-word', overflowWrap: 'break-word',
        },
    });

    // Turnstile symbol in the type's color
    const turnstile = el('span', {
        text: Type + ' ',
        style: { color: group.color, fontWeight: 'bold' },
    });

    // Type name
    const nameSpan = el('span', {
        text: group.subject,
        style: { color: '#e0d0f0' },
    });

    text.append(turnstile, nameSpan);

    // Label (if different from name)
    if (group.label !== group.subject) {
        const sep = el('span', { text: ' · ', style: { color: '#7a6a8a' } });
        const labelSpan = el('span', {
            text: group.label,
            style: { color: '#c0b0d0' },
        });
        text.append(sep, labelSpan);
    }

    // Field count
    const totalFields = group.richFieldCount + group.arrayFieldCount;
    if (totalFields > 0) {
        const sep = el('span', { text: ' · ', style: { color: '#7a6a8a' } });
        const fieldSpan = el('span', {
            text: `${totalFields} field${totalFields !== 1 ? 's' : ''}`,
            style: { color: '#7a6a8a' },
        });
        text.append(sep, fieldSpan);
    }

    // Attestation count (how many actors attested this type)
    if (group.attestations.length > 1) {
        const sep = el('span', { text: ' · ', style: { color: '#7a6a8a' } });
        const countSpan = el('span', {
            text: `${group.attestations.length} attestations`,
            style: { color: '#7a6a8a' },
        });
        text.append(sep, countSpan);
    }

    // Deprecated marker
    if (group.deprecated) {
        const sep = el('span', { text: ' · ', style: { color: '#7a6a8a' } });
        const depSpan = el('span', {
            text: 'deprecated',
            style: { color: '#8a5050', fontStyle: 'italic' },
        });
        text.append(sep, depSpan);
    }

    // Color swatch
    const swatch = el('span', {
        style: {
            display: 'inline-block', width: '8px', height: '8px',
            backgroundColor: group.color, borderRadius: '2px',
            marginLeft: '6px', verticalAlign: 'middle',
        },
    });
    text.appendChild(swatch);

    item.appendChild(text);
    return item;
}
