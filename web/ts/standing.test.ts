import { describe, test, expect, beforeEach } from 'bun:test';
import { canvasQuery, keyFor, setOpenCanvas, setStanding, standingNamespace } from './standing';

describe('what the browser keeps is the namespace\'s, and the canvas\'s', () => {
    beforeEach(() => {
        setStanding('');
        setOpenCanvas('');
    });

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
    });

    test('a User\'s canvas keeps its own key, and every canvas route is asked with it', () => {
        setStanding('aws');
        expect(canvasQuery()).toBe('');
        setOpenCanvas('CV-ALICE');
        expect(keyFor('qntx-ui-state')).toBe('qntx-ui-state:aws:CV-ALICE');
        expect(canvasQuery()).toBe('?canvas=CV-ALICE');
    });

    test('the namespace\'s own canvas keeps the namespace\'s key', () => {
        setStanding('default');
        setOpenCanvas('');
        expect(keyFor('qntx-ui-state')).toBe('qntx-ui-state');
    });
});
