import { afterEach, describe, expect, test } from 'bun:test';
import { heldSession, holdSession, dropSession, bearing } from './session';

// An app holds the session the node handed it and presents it as a bearer;
// the node's own web holds none and presents nothing.

afterEach(() => dropSession());

describe('Tim', () => {
    test('a held session rides every request as a bearer', () => {
        holdSession('s3ss');
        expect(heldSession()).toBe('s3ss');
        expect(bearing().get('Authorization')).toBe('Bearer s3ss');
        expect(bearing({ 'Content-Type': 'application/json' }).get('Content-Type')).toBe('application/json');
    });

    test('logging out lets go of it', () => {
        holdSession('s3ss');
        dropSession();
        expect(heldSession()).toBe('');
        expect(bearing().has('Authorization')).toBe(false);
    });
});

describe('Spike', () => {
    test('a bearer already on the request is not overwritten', () => {
        holdSession('s3ss');
        expect(bearing({ Authorization: 'Bearer other' }).get('Authorization')).toBe('Bearer other');
    });
});
