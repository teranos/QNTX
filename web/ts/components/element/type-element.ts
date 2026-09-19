/**
 * Type Element (⊢) — canvas element for type attestations
 *
 * Shows a type definition: name, label, color swatch, field list,
 * and how many actors attested it. Simpler than sigma — it's a type,
 * not a statistical report.
 *
 * Opened via double-click on type result lines in AX or SE elements.
 */

import type { Element } from '@teranos/elements';
import { wireExpandToWindow, canvasPlaced, preventDrag, createSymbolSpan, settleSymbolSpan } from '@teranos/elements';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Type } from '../../sym';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getElementTypeBySymbol } from './element-registry';
import { el } from '../../html-utils';
import { parseTypeAttrs, type TypeGroup, groupTypeAttestations } from './type-result-line';

// Muted violet palette for type attestations
const TYPE_COLOR = '#b0a0d0';
const TYPE_DIM = '#7a6a8a';
const TYPE_VALUE = '#e0d0f0';

/** Build the type definition content */
function buildTypeContent(group: TypeGroup): HTMLElement {
    const container = el('div', {
        style: { padding: '8px 12px', fontFamily: 'monospace', fontSize: '12px' },
    });

    // Header: turnstile + type name in its own color
    const header = el('div', { style: { marginBottom: '8px' } });

    const nameRow = el('div', {
        style: { fontSize: '20px', fontWeight: 'bold', color: group.color, lineHeight: '1.2' },
    });
    nameRow.appendChild(el('span', { text: `${Type} ${group.subject}` }));

    // Color swatch next to name
    nameRow.appendChild(el('span', {
        style: {
            display: 'inline-block', width: '12px', height: '12px',
            backgroundColor: group.color, borderRadius: '2px',
            marginLeft: '8px', verticalAlign: 'middle',
        },
    }));
    header.appendChild(nameRow);

    // Label (if different from name)
    if (group.label !== group.subject) {
        header.appendChild(el('div', {
            text: group.label,
            style: { fontSize: '13px', color: TYPE_VALUE, marginTop: '2px' },
        }));
    }

    // Deprecated badge
    if (group.deprecated) {
        header.appendChild(el('div', {
            text: 'deprecated',
            style: { fontSize: '11px', color: '#8a5050', fontStyle: 'italic', marginTop: '2px' },
        }));
    }

    container.appendChild(header);

    // Fields section
    const attrs = parseTypeAttrs(group.attestations[0]);
    const richFields = Array.isArray(attrs?.rich_string_fields) ? attrs!.rich_string_fields as string[] : [];
    const arrayFields = Array.isArray(attrs?.array_fields) ? attrs!.array_fields as string[] : [];

    if (richFields.length > 0 || arrayFields.length > 0) {
        const fieldSection = el('div', { style: { marginBottom: '8px' } });

        fieldSection.appendChild(el('div', {
            text: 'fields',
            style: { fontSize: '10px', color: TYPE_DIM, marginBottom: '4px' },
        }));

        const fieldList = el('div', {
            style: { display: 'flex', flexWrap: 'wrap', gap: '4px' },
        });

        for (const f of richFields) {
            fieldList.appendChild(el('span', {
                text: f,
                style: {
                    fontSize: '11px', color: TYPE_VALUE, padding: '1px 5px',
                    backgroundColor: 'rgba(100, 80, 140, 0.25)', borderRadius: '2px',
                },
            }));
        }
        for (const f of arrayFields) {
            fieldList.appendChild(el('span', {
                text: `${f}[]`,
                style: {
                    fontSize: '11px', color: TYPE_COLOR, padding: '1px 5px',
                    backgroundColor: 'rgba(100, 80, 140, 0.15)', borderRadius: '2px',
                },
            }));
        }

        fieldSection.appendChild(fieldList);
        container.appendChild(fieldSection);
    }

    // Attestation count footer
    if (group.attestations.length > 0) {
        const actors = new Set(group.attestations.flatMap(a => a.actors || []));
        const parts: string[] = [];
        parts.push(`${group.attestations.length} attestation${group.attestations.length !== 1 ? 's' : ''}`);
        if (actors.size > 1) {
            parts.push(`${actors.size} actors`);
        }
        container.appendChild(el('div', {
            text: parts.join(' · '),
            style: { fontSize: '9px', color: TYPE_DIM, marginTop: '4px' },
        }));
    }

    return container;
}

/** Extract TypeGroup from attestations stored in element content */
function groupFromContent(content: string | undefined): TypeGroup | null {
    if (!content) return null;
    try {
        const data = JSON.parse(content);
        // Content can be a single attestation or an array
        const atts: Attestation[] = Array.isArray(data) ? data : [data];
        const groups = groupTypeAttestations(atts);
        return groups[0] || null;
    } catch (err) {
        // This is the element's own serialized state: unparseable means the
        // element renders empty, and that must not look like emptiness.
        log.warn(SEG.ELEMENT, 'Type element content is not parseable; rendering empty:', err);
        return null;
    }
}

// ─── Canvas element ────────────────────────────────────────────

/** Create a Type element for canvas placement */
export function createTypeElement(item: Element): HTMLElement {
    const group = groupFromContent(item.content);

    const titleBar = el('div', {
        class: 'title-bar title-bar--auto',
        style: { position: 'relative' },
    });

    const color = group?.color || TYPE_COLOR;

    // .symbol via the package — thread-line snapping and spine
    // anchoring locate elements by that class
    const symbolEl = item.symbolElement ? settleSymbolSpan(item.symbolElement) : createSymbolSpan(Type);
    Object.assign(symbolEl.style, { fontWeight: 'bold', color });
    titleBar.appendChild(symbolEl);

    if (group) {
        const titleText = el('span', {
            text: `${group.subject}${group.label !== group.subject ? ` · ${group.label}` : ''}`,
            style: { fontSize: '12px', fontFamily: 'monospace', color: TYPE_VALUE },
        });
        titleBar.appendChild(titleText);
    }

    const expandBtn = el('button', {
        class: 'titlebar-btn',
        text: '\u2B06',
        style: { flexShrink: '0', marginLeft: 'auto' },
    });
    expandBtn.title = 'Expand to window';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    const { element } = canvasPlaced({
        item: item,
        className: 'canvas-type-element',
        defaults: { x: 200, y: 200, width: 320, height: 280 },
        resizable: true,
        useMinHeight: true,
        logLabel: 'TypeElement',
    });
    element.style.minWidth = '180px';
    element.appendChild(titleBar);

    if (group) {
        const content = el('div', {
            class: 'content-area',
            style: {
                backgroundColor: 'rgba(25, 25, 30, 0.95)',
                borderTop: '1px solid var(--border)',
                overflow: 'auto',
            },
        });
        content.appendChild(buildTypeContent(group));
        element.appendChild(content);
    }

    const title = group
        ? `${Type} ${group.subject}`
        : 'Type';

    wireExpandToWindow({
        element,
        expandBtn,
        elementId: item.id,
        title,
        symbol: Type,
        renderContent: () => {
            const outer = el('div');
            const wrapper = el('div', { class: 'element-content' });
            outer.appendChild(wrapper);
            if (group) {
                wrapper.appendChild(buildTypeContent(group));
            }
            return outer;
        },
        logLabel: 'TypeElement',
    });

    log.debug(SEG.ELEMENT, `[TypeElement] Created ${item.id}`);
    return element;
}

// ─── Spawn helpers ───────────────────────────────────────────

/** Spawn a type element on the canvas from attestations */
export function spawnTypeElement(attestations: Attestation[], mouseX?: number, mouseY?: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.ELEMENT, '[TypeElement] Cannot spawn: no canvas-content-layer found');
        return;
    }

    const groups = groupTypeAttestations(attestations);
    if (groups.length === 0) return;
    const group = groups[0];

    const itemId = `type-${group.subject}-${crypto.randomUUID().slice(0, 8)}`;
    const item: Element = {
        id: itemId,
        title: group.subject,
        symbol: Type,
        x: mouseX !== undefined ? Math.round(mouseX - contentLayer.getBoundingClientRect().left + 20) : Math.round(window.innerWidth / 2 - 140),
        y: mouseY !== undefined ? Math.round(mouseY - contentLayer.getBoundingClientRect().top - 20) : Math.round(window.innerHeight / 2 - 120),
        content: JSON.stringify(attestations),
        renderContent: () => el('div'),
    };

    const entry = getElementTypeBySymbol(Type);
    if (!entry) {
        log.error(SEG.ELEMENT, '[TypeElement] Type not found in element registry');
        return;
    }

    const elementElement = entry.render(item) as HTMLElement;
    contentLayer.appendChild(elementElement);

    const rect = elementElement.getBoundingClientRect();
    uiState.addCanvasElement({
        id: itemId,
        symbol: Type,
        x: item.x!,
        y: item.y!,
        width: Math.round(rect.width) || 320,
        height: Math.round(rect.height) || 280,
        content: JSON.stringify(attestations),
    });

    log.debug(SEG.ELEMENT, `[TypeElement] Spawned ${itemId} at (${item.x}, ${item.y})`);
}
