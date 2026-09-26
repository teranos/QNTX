import { describe, test, expect } from 'bun:test';
import { keyFor, setStanding, standingNamespace } from './standing';

describe('what the browser keeps is the namespace\'s', () => {
    test('default and nowhere keep the key the browser always used', () => {
        expect(keyFor('qntx-ui-state', '')).toBe('qntx-ui-state');
        expect(keyFor('qntx-ui-state', 'default')).toBe('qntx-ui-state');
    });

    test('any other namespace keeps its own', () => {
        expect(keyFor('qntx-ui-state', 'aws')).toBe('qntx-ui-state:aws');
        expect(keyFor('qntx-canvas-sync-queue', 'aws')).toBe('qntx-canvas-sync-queue:aws');
    });

    test('the key follows where the person stands', () => {
        setStanding('aws');
        expect(standingNamespace()).toBe('aws');
        expect(keyFor('qntx-ui-state')).toBe('qntx-ui-state:aws');
        setStanding('');
        expect(keyFor('qntx-ui-state')).toBe('qntx-ui-state');
    });
});
