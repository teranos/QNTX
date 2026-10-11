import { describe, expect, test } from 'bun:test';
import { madeWhere } from './passkey';

// The node asks for a passkey made in this device's own store. A browser that
// has none, Chromium on Linux, reaches the person's phone over hybrid instead,
// and WebAuthn skips any authenticator whose attachment differs (§5.1.3).

const fromTheNode = () => ({
    publicKey: {
        challenge: 'c',
        authenticatorSelection: { authenticatorAttachment: 'platform', residentKey: 'discouraged' },
    },
});

describe('Tim', () => {
    test('a browser with a store of its own is asked exactly as the node said', () => {
        const options = fromTheNode();
        expect(madeWhere(options, true)).toBe(options);
    });

    test('a browser with no store of its own may make the passkey on a phone', () => {
        const made = madeWhere(fromTheNode(), false);
        expect(made.publicKey.authenticatorSelection).toEqual({ residentKey: 'discouraged' });
    });
});

describe('Spike', () => {
    test('what the node said is left as it said it', () => {
        const options = fromTheNode();
        madeWhere(options, false);
        expect(options.publicKey.authenticatorSelection.authenticatorAttachment).toBe('platform');
    });

    test('a node that names no selection is passed through', () => {
        const options = { publicKey: { challenge: 'c' } };
        expect(madeWhere(options, false)).toBe(options);
    });
});
