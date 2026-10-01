import { describe, expect, test } from 'bun:test';
import { readSetup } from './setup';

describe('what a node says about being owned', () => {
    test('a node with login says it in JSON', () => {
        expect(readSetup(200, '{"claimed":true}')).toEqual({ claimed: true });
    });

    // A node with auth.enabled = false answers that it has no login, which is
    // a statement: there is no door to stand at and nobody to claim it.
    test('a node with no login is neither claimed nor governed', () => {
        expect(readSetup(404, 'this node has no login\n')).toEqual({ claimed: false, governed: false });
    });

    test('any other answer is the node not saying', () => {
        expect(() => readSetup(404, 'File not found')).toThrow('404');
        expect(() => readSetup(500, 'boom')).toThrow('500');
    });
});
