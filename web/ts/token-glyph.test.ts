import { test, expect } from 'bun:test';
import { linesFor, parseRole, rolesText, type TokenInfo } from './token-glyph';

function token(): TokenInfo {
    return {
        id: 'AT_1',
        label: 'clean-datapunt',
        did: 'did:key:zDatapunt',
        minted_by: 'apple:001750',
        namespaces: ['clean'],
        created_at: '2026-09-14T00:43:56Z',
    };
}

// A line is read back with its roles uppercased and a grant is not, so the
// role typed in lowercase must not silently hold nothing.
test('the role is uppercased', () => {
    expect(parseRole('datapunt')).toBe('DATAPUNT');
    expect(parseRole('  DATAPUNT ')).toBe('DATAPUNT');
    expect(parseRole('')).toBe('');
});

// "yes the label is the token's name": the grant names the label, in every
// namespace the token names, and nothing on it is the DID.
test('the grant names the token by its label in every namespace it names', () => {
    const t = { ...token(), namespaces: ['clean', 'pond'] };
    expect(linesFor(t, 'DATAPUNT')).toEqual([
        { subjects: ['clean-datapunt'], predicates: ['role:granted', 'DATAPUNT'], contexts: ['clean'] },
        { subjects: ['clean-datapunt'], predicates: ['role:granted', 'DATAPUNT'], contexts: ['pond'] },
    ]);
    expect(JSON.stringify(linesFor(t, 'DATAPUNT'))).not.toContain('did:key');
});

// The grant's context is the namespace exactly as the token's record spells
// it, since that is the string the node reads the token's roles by.
test('the grant names the namespace as the token spells it', () => {
    const t = { ...token(), namespaces: ['Clean'] };
    expect(linesFor(t, 'DATAPUNT')[0].contexts).toEqual(['Clean']);
});

test('the roles read per namespace, and a dash for none', () => {
    expect(rolesText(token())).toBe('clean: —');
    expect(rolesText({ ...token(), roles: { clean: ['DATAPUNT'] } })).toBe('clean: DATAPUNT');
});
