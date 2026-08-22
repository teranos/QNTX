/**
 * Encoder to decoder and back, which is the only end-to-end check either half
 * has.
 */

// This is also the encoder's real test — a wrong codeword stream fails here and
// nowhere else.

import { describe, test, expect } from 'bun:test';
import { encodeQR } from './qr';
import { scanFrame } from './qr-scan';

/** Paints a grid into an RGBA buffer the way a camera would see it on a screen:
 *  square on, evenly lit, with a quiet zone. */
function paint(text: string, scale: number, quiet = 4): {
    data: Uint8ClampedArray;
    width: number;
    height: number;
} {
    const grid = encodeQR(text);
    const span = (grid.length + quiet * 2) * scale;
    const data = new Uint8ClampedArray(span * span * 4);

    for (let y = 0; y < span; y++) {
        for (let x = 0; x < span; x++) {
            const mx = Math.floor(x / scale) - quiet;
            const my = Math.floor(y / scale) - quiet;
            const inside = mx >= 0 && my >= 0 && mx < grid.length && my < grid.length;
            const dark = inside && grid[my][mx] === 1;
            const p = (y * span + x) * 4;
            data[p] = data[p + 1] = data[p + 2] = dark ? 0 : 255;
            data[p + 3] = 255;
        }
    }
    return { data, width: span, height: span };
}

function roundTrip(text: string, scale = 6): string | null {
    const { data, width, height } = paint(text, scale);
    return scanFrame(data, width, height);
}

describe('scanFrame — a code this node drew', () => {
    test('a short payload comes back', () => {
        expect(roundTrip('qntx')).toBe('qntx');
    });

    test('a connect URL comes back', () => {
        const url = 'https://q.sbvh.nl/branch/user/#connect=8f14e45fceea167a5a36dedd4bea2543';
        expect(roundTrip(url)).toBe(url);
    });

    test('a TestFlight URL comes back', () => {
        const url = 'https://testflight.apple.com/join/AbCdEfGh';
        expect(roundTrip(url)).toBe(url);
    });

    test('every version this encoder writes reads back', () => {
        for (const length of [14, 15, 42, 62, 84, 106, 122, 152, 180, 213]) {
            const payload = 'x'.repeat(length);
            expect(roundTrip(payload)).toBe(payload);
        }
    });

    test('a bigger scale is still the same string', () => {
        expect(roundTrip('https://q.sbvh.nl/#connect=abcdef', 11)).toBe('https://q.sbvh.nl/#connect=abcdef');
    });
});

describe('scanFrame — frames that are not a code', () => {
    test('a blank frame decodes to nothing', () => {
        const data = new Uint8ClampedArray(200 * 200 * 4).fill(255);
        expect(scanFrame(data, 200, 200)).toBeNull();
    });

    test('noise decodes to nothing', () => {
        const data = new Uint8ClampedArray(200 * 200 * 4);
        for (let i = 0; i < data.length; i += 4) {
            const value = (i * 2654435761) % 256;
            data[i] = data[i + 1] = data[i + 2] = value;
            data[i + 3] = 255;
        }
        expect(scanFrame(data, 200, 200)).toBeNull();
    });
});

describe('scanFrame — the error correction earns its place', () => {
    // A decoder that only read perfect frames would fail on every real camera.
    test('a damaged code still comes back', () => {
        const url = 'https://q.sbvh.nl/#connect=8f14e45fceea167a';
        const scale = 6;
        const { data, width, height } = paint(url, scale);

        // Blot out a few modules well inside the data region, away from the
        // finders and the format field.
        const quiet = 4;
        for (let my = 12; my < 15; my++) {
            for (let mx = 12; mx < 15; mx++) {
                for (let y = 0; y < scale; y++) {
                    for (let x = 0; x < scale; x++) {
                        const px = (mx + quiet) * scale + x;
                        const py = (my + quiet) * scale + y;
                        const p = (py * width + px) * 4;
                        data[p] = data[p + 1] = data[p + 2] = 0;
                    }
                }
            }
        }

        expect(scanFrame(data, width, height)).toBe(url);
    });
});
