import { describe, expect, test } from 'bun:test';
import { leavingFrom } from './leave';

// The deploy hands the build a DSN or nothing. Nothing is off, and off means
// no client exists, so the logger's third sink discards everything.

describe('Tim', () => {
    test('a DSN the build stamped turns the sink on, under the QNTX commit', () => {
        const going = leavingFrom({
            __SENTRY_DSN__: 'https://key@ingest.example/1',
            __QNTX_WEB_BUILD__: { commit: 'aaa', build_time: 't', qntx: 'abc1234' },
        });
        expect(going).toEqual({ dsn: 'https://key@ingest.example/1', release: 'abc1234' });
    });
});

describe('Spike', () => {
    test('no DSN is off', () => {
        expect(leavingFrom({})).toBeNull();
        expect(leavingFrom({ __SENTRY_DSN__: '' })).toBeNull();
        expect(leavingFrom({ __SENTRY_DSN__: '   ' })).toBeNull();
    });

    test('a DSN with no build stamp still leaves, under an unknown release', () => {
        expect(leavingFrom({ __SENTRY_DSN__: 'https://key@ingest.example/1' })?.release).toBe('unknown');
    });
});
