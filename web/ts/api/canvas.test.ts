import { describe, test, expect, mock } from 'bun:test';

let answer: (path: string, init?: RequestInit) => Promise<Response>;
mock.module('../client', () => ({
    apiFetch: (path: string, init?: RequestInit) => answer(path, init),
    apiJson: async () => { throw new Error('not asked'); },
    connectivity: { state: 'offline', subscribe: () => () => {}, subscribeAuth: () => () => {} },
}));

const { theCanvas, createCanvas } = await import('./canvas');

describe('the canvas of the namespace stood in', () => {
    test('404 is a namespace with no canvas', async () => {
        answer = async () => new Response(JSON.stringify({ error: 'this namespace has no canvas' }), { status: 404 });
        expect(await theCanvas()).toBeNull();
    });

    test('200 is its name', async () => {
        answer = async () => new Response(JSON.stringify({ name: 'default' }), { status: 200 });
        expect(await theCanvas()).toEqual({ name: 'default' });
    });

    test('a refusal is thrown in the node\'s words', async () => {
        answer = async () => new Response(JSON.stringify({ error: 'the User is switched off' }), { status: 403 });
        await expect(theCanvas()).rejects.toThrow('the User is switched off');
    });

    test('create sends the name, and a 409 is the node\'s words', async () => {
        let sent = '';
        answer = async (_path, init) => {
            sent = String(init?.body);
            return new Response(JSON.stringify({ error: 'this namespace already has a canvas: creating aws' }), { status: 409 });
        };
        await expect(createCanvas('aws')).rejects.toThrow('this namespace already has a canvas');
        expect(sent).toBe('{"name":"aws"}');
    });
});
