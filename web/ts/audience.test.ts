// An element's audience is whoever reaches every path it needs (#1239).

import { describe, test, expect } from 'bun:test';
import type { Element } from '@teranos/elements';
import { forTheViewer, type Audience } from './audience';

const element = (id: string): Element => ({ id, title: id, symbol: id, renderContent: () => document.createElement('div') });

const i: Audience = { needs: ['/i/'], create: () => element('i') };
const approvals: Audience = { needs: ['/api/approvals'], create: () => element('approvals') };
const handlers: Audience = { needs: ['/api/attestations', '/api/watchers'], create: () => element('handlers') };

describe('Tim', () => {
    test('a USER is offered what they reach and nothing else', async () => {
        const offered = await forTheViewer([i, approvals], async () => ['/i/']);
        expect(offered).toEqual([i]);
    });
});

describe('Spike', () => {
    test('an element needing two paths is not offered on one of them', async () => {
        const offered = await forTheViewer([handlers], async () => ['/api/attestations']);
        expect(offered).toEqual([]);
    });

    test('an element naming no path does not compile', () => {
        // @ts-expect-error an element naming no path would be every viewer's
        const nothing: Audience = { needs: [], create: () => element('nothing') };
        expect(nothing.needs).toEqual([]);
    });
});

describe('Jenny', () => {
    test('the node is asked once, each path once', async () => {
        const asked: string[][] = [];
        const both: Audience = { needs: ['/i/', '/api/watchers'], create: () => element('both') };
        await forTheViewer([i, handlers, both], async (paths) => {
            asked.push(paths);
            return paths;
        });
        expect(asked).toEqual([['/i/', '/api/attestations', '/api/watchers']]);
    });
});
