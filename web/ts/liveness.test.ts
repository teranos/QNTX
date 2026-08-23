import { test, expect } from 'bun:test';
import { isLive, statedPlainly, type Reached } from './liveness';

const HEALTH = 'https://api.qntx.example/health';

function reached(over: Partial<Reached>): Reached {
    return { url: HEALTH, status: 0, body: '', error: '', ...over };
}

test('200 is running', () => {
    expect(isLive(reached({ status: 200, body: '{"status":"ok"}' }))).toBe(true);
});

// 503 is what /health answers when the operational store is unreadable.
test('503 is not running', () => {
    expect(isLive(reached({ status: 503, body: '{"status":"down"}' }))).toBe(false);
});

test('502 is not running', () => {
    expect(isLive(reached({ status: 502 }))).toBe(false);
});

test('no answer at all is not running', () => {
    expect(isLive(reached({ error: 'TypeError: NetworkError' }))).toBe(false);
});

test('what it says is the request and the answer', () => {
    const said = statedPlainly(reached({ status: 503, body: '{"status":"down"}' }));
    expect(said[0]).toBe(`GET ${HEALTH}`);
    expect(said[1]).toBe('answered 503');
    expect(said[2]).toBe('the node said: {"status":"down"}');
});

test('a request that never landed says so, in the thrown words', () => {
    const said = statedPlainly(reached({ error: 'TypeError: NetworkError when attempting to fetch resource.' }));
    expect(said[1]).toBe('no answer — TypeError: NetworkError when attempting to fetch resource.');
});

// The browser cannot see a database, a process or a proxy. Naming one would be
// this side of the wire guessing at the other side.
test('it never names a cause it cannot have seen', () => {
    const everything = [
        statedPlainly(reached({ status: 503, body: '{"status":"down"}' })),
        statedPlainly(reached({ status: 502 })),
        statedPlainly(reached({ error: 'TypeError: NetworkError' })),
    ].flat().join(' ').toLowerCase();

    for (const guess of ['database', 'sqlite', 'operational', 'server is', 'crashed', 'offline', 'probably', 'likely']) {
        expect(everything).not.toContain(guess);
    }
});

test('it says the UI did not load, which is a fact about this page', () => {
    const said = statedPlainly(reached({ status: 503 }));
    expect(said[said.length - 1]).toContain('QNTX did not start');
});
