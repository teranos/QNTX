/**
 * A QR encoder: byte mode, error correction M, versions 1 through 10.
 */

// Written rather than pulled in, because a code on a screen that admits a device
// for thirty days should not be produced by something nobody in this repo can
// read. Everything below is ISO/IEC 18004; the tables are the standard's.

// M recovers about fifteen percent, which is what a phone camera pointed at a
// lit screen wants. Nothing here needs the other levels, so nothing here has
// their tables.

/** How many bytes fit, by version, in byte mode at level M. */
const CAPACITY = [14, 26, 42, 62, 84, 106, 122, 152, 180, 213];

/** Per version: error correction codewords per block, then the block groups as
 *  [count, data codewords each]. */
const BLOCKS: Array<[number, Array<[number, number]>]> = [
    [10, [[1, 16]]],
    [16, [[1, 28]]],
    [26, [[1, 44]]],
    [18, [[2, 32]]],
    [24, [[2, 43]]],
    [16, [[4, 27]]],
    [18, [[4, 31]]],
    [22, [[2, 38], [2, 39]]],
    [22, [[3, 36], [2, 37]]],
    [26, [[4, 43], [1, 44]]],
];

/** Alignment pattern centres, by version. */
const ALIGNMENT: number[][] = [
    [], [6, 18], [6, 22], [6, 26], [6, 30],
    [6, 34], [6, 22, 38], [6, 24, 42], [6, 26, 46], [6, 28, 50],
];

// GF(256) with the QR primitive polynomial. Multiplication is addition of
// logarithms, which is what makes the Reed-Solomon remainder cheap.
const EXP = new Uint8Array(512);
const LOG = new Uint8Array(256);
(() => {
    let x = 1;
    for (let i = 0; i < 255; i++) {
        EXP[i] = x;
        LOG[x] = i;
        x <<= 1;
        if (x & 0x100) x ^= 0x11d;
    }
    for (let i = 255; i < 512; i++) EXP[i] = EXP[i - 255];
})();

function mul(a: number, b: number): number {
    if (a === 0 || b === 0) return 0;
    return EXP[LOG[a] + LOG[b]];
}

/** The generator polynomial for n error correction codewords. */
function generator(n: number): Uint8Array {
    let poly = new Uint8Array([1]);
    for (let i = 0; i < n; i++) {
        const next = new Uint8Array(poly.length + 1);
        for (let j = 0; j < poly.length; j++) {
            next[j] ^= poly[j];
            next[j + 1] ^= mul(poly[j], EXP[i]);
        }
        poly = next;
    }
    return poly;
}

/** The remainder of dividing the data by the generator: the ECC codewords. */
function remainder(data: Uint8Array, n: number): Uint8Array {
    const gen = generator(n);
    const out = new Uint8Array(data.length + n);
    out.set(data);
    for (let i = 0; i < data.length; i++) {
        const factor = out[i];
        if (factor === 0) continue;
        for (let j = 0; j < gen.length; j++) {
            out[i + j] ^= mul(gen[j], factor);
        }
    }
    return out.slice(data.length);
}

/** Format information: level M, this mask, BCH(15,5), masked as the standard says. */
function formatBits(mask: number): number {
    const value = (0b00 << 3) | mask;
    let rest = value << 10;
    for (let i = 4; i >= 0; i--) {
        if (rest & (1 << (i + 10))) rest ^= 0b10100110111 << i;
    }
    return ((value << 10) | rest) ^ 0b101010000010010;
}

/** Version information, carried only from version 7 up. BCH(18,6). */
function versionBits(version: number): number {
    let rest = version << 12;
    for (let i = 5; i >= 0; i--) {
        if (rest & (1 << (i + 12))) rest ^= 0b1111100100 << i;
    }
    return (version << 12) | rest;
}

/** The smallest version this many bytes fit in. */
function versionFor(length: number): number {
    for (let v = 1; v <= CAPACITY.length; v++) {
        if (length <= CAPACITY[v - 1]) return v;
    }
    throw new Error(`${length} bytes is more than a version ${CAPACITY.length} QR holds at this error correction`);
}

/** Mode indicator, length, payload, terminator, padding — the codeword stream. */
function codewords(bytes: Uint8Array, version: number): Uint8Array {
    const [ec, groups] = BLOCKS[version - 1];
    let total = 0;
    for (const [count, size] of groups) total += count * size;

    const bits: number[] = [];
    const push = (value: number, width: number) => {
        for (let i = width - 1; i >= 0; i--) bits.push((value >> i) & 1);
    };

    push(0b0100, 4);
    // One length byte up to version 9, two from version 10 — the count field
    // widens with the version, and getting this wrong decodes as noise.
    push(bytes.length, version < 10 ? 8 : 16);
    for (const byte of bytes) push(byte, 8);

    for (let i = 0; i < 4 && bits.length < total * 8; i++) bits.push(0);
    while (bits.length % 8 !== 0) bits.push(0);

    const data = new Uint8Array(total);
    for (let i = 0; i < bits.length; i += 8) {
        let byte = 0;
        for (let b = 0; b < 8; b++) byte = (byte << 1) | bits[i + b];
        data[i / 8] = byte;
    }
    // The two pad codewords alternate for the rest, which is what the standard
    // asks for rather than zeroes.
    for (let i = bits.length / 8; i < total; i++) {
        data[i] = i % 2 === (bits.length / 8) % 2 ? 0xec : 0x11;
    }

    // Blocks are interleaved: one codeword from each in turn, data then ECC.
    const dataBlocks: Uint8Array[] = [];
    const ecBlocks: Uint8Array[] = [];
    let taken = 0;
    for (const [count, size] of groups) {
        for (let i = 0; i < count; i++) {
            const block = data.slice(taken, taken + size);
            taken += size;
            dataBlocks.push(block);
            ecBlocks.push(remainder(block, ec));
        }
    }

    const out: number[] = [];
    const longest = Math.max(...dataBlocks.map(b => b.length));
    for (let i = 0; i < longest; i++) {
        for (const block of dataBlocks) {
            if (i < block.length) out.push(block[i]);
        }
    }
    for (let i = 0; i < ec; i++) {
        for (const block of ecBlocks) out.push(block[i]);
    }
    return new Uint8Array(out);
}

type Grid = Int8Array[];

/** -1 is free, 0 and 1 are set modules. Function patterns are marked as they go
 *  so the data placement can skip them. */
function skeleton(version: number): { grid: Grid; reserved: boolean[][] } {
    const size = 21 + 4 * (version - 1);
    const grid: Grid = [];
    const reserved: boolean[][] = [];
    for (let y = 0; y < size; y++) {
        grid.push(new Int8Array(size).fill(-1));
        reserved.push(new Array(size).fill(false));
    }

    const set = (x: number, y: number, on: boolean) => {
        grid[y][x] = on ? 1 : 0;
        reserved[y][x] = true;
    };

    // Finders and their separators, in three corners.
    for (const [ox, oy] of [[0, 0], [size - 7, 0], [0, size - 7]]) {
        for (let dy = -1; dy <= 7; dy++) {
            for (let dx = -1; dx <= 7; dx++) {
                const x = ox + dx;
                const y = oy + dy;
                if (x < 0 || y < 0 || x >= size || y >= size) continue;
                const edge = dx === 0 || dx === 6 || dy === 0 || dy === 6;
                const core = dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4;
                const inside = dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6;
                set(x, y, inside && (edge || core));
            }
        }
    }

    // Timing: alternating modules along row and column six.
    for (let i = 8; i < size - 8; i++) {
        set(i, 6, i % 2 === 0);
        set(6, i, i % 2 === 0);
    }

    // Alignment, everywhere the centres meet except over a finder.
    const centres = ALIGNMENT[version - 1];
    for (const cy of centres) {
        for (const cx of centres) {
            const nearFinder = (cx < 8 && cy < 8)
                || (cx > size - 9 && cy < 8)
                || (cx < 8 && cy > size - 9);
            if (nearFinder) continue;
            for (let dy = -2; dy <= 2; dy++) {
                for (let dx = -2; dx <= 2; dx++) {
                    const ring = Math.max(Math.abs(dx), Math.abs(dy));
                    set(cx + dx, cy + dy, ring !== 1);
                }
            }
        }
    }

    // The dark module, which is always on and never data.
    set(8, size - 8, true);

    // Format areas are reserved now and written after the mask is chosen.
    for (let i = 0; i < 9; i++) {
        if (!reserved[8][i]) { grid[8][i] = 0; reserved[8][i] = true; }
        if (!reserved[i][8]) { grid[i][8] = 0; reserved[i][8] = true; }
    }
    for (let i = 0; i < 8; i++) {
        if (!reserved[8][size - 1 - i]) { grid[8][size - 1 - i] = 0; reserved[8][size - 1 - i] = true; }
        if (!reserved[size - 1 - i][8]) { grid[size - 1 - i][8] = 0; reserved[size - 1 - i][8] = true; }
    }

    if (version >= 7) {
        for (let i = 0; i < 18; i++) {
            const x = i % 3;
            const y = Math.floor(i / 3);
            grid[y][size - 11 + x] = 0;
            reserved[y][size - 11 + x] = true;
            grid[size - 11 + x][y] = 0;
            reserved[size - 11 + x][y] = true;
        }
    }

    return { grid, reserved };
}

/** Walks the two-module-wide columns upward then downward, skipping column six. */
function place(grid: Grid, reserved: boolean[][], stream: Uint8Array): void {
    const size = grid.length;
    let bit = 0;
    let upward = true;

    for (let right = size - 1; right > 0; right -= 2) {
        if (right === 6) right = 5;
        for (let step = 0; step < size; step++) {
            const y = upward ? size - 1 - step : step;
            for (const x of [right, right - 1]) {
                if (reserved[y][x]) continue;
                const byte = stream[bit >> 3];
                grid[y][x] = byte === undefined ? 0 : (byte >> (7 - (bit & 7))) & 1;
                bit++;
            }
        }
        upward = !upward;
    }
}

function masked(x: number, y: number, mask: number): boolean {
    switch (mask) {
        case 0: return (x + y) % 2 === 0;
        case 1: return y % 2 === 0;
        case 2: return x % 3 === 0;
        case 3: return (x + y) % 3 === 0;
        case 4: return (Math.floor(y / 2) + Math.floor(x / 3)) % 2 === 0;
        case 5: return ((x * y) % 2) + ((x * y) % 3) === 0;
        case 6: return (((x * y) % 2) + ((x * y) % 3)) % 2 === 0;
        default: return (((x + y) % 2) + ((x * y) % 3)) % 2 === 0;
    }
}

/** The standard's four penalties. Lower is easier for a camera to read. */
function penalty(grid: Grid): number {
    const size = grid.length;
    let score = 0;

    // Runs of five or more in a row or column.
    for (let i = 0; i < size; i++) {
        for (const read of [
            (j: number) => grid[i][j],
            (j: number) => grid[j][i],
        ]) {
            let run = 1;
            for (let j = 1; j < size; j++) {
                if (read(j) === read(j - 1)) {
                    run++;
                    continue;
                }
                if (run >= 5) score += run - 2;
                run = 1;
            }
            if (run >= 5) score += run - 2;
        }
    }

    // Two-by-two blocks of one colour.
    for (let y = 0; y < size - 1; y++) {
        for (let x = 0; x < size - 1; x++) {
            const v = grid[y][x];
            if (v === grid[y][x + 1] && v === grid[y + 1][x] && v === grid[y + 1][x + 1]) {
                score += 3;
            }
        }
    }

    // The finder-lookalike, which is what confuses a scanner most.
    const finder = [1, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0];
    const reverse = [0, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1];
    for (let i = 0; i < size; i++) {
        for (let j = 0; j <= size - 11; j++) {
            for (const pattern of [finder, reverse]) {
                let row = true;
                let column = true;
                for (let k = 0; k < 11; k++) {
                    if (grid[i][j + k] !== pattern[k]) row = false;
                    if (grid[j + k][i] !== pattern[k]) column = false;
                }
                if (row) score += 40;
                if (column) score += 40;
            }
        }
    }

    // Imbalance between dark and light.
    let dark = 0;
    for (let y = 0; y < size; y++) {
        for (let x = 0; x < size; x++) dark += grid[y][x];
    }
    const percent = (dark * 100) / (size * size);
    score += Math.floor(Math.abs(percent - 50) / 5) * 10;
    return score;
}

/**
 * Encodes text as a grid of 0 and 1, one entry per module, no quiet zone. The
 * caller decides how it is drawn and how much white surrounds it.
 */
export function encodeQR(text: string): Int8Array[] {
    const bytes = new TextEncoder().encode(text);
    const version = versionFor(bytes.length);
    const stream = codewords(bytes, version);
    const size = 21 + 4 * (version - 1);

    let best: Grid | null = null;
    let bestScore = Infinity;

    for (let mask = 0; mask < 8; mask++) {
        const { grid, reserved } = skeleton(version);
        place(grid, reserved, stream);

        for (let y = 0; y < size; y++) {
            for (let x = 0; x < size; x++) {
                if (!reserved[y][x] && masked(x, y, mask)) grid[y][x] ^= 1;
            }
        }

        const format = formatBits(mask);
        for (let i = 0; i < 15; i++) {
            const bit = (format >> i) & 1;
            // The two copies, laid out the way the standard places them around
            // the top-left finder and along the other two edges.
            const [ax, ay] = i < 6 ? [8, i]
                : i === 6 ? [8, 7]
                : i === 7 ? [8, 8]
                : [14 - i, 8];
            grid[ay][ax] = bit;

            const [bx, by] = i < 8 ? [size - 1 - i, 8] : [8, size - 15 + i];
            grid[by][bx] = bit;
        }

        if (version >= 7) {
            const info = versionBits(version);
            for (let i = 0; i < 18; i++) {
                const bit = (info >> i) & 1;
                const x = i % 3;
                const y = Math.floor(i / 3);
                grid[y][size - 11 + x] = bit;
                grid[size - 11 + x][y] = bit;
            }
        }

        const score = penalty(grid);
        if (score < bestScore) {
            bestScore = score;
            best = grid;
        }
    }

    if (best === null) {
        throw new Error('no mask produced a grid, which cannot happen unless the loop above changed');
    }
    return best;
}

/** Draws a grid as an SVG path, sized in modules with a four-module quiet zone. */
export function renderQR(text: string, pixels: number): SVGSVGElement {
    const grid = encodeQR(text);
    const quiet = 4;
    const span = grid.length + quiet * 2;

    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', `0 0 ${span} ${span}`);
    svg.setAttribute('width', String(pixels));
    svg.setAttribute('height', String(pixels));
    svg.setAttribute('shape-rendering', 'crispEdges');

    const ground = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    ground.setAttribute('width', String(span));
    ground.setAttribute('height', String(span));
    ground.setAttribute('fill', '#ffffff');
    svg.append(ground);

    let d = '';
    for (let y = 0; y < grid.length; y++) {
        for (let x = 0; x < grid.length; x++) {
            if (grid[y][x] === 1) d += `M${x + quiet} ${y + quiet}h1v1h-1z`;
        }
    }

    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', d);
    path.setAttribute('fill', '#000000');
    svg.append(path);
    return svg;
}
