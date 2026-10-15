/**
 * relayed — whether the session on the page is the dev server's rather than
 * this browser's. Personas: Tim (through the relay with his own cookie), Spike
 * (the relay presenting a token of its own).
 */

import { describe, test, expect, afterEach } from 'bun:test';
import { relayed } from './door';

type DevWindow = { __DEV__?: boolean; __RELAYED__?: boolean; __BACKEND_URL__?: string };
const w = window as unknown as DevWindow;

afterEach(() => {
    delete w.__DEV__;
    delete w.__RELAYED__;
    delete w.__BACKEND_URL__;
});

describe('relayed - Tim', () => {
    test('the dev server beside a node on this machine says nothing, and the session is Tim’s own', () => {
        w.__DEV__ = true;
        expect(relayed()).toBe(false);
    });

    test('a built page served by the node itself is never relayed', () => {
        expect(relayed()).toBe(false);
    });
});

describe('relayed - Spike', () => {
    test('the dev server presenting its own credential says so, and the door stands', () => {
        w.__DEV__ = true;
        w.__RELAYED__ = true;
        expect(relayed()).toBe(true);
    });

    test('naming the node no longer decides it', () => {
        w.__DEV__ = true;
        w.__BACKEND_URL__ = 'http://127.0.0.1:8780';
        expect(relayed()).toBe(false);
    });
});
