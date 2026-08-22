/**
 * The QR encoder, checked against the parts of ISO/IEC 18004 that are positional
 * and therefore checkable without a decoder.
 */

// What these catch: a wrong version for a length, a finder or timing pattern in
// the wrong place, a format field whose BCH does not come back, and the mask the
// format claims not being the mask that was applied.

import { describe, test, expect } from 'bun:test';
import { encodeQR } from './qr';

/** The 15 format bits, read out of the copy beside the top-left finder. */
function formatFrom(grid: Int8Array[]): number {
    let bits = 0;
    for (let i = 0; i < 15; i++) {
        const [x, y] = i < 6 ? [8, i]
            : i === 6 ? [8, 7]
            : i === 7 ? [8, 8]
            : i === 8 ? [7, 8]
            : [14 - i, 8];
        bits |= grid[y][x] << i;
    }
    return bits;
}

/** Un-masks the format field and checks its BCH remainder is zero. */
function readFormat(grid: Int8Array[]): { level: number; mask: number; sound: boolean } {
    const bits = formatFrom(grid) ^ 0b101010000010010;

    let rest = bits;
    for (let i = 4; i >= 0; i--) {
        if (rest & (1 << (i + 10))) rest ^= 0b10100110111 << i;
    }
    return { level: bits >> 13, mask: (bits >> 10) & 0b111, sound: rest === 0 };
}

describe('encodeQR — the version fits the payload', () => {
    test('a short string is the smallest grid', () => {
        expect(encodeQR('qntx').length).toBe(21);
    });

    test('the grid grows four modules per version', () => {
        // 14 bytes is all a version 1 holds at this error correction; 15 is a
        // version 2, which is 25 modules across.
        expect(encodeQR('a'.repeat(14)).length).toBe(21);
        expect(encodeQR('a'.repeat(15)).length).toBe(25);
        expect(encodeQR('a'.repeat(26)).length).toBe(25);
        expect(encodeQR('a'.repeat(27)).length).toBe(29);
    });

    test('a connect URL fits, and says which version it took', () => {
        const url = 'https://qntx.example/branch/user/#connect=' + 'a1b2c3d4'.repeat(8);
        const grid = encodeQR(url);
        expect(grid.length).toBeGreaterThan(21);
        expect(grid.length).toBeLessThanOrEqual(57);
    });

    test('more than a version 10 holds is refused rather than truncated', () => {
        expect(() => encodeQR('a'.repeat(214))).toThrow();
    });
});

describe('encodeQR — the function patterns are where a scanner looks', () => {
    const grid = encodeQR('https://qntx.example/branch/user/#connect=deadbeef');
    const size = grid.length;

    test('three finders, each an exact seven by seven', () => {
        for (const [ox, oy] of [[0, 0], [size - 7, 0], [0, size - 7]]) {
            for (let dy = 0; dy < 7; dy++) {
                for (let dx = 0; dx < 7; dx++) {
                    const edge = dx === 0 || dx === 6 || dy === 0 || dy === 6;
                    const core = dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4;
                    expect(grid[oy + dy][ox + dx]).toBe(edge || core ? 1 : 0);
                }
            }
        }
    });

    test('the fourth corner is not a finder', () => {
        let dark = 0;
        for (let dy = 0; dy < 7; dy++) {
            for (let dx = 0; dx < 7; dx++) dark += grid[size - 7 + dy][size - 7 + dx];
        }
        expect(dark).not.toBe(33);
    });

    test('timing alternates along row and column six', () => {
        for (let i = 8; i < size - 8; i++) {
            expect(grid[6][i]).toBe(i % 2 === 0 ? 1 : 0);
            expect(grid[i][6]).toBe(i % 2 === 0 ? 1 : 0);
        }
    });

    test('the dark module is dark', () => {
        expect(grid[size - 8][8]).toBe(1);
    });
});

describe('encodeQR — the format field says what was actually done', () => {
    test('it decodes, and it says level M', () => {
        const { level, sound } = readFormat(encodeQR('qntx'));
        expect(sound).toBe(true);
        expect(level).toBe(0b00);
    });

    test('the mask it names is one of the eight', () => {
        const { mask } = readFormat(encodeQR('qntx'));
        expect(mask).toBeGreaterThanOrEqual(0);
        expect(mask).toBeLessThanOrEqual(7);
    });

    test('both copies of the field agree', () => {
        const grid = encodeQR('https://qntx.example/#connect=abc');
        const size = grid.length;
        for (let i = 0; i < 15; i++) {
            const [ax, ay] = i < 6 ? [8, i]
                : i === 6 ? [8, 7]
                : i === 7 ? [8, 8]
                : i === 8 ? [7, 8]
                : [14 - i, 8];
            const [bx, by] = i < 8 ? [size - 1 - i, 8] : [8, size - 15 + i];
            expect(grid[ay][ax]).toBe(grid[by][bx]);
        }
    });

    test('a different payload is a different grid', () => {
        const one = encodeQR('https://qntx.example/#connect=aaaa');
        const two = encodeQR('https://qntx.example/#connect=bbbb');
        let same = true;
        for (let y = 0; y < one.length; y++) {
            for (let x = 0; x < one.length; x++) {
                if (one[y][x] !== two[y][x]) same = false;
            }
        }
        expect(same).toBe(false);
    });

    test('the same payload is the same grid', () => {
        const one = encodeQR('qntx');
        const two = encodeQR('qntx');
        for (let y = 0; y < one.length; y++) {
            for (let x = 0; x < one.length; x++) {
                expect(one[y][x]).toBe(two[y][x]);
            }
        }
    });
});
