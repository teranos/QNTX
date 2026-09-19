/**
 * @jest-environment jsdom
 *
 * Plugin panel — what a plugin does, as sigils (ADR-039).
 *
 * "I WANT TO USE MANUS AI": an outside agent reaches a plugin's sigil over MCP
 * only once a line opens it. The row says who reaches it now, and grants it.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { reachedInWords, renderSigilBadges, renderSigils } from './plugin-panel.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

type Plugin = Parameters<typeof renderSigils>[0];

const nobody = { anyone: false, levels: [], roles: [] };

function datapunt(overrides: Partial<Plugin> = {}): Plugin {
    return {
        name: 'datapunt',
        version: '0.1.0',
        description: 'the competitor data',
        healthy: true,
        state: 'running',
        pausable: false,
        sigils: [{
            signum: 'datapunt',
            sigil: 'read',
            tool: 'datapunt_read',
            method: 'GET',
            path: '/api/datapunt/read',
            does: 'What is observed of a kind.',
            takes: [
                { name: 'kind', says: 'The kind.', required: true, one_of: ['competitor'] },
                { name: 'name', says: 'One subject.' },
            ],
            gives: [{ name: 'observed', says: 'Whether it is.' }],
            reach: { http: nobody, mcp: { anyone: false, levels: [], roles: ['manus'] } },
        }],
        ...overrides,
    };
}

describe('Plugin panel sigils', () => {
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

    test('a sigil row says the tool, where it answers, what it does and what it takes', () => {
        container.innerHTML = renderSigils(datapunt());

        expect(container.textContent).toContain('datapunt_read');
        expect(container.textContent).toContain('GET /api/datapunt/read');
        expect(container.textContent).toContain('What is observed of a kind.');

        const params = container.querySelectorAll('.plugin-sigil-param');
        expect(params.length).toBe(2);
        expect(params[0].textContent).toBe('kind*');
        expect(params[0].getAttribute('data-tooltip')).toContain('One of: competitor');
        expect(params[1].textContent).toBe('name');
    });

    test('who reaches a sigil is said per surface, and nobody named is ROOT only', () => {
        container.innerHTML = renderSigils(datapunt());

        const reach = container.querySelector('.plugin-sigil-reach')?.textContent ?? '';
        expect(reach).toContain('http: ROOT only');
        expect(reach).toContain('mcp: ROOT, manus');
    });

    test('a sigil row offers a grant, keyed by the signum and the sigil', () => {
        container.innerHTML = renderSigils(datapunt());

        const grant = container.querySelector<HTMLButtonElement>('.plugin-sigil-grant-btn');
        expect(grant?.dataset.sigil).toBe('datapunt:read');
        expect(container.querySelector('.plugin-sigil-grant-role')).not.toBeNull();
    });

    test('a signum the node refused is shown with why, not only logged', () => {
        const refused = datapunt({ sigils: [], signa_refused: ['the plugin datapunt handed a signum named staands'] });

        container.innerHTML = renderSigils(refused);
        expect(container.textContent).toContain('handed a signum named staands');

        container.innerHTML = renderSigilBadges(refused);
        expect(container.querySelector('.plugin-sigil-refused')?.getAttribute('data-tooltip'))
            .toContain('handed a signum named staands');
    });

    test('a plugin that hands no signa draws no sigils', () => {
        const none = datapunt({ sigils: undefined });
        expect(renderSigils(none)).toBe('');
        expect(renderSigilBadges(none)).toBe('');
    });

    test('the header counts the sigils', () => {
        container.innerHTML = renderSigilBadges(datapunt());
        expect(container.textContent).toBe('1 sigil');
    });
});

describe('reachedInWords', () => {
    test('anyone, ROOT only, and ROOT with who else', () => {
        expect(reachedInWords({ anyone: true, levels: [], roles: [] })).toBe('anyone');
        expect(reachedInWords(nobody)).toBe('ROOT only');
        expect(reachedInWords({ anyone: false, levels: ['SUPER'], roles: ['manus'] })).toBe('ROOT, SUPER, manus');
    });
});
