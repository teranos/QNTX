import { test, expect } from 'bun:test';
import { issuedBy, linesFor, namespacesField, renderIssued, rolesText, saidLine, type TokenInfo } from './token-element';

function token(): TokenInfo {
    return {
        id: 'AT_1',
        label: 'pond-sensor',
        did: 'did:key:zDatapunt',
        minted_by: 'apple:001750',
        namespaces: ['clean'],
        created_at: '2026-09-14T00:43:56Z',
    };
}

// "yes the label is the token's name": the grant names the label, in every
// namespace the token names, and nothing on it is the DID.
test('the grant names the token by its label in every namespace it names', () => {
    const t = { ...token(), namespaces: ['clean', 'pond'] };
    expect(linesFor(t, 'DATAPUNT')).toEqual([
        { subjects: ['pond-sensor'], predicates: ['role:granted', 'DATAPUNT'], contexts: ['clean'] },
        { subjects: ['pond-sensor'], predicates: ['role:granted', 'DATAPUNT'], contexts: ['pond'] },
    ]);
    expect(JSON.stringify(linesFor(t, 'DATAPUNT'))).not.toContain('did:key');
});

// The grant's context is the namespace exactly as the token's record spells
// it, since that is the string the node reads the token's roles by.
test('the grant names the namespace as the token spells it', () => {
    const t = { ...token(), namespaces: ['Clean'] };
    expect(linesFor(t, 'DATAPUNT')[0].contexts).toEqual(['Clean']);
});

// What a token wrote is read out loud, one line each, and a DID in it is its
// last eight so the line stays a line.
test('an attestation reads as X is Y of Z, a DID by its last eight', () => {
    expect(saidLine({ subjects: ['REACH'], predicates: ['/api/namespaces'], contexts: ['NOBODY'] }))
        .toBe('REACH is /api/namespaces of NOBODY');
    expect(saidLine({
        subjects: ['did:key:z6MkuibmftN7apH2C7NR1iduBvndKR7J46C5kPLxCSCn1XDK'],
        predicates: ['role:granted', 'WORKER'],
        contexts: ['TEST1'],
    })).toBe('CSCn1XDK is role:granted WORKER of TEST1');
    expect(saidLine({ subjects: ['visit-1'], predicates: ['visit:done'], contexts: [] })).toBe('visit-1 is visit:done');
});

// The namespaces as the namespace bar draws them, below the title bar.
function strip(t: TokenInfo): HTMLElement {
    const container = document.createElement('div');
    const row = namespacesField(container, t);
    container.appendChild(row);
    return row;
}

function tileNames(row: HTMLElement): string[] {
    return [...row.querySelectorAll<HTMLElement>('.namespace-tile[data-name]')].map(t => t.dataset.name ?? '');
}

test('the namespaces are the bar\'s tiles, no caption, the active one under the rectangle, and a + for a live client', () => {
    const row = strip({ ...token(), level: 'OAUTH', namespaces: ['Clean', 'default'] });
    expect(row.textContent).not.toContain('Namespaces');
    expect(row.querySelector('select')).toBeNull();
    // The bar's order, whatever order the client names them in.
    expect(tileNames(row)).toEqual(['default', 'Clean']);
    expect(row.querySelector<HTMLElement>('.namespace-tile.standing')?.dataset.name).toBe('Clean');
    expect(row.querySelector('.namespaces-rectangle')).not.toBeNull();
    expect(row.querySelector('.namespace-add')?.textContent).toBe('+');
    expect(row.querySelector<HTMLElement>('.namespace-tile[data-name="default"]')?.dataset.kind).toBe('default');
});

// "small ui bug, why does it not say that clean is where the rectangle is around ?"
// The strip is drawn before the window holds it, when nothing has a size, so
// the rectangle is placed again once the tiles are laid out.
test('the rectangle lands on the active tile once the tiles have their size', () => {
    const observed: Array<() => void> = [];
    const had = globalThis.ResizeObserver;
    globalThis.ResizeObserver = class {
        constructor(callback: () => void) { observed.push(callback); }
        observe() {}
        unobserve() {}
        disconnect() {}
    } as unknown as typeof ResizeObserver;
    try {
        const row = strip({ ...token(), level: 'OAUTH', namespaces: ['Clean', 'default'] });
        const active = row.querySelector<HTMLElement>('.namespace-tile.standing')!;
        for (const [key, value] of Object.entries({ offsetWidth: 120, offsetHeight: 24, offsetLeft: 132, offsetTop: 6 })) {
            Object.defineProperty(active, key, { configurable: true, value });
        }
        expect(observed.length).toBeGreaterThan(0);
        observed.forEach(laidOut => laidOut());

        const rectangle = row.querySelector<HTMLElement>('.namespaces-rectangle')!;
        expect(rectangle.hidden).toBe(false);
        expect(rectangle.style.width).toBe('120px');
        expect(rectangle.style.height).toBe('24px');
        expect(rectangle.style.transform).toBe('translate(132px, 6px)');
    } finally {
        globalThis.ResizeObserver = had;
    }
});

// "another route is right click, see the X next to it, press it once invert, again, reoved"
test('a right-click splits a tile into [<] name [X], and the first press of X arms it', () => {
    const row = strip({ ...token(), level: 'OAUTH', namespaces: ['Clean', 'default'] });
    const other = row.querySelector<HTMLElement>('.namespace-tile[data-name="default"]')!;
    other.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    expect(other.classList.contains('open')).toBe(true);
    expect([...other.querySelectorAll<HTMLElement>('.namespace-part')].map(p => p.textContent)).toEqual(['<', 'default', 'X']);

    const end = other.querySelector<HTMLElement>('[data-part="end"]')!;
    expect(end.dataset.end).toBe('active');
    end.click();
    expect(end.dataset.end).toBe('sure');
    expect(tileNames(row)).toEqual(['default', 'Clean']);

    other.querySelector<HTMLElement>('[data-part="back"]')!.click();
    expect(other.classList.contains('open')).toBe(false);
    expect(other.textContent).toBe('default');
});

// No fallback: the tile a client is active in does not split.
test('the active tile does not split', () => {
    const row = strip({ ...token(), level: 'OAUTH', namespaces: ['Clean', 'default'] });
    const active = row.querySelector<HTMLElement>('.namespace-tile.standing')!;
    active.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    expect(active.classList.contains('open')).toBe(false);
});

// Only ROOT changes where a client is, and only a live client issues anything.
test('a token of another kind, or a revoked client, shows plain tiles and nothing to press', () => {
    for (const t of [
        { ...token(), level: 'ATTESTOR' },
        { ...token(), level: 'REFRESH' },
        { ...token(), level: 'OAUTH', revoked_at: '2026-09-15T00:00:00Z' },
    ]) {
        const row = strip(t);
        expect(row.querySelector('.namespace-add')).toBeNull();
        expect(tileNames(row)).toEqual(['clean']);
        const tile = row.querySelector<HTMLElement>('.namespace-tile')!;
        tile.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
        expect(tile.classList.contains('open')).toBe(false);
    }
});

// "can't we move all the refresh tokens into a compact list in the element of the token it belongs to?"
test('a client lists what it issued, newest first, and nothing it did not', () => {
    const client = { ...token(), level: 'OAUTH', did: 'did:key:zClient' };
    const issued = issuedBy(client, [
        client,
        { ...token(), id: 'R1', level: 'REFRESH', client_did: 'did:key:zClient', created_at: '2026-09-20T10:00:00Z' },
        { ...token(), id: 'A2', level: 'ROOT', client_did: 'did:key:zClient', created_at: '2026-09-22T10:00:00Z' },
        { ...token(), id: 'X', level: 'REFRESH', client_did: 'did:key:zOther', created_at: '2026-09-23T10:00:00Z' },
    ]);
    expect(issued.map(t => t.id)).toEqual(['A2', 'R1']);
});

test('the issued list is one line each: kind, day and state, the time on the hover', () => {
    const now = new Date('2026-09-23T12:00:00Z');
    const container = document.createElement('div');
    renderIssued(container, [
        { ...token(), id: 'A2', level: 'ROOT', created_at: '2026-09-23T11:00:00Z', expires_at: '2026-09-23T12:30:00Z' },
        { ...token(), id: 'R1', level: 'REFRESH', created_at: '2026-09-20T10:00:00Z', revoked_at: '2026-09-20T11:00:00Z' },
        { ...token(), id: 'A1', level: 'ROOT', created_at: '2026-09-20T10:00:00Z', expires_at: '2026-09-20T11:00:00Z' },
    ], now);
    const lines = [...container.querySelectorAll<HTMLElement>('.token-issued-line')];
    expect(lines.map(l => l.textContent)).toEqual([
        'ROOT 2026-09-23 active',
        'REFRESH 2026-09-20 revoked',
        'ROOT 2026-09-20 expired',
    ]);
    expect(lines[0].title).toBe('created 2026-09-23 11:00:00');
    expect(container.textContent).toContain('Issued 3, 1 live');
});

test('a client that issued nothing says so', () => {
    const container = document.createElement('div');
    renderIssued(container, [], new Date());
    expect(container.textContent).toBe('Issued nothing yet');
});

test('the roles read per namespace, and a dash for none', () => {
    expect(rolesText(token())).toBe('clean: —');
    expect(rolesText({ ...token(), roles: { clean: ['DATAPUNT'] } })).toBe('clean: DATAPUNT');
});
