import { describe, test, expect, mock } from 'bun:test';
import { renderForm, renderKeys } from './keys-element';

const KEYS = [
    { name: 'OPENROUTER_API_KEY', set_by: 'https://mastodon.example/@gardener', set_at: '2026-10-08T09:30:00Z' },
    { name: 'SENTRY_DSN', set_by: 'https://mastodon.example/@gardener', set_at: '2026-10-07T21:05:12Z' },
];

const settle = () => new Promise(resolve => setTimeout(resolve, 0));

describe('the keys of the namespace stood in', () => {
    describe('tim', () => {
        test('each key reads its name, who set it and when', () => {
            const container = document.createElement('div');
            renderKeys(container, KEYS, async () => {});
            const rows = [...container.querySelectorAll('tbody tr')].map(tr =>
                [...tr.querySelectorAll('td')].slice(0, 3).map(td => td.textContent));
            expect(rows).toEqual([
                ['OPENROUTER_API_KEY', 'https://mastodon.example/@gardener', '2026-10-08 09:30:00'],
                ['SENTRY_DSN', 'https://mastodon.example/@gardener', '2026-10-07 21:05:12'],
            ]);
        });

        test('the value field is a password field, and is emptied once saved', async () => {
            const container = document.createElement('div');
            document.body.appendChild(container);
            const onSave = mock(async (_name: string, _value: string) => {});
            renderForm(container, onSave);
            const name = container.querySelector<HTMLInputElement>('.keys-name')!;
            const value = container.querySelector<HTMLInputElement>('.keys-value')!;
            expect(value.type).toBe('password');

            name.value = 'OPENROUTER_API_KEY';
            value.value = 'sk-or-v1-secret';
            container.querySelector<HTMLButtonElement>('button')!.click();
            await settle();

            expect(onSave).toHaveBeenCalledWith('OPENROUTER_API_KEY', 'sk-or-v1-secret');
            expect(value.value).toBe('');
            container.remove();
        });

        test('a drop asks twice, then drops that name', async () => {
            const container = document.createElement('div');
            document.body.appendChild(container);
            const onDrop = mock(async (_name: string) => {});
            renderKeys(container, KEYS, onDrop);
            const drop = container.querySelector<HTMLButtonElement>('tbody tr:last-child button')!;
            drop.click();
            await settle();
            expect(onDrop).not.toHaveBeenCalled();
            drop.click();
            await settle();
            expect(onDrop).toHaveBeenCalledWith('SENTRY_DSN');
            container.remove();
        });
    });

    describe('spike', () => {
        test('no keys reads so', () => {
            const container = document.createElement('div');
            renderKeys(container, [], async () => {});
            expect(container.textContent).toBe('No keys.');
        });

        test('a refusal is shown beside the save as the node worded it, and the value stays', async () => {
            const container = document.createElement('div');
            document.body.appendChild(container);
            const said = 'the keys of garden are set by ROOT, SUPER and its owner';
            renderForm(container, async () => { throw new Error(said); });
            const value = container.querySelector<HTMLInputElement>('.keys-value')!;
            value.value = 'sk-or-v1-secret';
            container.querySelector<HTMLButtonElement>('button')!.click();
            await settle();
            expect(container.querySelector('.qntx-btn-error-box')?.textContent).toBe(said);
            expect(value.value).toBe('sk-or-v1-secret');
            container.remove();
        });
    });
});
