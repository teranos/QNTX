import { describe, expect, test } from 'bun:test';
import type { Namespace } from './namespaces-view';
import { namespacePick } from './token-mint-element';

function ns(name: string): Namespace {
    return { name, definition: null, kinds: [] };
}

// "I SHOULD NOT HAVE TO TYPE THE NAMESPACE NAME"
describe('which namespace a token is minted into', () => {
    describe('tim', () => {
        test('every namespace the node lists is offered, in the bar order, set to where you stand', () => {
            const pick = namespacePick([ns('POND'), ns('default'), ns('system')], 'POND');
            expect(pick.names).toEqual(['system', 'default', 'POND']);
            expect(pick.chosen).toBe('POND');
        });

        test('standing nowhere is standing in default', () => {
            expect(namespacePick([ns('system'), ns('default')], '').chosen).toBe('default');
        });
    });

    describe('spike', () => {
        // Starting on the first namespace would mint into one you are not in.
        test('a standing the list lacks starts on nothing', () => {
            expect(namespacePick([ns('default'), ns('POND')], 'MARSH').chosen).toBe('');
        });

        test('a node listing no namespaces offers nothing and starts on nothing', () => {
            expect(namespacePick([], 'default')).toEqual({ names: [], chosen: '' });
        });
    });
});
