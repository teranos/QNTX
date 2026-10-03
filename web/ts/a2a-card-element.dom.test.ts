/**
 * @jest-environment jsdom
 *
 * The A2A card — the card a caller would be given, resting in ≡ as a button.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderA2ACard, a2aCardRow, A2A_CARD_ID, type AmCard } from './a2a-card-element.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

// What am node answered on a node with no [node] set, cut to two skills.
const answered: AmCard = {
    card: {
        version: 'v0.36.0',
        supportedInterfaces: [
            { url: 'http://localhost:8770/a2a', protocolBinding: 'HTTP+JSON', protocolVersion: '1.0' },
        ],
        capabilities: { extensions: [{ params: { mcp: { url: 'http://localhost:8770/mcp', protocolVersion: '2026-07-28' } } }] },
        skills: [{ name: 'staands' }, { name: 'parity' }],
    },
    missing: ['AgentCard.name', 'AgentCard.description', 'AgentCard.skills[0].tags', 'AgentCard.skills[1].tags'],
};

describe('A2A card', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    // Tim: the card says what the node said, and nothing it did not.
    test('the card says what the node said, and what it left empty', () => {
        renderA2ACard(container, answered);
        const text = container.textContent ?? '';
        expect(text).toContain('not said');
        expect(text).toContain('v0.36.0');
        expect(text).toContain('http://localhost:8770/a2a HTTP+JSON 1.0');
        expect(text).toContain('http://localhost:8770/mcp 2026-07-28');
        expect(text).toContain('staands, parity');
        expect(text).toContain('AgentCard.name');
        expect(text).toContain('AgentCard.skills[1].tags');
    });

    // Tim: in ≡ it rests as one small italic button saying A2A.
    test('it rests as a button saying A2A', () => {
        container.appendChild(a2aCardRow());
        const card = container.querySelector<HTMLElement>(`[data-element-id="${A2A_CARD_ID}"]`);
        expect(card).not.toBeNull();
        expect(card!.dataset.form).toBe('button');
        expect(card!.textContent).toBe('A2A');
        expect(card!.classList.contains('a2a-card-button')).toBe(true);
    });

    // Spike: ≡ redraws itself; the card is still one element, never a second.
    test('drawn again, it is the same element', () => {
        const first = a2aCardRow();
        container.appendChild(first);
        const button = container.querySelector(`[data-element-id="${A2A_CARD_ID}"]`);
        container.innerHTML = '';
        container.appendChild(a2aCardRow());
        expect(document.querySelectorAll(`[data-element-id="${A2A_CARD_ID}"]`)).toHaveLength(1);
        expect(container.querySelector(`[data-element-id="${A2A_CARD_ID}"]`)).toBe(button);
    });

    // Jenny: pressed, it is the window, and ≡ drawn again keeps its hole.
    test('pressed, it is the window, and ≡ drawn again keeps the hole', () => {
        container.appendChild(a2aCardRow());
        const card = container.querySelector<HTMLElement>(`[data-element-id="${A2A_CARD_ID}"]`)!;
        card.click();
        expect(card.dataset.form).toBe('window');
        container.innerHTML = '';
        container.appendChild(a2aCardRow());
        expect(container.querySelector('.button-gap')).not.toBeNull();
        expect(document.querySelectorAll(`[data-element-id="${A2A_CARD_ID}"]`)).toHaveLength(1);
    });
});
