/**
 * What decides that the ceremony is offered. A fresh browser holds no binding,
 * so its first login is refused — whether the door answers that with provider
 * selection or with a dead end is this rule.
 */

import { describe, test, expect } from 'bun:test';
import { needsCeremony } from './signin';
import { LayeLoginRefused } from './laye';

describe('needsCeremony — Tim links his first account', () => {
    test('403 opens the ceremony', () => {
        expect(needsCeremony(new LayeLoginRefused(403, '{"error":"nope"}'))).toBe(true);
    });

    test('the status survives being thrown and caught', () => {
        try {
            throw new LayeLoginRefused(403, '{"error":"x"}');
        } catch (e) {
            expect(needsCeremony(e)).toBe(true);
        }
    });
});

describe('needsCeremony — Spike sends something else', () => {
    // A ceremony cannot fix a node that is broken, unreachable, or refusing
    // the signature itself. Offering one sends the person to a provider to
    // answer a question nobody asked.
    test('other refusals do not', () => {
        expect(needsCeremony(new LayeLoginRefused(401, 'signature does not verify'))).toBe(false);
        expect(needsCeremony(new LayeLoginRefused(400, 'bad request'))).toBe(false);
        expect(needsCeremony(new LayeLoginRefused(500, 'boom'))).toBe(false);
        expect(needsCeremony(new LayeLoginRefused(503, 'no signing key'))).toBe(false);
    });

    // Prose that looks like a refusal is not one — this is what the old
    // string match accepted.
    test('a plain error is not a refusal even when it says so', () => {
        expect(needsCeremony(new Error('laye login refused (403): auth.root_identities'))).toBe(false);
        expect(needsCeremony(new Error('root_identities'))).toBe(false);
    });

    test('a non-error decides nothing', () => {
        expect(needsCeremony('403')).toBe(false);
        expect(needsCeremony(null)).toBe(false);
        expect(needsCeremony(undefined)).toBe(false);
    });
});

describe('needsCeremony — Jenny changes the wording', () => {
    // The regression. The refusal named auth.root_identities and the glyph
    // matched on it, so making the message generic removed the ceremony and
    // nothing failed.
    test('the wording of the refusal decides nothing', () => {
        const named = new LayeLoginRefused(403, '{"error":"not listed in auth.root_identities"}');
        const generic = new LayeLoginRefused(403, '{"error":"this identity may not log in here"}');
        const empty = new LayeLoginRefused(403, '');

        expect(needsCeremony(named)).toBe(true);
        expect(needsCeremony(generic)).toBe(true);
        expect(needsCeremony(empty)).toBe(true);
    });
});
