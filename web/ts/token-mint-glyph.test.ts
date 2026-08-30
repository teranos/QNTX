import { describe, expect, test } from 'bun:test';
import { asList } from './token-mint-glyph';

describe('what a field says a token may touch', () => {
    describe('tim', () => {
        test('one entry is a list of one', () => {
            expect(asList('deploy')).toEqual(['deploy']);
        });

        test('several entries are the list, without their spacing', () => {
            expect(asList('deploy, ingested ,  built')).toEqual(['deploy', 'ingested', 'built']);
        });

        test('a star stays a star', () => {
            expect(asList('*')).toEqual(['*']);
        });
    });

    describe('spike', () => {
        // Empty grants nothing, so a blank field cannot become a list holding
        // one empty entry — that is a token scoped to a predicate with no name.
        test('a blank field grants nothing rather than something unnamed', () => {
            expect(asList('')).toEqual([]);
            expect(asList('   ')).toEqual([]);
        });

        test('commas without entries between them are still nothing', () => {
            expect(asList(',,')).toEqual([]);
            expect(asList('deploy,,built')).toEqual(['deploy', 'built']);
        });

        test('a trailing comma does not add an entry', () => {
            expect(asList('deploy,')).toEqual(['deploy']);
        });
    });
});
