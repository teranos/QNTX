import { test, expect } from 'bun:test';
import { linesFor, namespacesField, rolesText, saidLine, type TokenInfo } from './token-element';

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

// "I wish i could as ROOT, change the namespace where an OAUTH token is active in."
test('a live client is offered a pick of the namespace it moves to', () => {
    const client = { ...token(), level: 'OAUTH', namespaces: ['default'] };
    const shown = namespacesField(document.createElement('div'), client);
    expect(shown.querySelector('select')).not.toBeNull();
    expect(shown.textContent).toContain('default');
});

// Every other kind names where it acts at minting and keeps it; a revoked
// client issues nothing, so moving it moves nothing.
test('only a live client is offered a move', () => {
    for (const t of [
        { ...token(), level: 'ATTESTOR' },
        { ...token(), level: 'REFRESH' },
        { ...token(), level: 'OAUTH', revoked_at: '2026-09-15T00:00:00Z' },
    ]) {
        expect(namespacesField(document.createElement('div'), t).querySelector('select')).toBeNull();
    }
});

test('the roles read per namespace, and a dash for none', () => {
    expect(rolesText(token())).toBe('clean: —');
    expect(rolesText({ ...token(), roles: { clean: ['DATAPUNT'] } })).toBe('clean: DATAPUNT');
});
