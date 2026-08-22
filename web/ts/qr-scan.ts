/**
 * Reading a QR out of a camera frame.
 */

// The only codes this ever has to read are the ones web/ts/qr.ts writes: byte
// mode, level M, versions one through ten. That is what keeps this the size it
// is — a general decoder carries four error correction levels and four modes.

// It runs per frame and is allowed to fail. A camera pointed at a screen gives
// thirty chances a second, so a frame that does not decode is not an error, it
// is the next frame.

const CAPACITY_VERSIONS = 10;

/** Per version: error correction codewords per block, then [count, data each]. */
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

const ALIGNMENT: number[][] = [
    [], [6, 18], [6, 22], [6, 26], [6, 30],
    [6, 34], [6, 22, 38], [6, 24, 42], [6, 26, 46], [6, 28, 50],
];

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
    return a === 0 || b === 0 ? 0 : EXP[LOG[a] + LOG[b]];
}

function div(a: number, b: number): number {
    return a === 0 ? 0 : EXP[LOG[a] + 255 - LOG[b]];
}

// Polynomials are coefficient lists, highest power first — the convention the
// error correction literature uses, kept so this reads like what it is.

function gfPow(x: number, power: number): number {
    return EXP[(((LOG[x] * power) % 255) + 255) % 255];
}

function gfInverse(x: number): number {
    return EXP[255 - LOG[x]];
}

function polyScale(p: number[], factor: number): number[] {
    return p.map(c => mul(c, factor));
}

function polyAdd(a: number[], b: number[]): number[] {
    const out = new Array(Math.max(a.length, b.length)).fill(0);
    for (let i = 0; i < a.length; i++) out[i + out.length - a.length] ^= a[i];
    for (let i = 0; i < b.length; i++) out[i + out.length - b.length] ^= b[i];
    return out;
}

function polyMul(a: number[], b: number[]): number[] {
    const out = new Array(a.length + b.length - 1).fill(0);
    for (let i = 0; i < a.length; i++) {
        for (let j = 0; j < b.length; j++) out[i + j] ^= mul(a[i], b[j]);
    }
    return out;
}

function polyEval(p: number[], x: number): number {
    let y = p[0];
    for (let i = 1; i < p.length; i++) y = mul(y, x) ^ p[i];
    return y;
}

/**
 * Reed-Solomon over GF(256): corrects up to half the error correction
 * codewords. Null is a block too damaged to be sure about, which is what tells
 * the caller this frame was not read rather than read wrongly.
 */
function correct(block: Uint8Array, ecCount: number): Uint8Array | null {
    const message = Array.from(block);

    // Syndromes, with the leading zero the locator search below indexes from.
    // All zero is a block that arrived intact.
    const syndromes = [0];
    for (let i = 0; i < ecCount; i++) syndromes.push(polyEval(message, gfPow(2, i)));
    if (syndromes.every(s => s === 0)) return block.slice(0, block.length - ecCount);

    // Berlekamp-Massey: the shortest polynomial whose roots are where the
    // errors are.
    let locator = [1];
    let previous = [1];
    for (let round = 0; round < ecCount; round++) {
        const at = round + 1;
        let delta = syndromes[at];
        for (let j = 1; j < locator.length; j++) {
            delta ^= mul(locator[locator.length - 1 - j], syndromes[at - j]);
        }
        previous = [...previous, 0];
        if (delta === 0) continue;

        if (previous.length > locator.length) {
            const next = polyScale(previous, delta);
            previous = polyScale(locator, gfInverse(delta));
            locator = next;
        }
        locator = polyAdd(locator, polyScale(previous, delta));
    }

    while (locator.length > 0 && locator[0] === 0) locator.shift();
    const errors = locator.length - 1;
    if (errors === 0 || errors * 2 > ecCount) return null;

    // Chien search. The locator is a product of (1 + X·x), so it vanishes at the
    // inverse of each X — looking for it at X itself finds nothing at all.
    const positions: number[] = [];
    for (let power = 0; power < message.length; power++) {
        if (polyEval(locator, gfInverse(gfPow(2, power))) === 0) {
            positions.push(message.length - 1 - power);
        }
    }
    if (positions.length !== errors) return null;

    // Forney: how wrong each of those positions is.
    const powers = positions.map(p => message.length - 1 - p);
    let errata = [1];
    for (const power of powers) errata = polyMul(errata, [gfPow(2, power), 1]);

    const reversed = [...syndromes].reverse();
    const product = polyMul(reversed, errata);
    const evaluator = product.slice(product.length - errata.length);

    const roots = powers.map(power => gfPow(2, power));
    const magnitudes = new Array(message.length).fill(0);
    for (let i = 0; i < roots.length; i++) {
        const inverse = gfInverse(roots[i]);

        // The locator's derivative at this root, as the product over the others.
        let derivative = 1;
        for (let j = 0; j < roots.length; j++) {
            if (j !== i) derivative = mul(derivative, 1 ^ mul(inverse, roots[j]));
        }
        if (derivative === 0) return null;

        magnitudes[positions[i]] = div(mul(roots[i], polyEval(evaluator, inverse)), derivative);
    }

    const fixed = polyAdd(message, magnitudes);

    // A correction that did not converge is silent corruption otherwise.
    for (let i = 0; i < ecCount; i++) {
        if (polyEval(fixed, gfPow(2, i)) !== 0) return null;
    }
    return Uint8Array.from(fixed.slice(0, fixed.length - ecCount));
}

/** Dark is 1. A local threshold, because a screen photographed in a room is
 *  never evenly lit. */
function binarize(data: Uint8ClampedArray, width: number, height: number): Uint8Array {
    const grey = new Uint8Array(width * height);
    for (let i = 0; i < grey.length; i++) {
        const p = i * 4;
        grey[i] = (data[p] * 77 + data[p + 1] * 150 + data[p + 2] * 29) >> 8;
    }

    const block = 16;
    const across = Math.ceil(width / block);
    const down = Math.ceil(height / block);
    const thresholds = new Float64Array(across * down).fill(NaN);

    let total = 0;
    let counted = 0;
    for (let by = 0; by < down; by++) {
        for (let bx = 0; bx < across; bx++) {
            let low = 255;
            let high = 0;
            const yEnd = Math.min(by * block + block, height);
            const xEnd = Math.min(bx * block + block, width);
            for (let y = by * block; y < yEnd; y++) {
                for (let x = bx * block; x < xEnd; x++) {
                    const value = grey[y * width + x];
                    if (value < low) low = value;
                    if (value > high) high = value;
                }
            }
            if (high - low <= 24) continue;
            thresholds[by * across + bx] = (low + high) / 2;
            total += (low + high) / 2;
            counted++;
        }
    }
    if (counted === 0) return new Uint8Array(width * height);
    const overall = total / counted;

    // A block with no contrast is all one thing, and which thing it is comes
    // from its neighbours. Deciding inside it makes a big code's own modules
    // vanish, because at close range one block fits inside one module.
    const settled = new Float64Array(across * down);
    for (let by = 0; by < down; by++) {
        for (let bx = 0; bx < across; bx++) {
            const own = thresholds[by * across + bx];
            if (!Number.isNaN(own)) {
                settled[by * across + bx] = own;
                continue;
            }
            let sum = 0;
            let seen = 0;
            for (let dy = -1; dy <= 1; dy++) {
                for (let dx = -1; dx <= 1; dx++) {
                    const nx = bx + dx;
                    const ny = by + dy;
                    if (nx < 0 || ny < 0 || nx >= across || ny >= down) continue;
                    const near = thresholds[ny * across + nx];
                    if (Number.isNaN(near)) continue;
                    sum += near;
                    seen++;
                }
            }
            settled[by * across + bx] = seen > 0 ? sum / seen : overall;
        }
    }

    const bits = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) {
        const row = Math.floor(y / block) * across;
        for (let x = 0; x < width; x++) {
            bits[y * width + x] = grey[y * width + x] < settled[row + Math.floor(x / block)] ? 1 : 0;
        }
    }
    return bits;
}

interface Finder {
    x: number;
    y: number;
    module: number;
    // How many scan rows agreed on it. A real finder is several modules tall,
    // so it is seen many times; a run that happened to fit the ratio once is not.
    seen: number;
}

// Enumerating triples is cubic, so a frame of noise needs a ceiling somewhere.
// These are backstops against a pathological frame, not a filter — a frame that
// hits them is a frame that did not decode, and the next one is along shortly.
const MOST_CANDIDATES = 64;
const MOST_PER_GROUP = 12;
const MOST_GROUPS = 8;

// How many of the ranked triples are worth sampling before giving up on the
// frame. The right one is usually first; being wrong about that is cheap.
const MOST_TRIPLES = 8;

/** The 1:1:3:1:1 run that only a finder pattern makes, found along rows and
 *  confirmed down the column through its centre. */
function findFinders(bits: Uint8Array, width: number, height: number): Finder[] {
    const found: Finder[] = [];

    const ratioHolds = (runs: number[]): number => {
        const unit = (runs[0] + runs[1] + runs[2] + runs[3] + runs[4]) / 7;
        if (unit < 1) return 0;
        const slack = unit * 0.6;
        const wants = [1, 1, 3, 1, 1];
        for (let i = 0; i < 5; i++) {
            if (Math.abs(runs[i] - wants[i] * unit) > slack * wants[i]) return 0;
        }
        return unit;
    };

    // The column through a candidate, which both confirms the pattern and says
    // where its centre actually is. The scan row is only somewhere inside it.
    const runsDown = (cx: number, cy: number): { runs: number[]; centre: number } | null => {
        if (cx < 0 || cx >= width || bits[cy * width + cx] !== 1) return null;

        const runs = [0, 0, 0, 0, 0];
        let up = 0;
        let y = cy;
        while (y >= 0 && bits[y * width + cx] === 1) { up++; y--; }
        while (y >= 0 && bits[y * width + cx] === 0) { runs[1]++; y--; }
        while (y >= 0 && bits[y * width + cx] === 1) { runs[0]++; y--; }

        let down = 0;
        y = cy + 1;
        while (y < height && bits[y * width + cx] === 1) { down++; y++; }
        while (y < height && bits[y * width + cx] === 0) { runs[3]++; y++; }
        while (y < height && bits[y * width + cx] === 1) { runs[4]++; y++; }

        runs[2] = up + down;
        if (runs[0] === 0 || runs[4] === 0) return null;
        return { runs, centre: cy + (down - up + 1) / 2 };
    };

    for (let y = 0; y < height; y += 2) {
        const runs = [0, 0, 0, 0, 0];
        let colour = bits[y * width];
        let count = 0;

        for (let x = 0; x <= width; x++) {
            // One past the end flushes the run in progress, so a code touching
            // the right edge of the frame is still five runs.
            const bit = x < width ? bits[y * width + x] : 1 - colour;
            if (bit === colour) {
                count++;
                continue;
            }

            runs.shift();
            runs.push(count);
            // The run just finished is runs[4]. Only a dark one can be the last
            // of 1:1:3:1:1, so only then is there anything to measure.
            if (colour === 1 && runs[0] > 0) {
                const unit = ratioHolds(runs);
                if (unit > 0) {
                    const centre = Math.round(x - runs[4] - runs[3] - runs[2] / 2);
                    const down = runsDown(centre, y);
                    if (down !== null && ratioHolds(down.runs) > 0) {
                        found.push({ x: centre, y: down.centre, module: unit, seen: 1 });
                    }
                }
            }
            colour = bit;
            count = 1;
        }
    }

    // Rows are scanned independently, so one finder is found many times. The
    // mean of all of them, not a running pairwise average — that one is
    // weighted towards whichever row came last, which is the bottom.
    const clusters: Array<{ x: number; y: number; module: number; seen: number }> = [];
    for (const candidate of found) {
        const near = clusters.find(c =>
            Math.abs(c.x / c.seen - candidate.x) < candidate.module * 3
            && Math.abs(c.y / c.seen - candidate.y) < candidate.module * 3);
        if (near) {
            near.x += candidate.x;
            near.y += candidate.y;
            near.module += candidate.module;
            near.seen++;
            continue;
        }
        clusters.push({ x: candidate.x, y: candidate.y, module: candidate.module, seen: 1 });
    }

    return clusters
        .sort((a, b) => b.seen - a.seen)
        .slice(0, MOST_CANDIDATES)
        .map(c => ({ x: c.x / c.seen, y: c.y / c.seen, module: c.module / c.seen, seen: c.seen }));
}

function distance(a: Finder, b: Finder): number {
    return Math.hypot(a.x - b.x, a.y - b.y);
}

interface Located {
    corner: Finder;
    right: Finder;
    below: Finder;
    version: number;
    size: number;
}

/**
 * Which three of the candidates are the finders. Not the three largest: a run
 * inside the data region can measure wider than a real finder, and picking by
 * size drops a real one for it.
 */
function rankTriples(finders: Finder[]): Located[] {
    if (finders.length < 3) return [];

    // Three finders of one code measure the same, so grouping by module size
    // comes first. Without it the search is over every candidate in the frame,
    // and a run inside somebody's data can be seen on more rows than a finder.
    const groups: Finder[][] = [];
    for (const candidate of finders) {
        const near = groups.find(g =>
            Math.abs(g[0].module - candidate.module) <= Math.max(g[0].module, candidate.module) * 0.25);
        if (near) near.push(candidate);
        else groups.push([candidate]);
    }

    const searchable = groups
        .filter(g => g.length >= 3)
        .slice(0, MOST_GROUPS)
        .map(g => g.sort((a, b) => b.seen - a.seen).slice(0, MOST_PER_GROUP));

    const ranked: Array<{ located: Located; error: number }> = [];

    for (const group of searchable) {
        for (let i = 0; i < group.length - 2; i++) {
            for (let j = i + 1; j < group.length - 1; j++) {
                for (let k = j + 1; k < group.length; k++) {
                const trio = [group[i], group[j], group[k]];
                const modules = trio.map(f => f.module);
                const mean = (modules[0] + modules[1] + modules[2]) / 3;
                if (mean <= 0) continue;

                const spread = (Math.max(...modules) - Math.min(...modules)) / mean;
                if (spread > 0.5) continue;

                const { corner, right, below } = orient(trio);
                const alongTop = distance(corner, right);
                const alongSide = distance(corner, below);
                const diagonal = distance(right, below);
                if (alongTop < mean * 7 || alongSide < mean * 7) continue;

                // And they sit on the corners of a square, so the two sides
                // match and the diagonal is root two of them.
                const square = Math.abs(alongTop - alongSide) / Math.max(alongTop, alongSide);
                const angle = Math.abs(diagonal - Math.SQRT2 * (alongTop + alongSide) / 2) / diagonal;

                const across = (alongTop + alongSide) / 2 / mean + 7;
                const version = Math.round((across - 17) / 4);
                if (version < 1 || version > CAPACITY_VERSIONS) continue;

                // Every dimension is 17 plus a multiple of four, so how far the
                // measurement had to be snapped says how good it was.
                const size = 17 + version * 4;
                const error = spread + square + angle + Math.abs(across - size) / size;
                ranked.push({ located: { corner, right, below, version, size }, error });
                }
            }
        }
    }

    return ranked.sort((a, b) => a.error - b.error).slice(0, MOST_TRIPLES).map(r => r.located);
}

/**
 * Whether a sampled grid is a QR at all. Three candidates can be square, the
 * same size, and still be three runs inside somebody's data — what they cannot
 * be is a code whose timing pattern alternates and whose format field decodes.
 */
function looksLikeACode(grid: Uint8Array, size: number): boolean {
    for (let i = 8; i < size - 8; i++) {
        if (grid[6 * size + i] !== (i % 2 === 0 ? 1 : 0)) return false;
        if (grid[i * size + 6] !== (i % 2 === 0 ? 1 : 0)) return false;
    }
    return maskFrom(grid, size) >= 0;
}

/** Which of the three is the corner, and which way round the other two go. */
function orient(finders: Finder[]): { corner: Finder; right: Finder; below: Finder } {
    const [a, b, c] = finders;
    const sides: Array<[number, Finder, Finder, Finder]> = [
        [distance(b, c), a, b, c],
        [distance(a, c), b, a, c],
        [distance(a, b), c, a, b],
    ];
    // The corner is opposite the longest side — the diagonal.
    sides.sort((one, two) => two[0] - one[0]);
    const [, corner, first, second] = sides[0];

    // Cross product says which of the two is clockwise from the corner.
    const cross = (first.x - corner.x) * (second.y - corner.y)
        - (first.y - corner.y) * (second.x - corner.x);
    return cross < 0
        ? { corner, right: second, below: first }
        : { corner, right: first, below: second };
}

type Transform = number[];

/** The projective map from module space to image space, from four point pairs. */
function homography(
    to: Array<[number, number]>,
    from: Array<[number, number]>,
): Transform | null {
    // Square to quadrilateral, for each side, then compose one with the other's
    // adjugate. This is the standard construction and avoids a solver.
    const square = (q: Array<[number, number]>): Transform => {
        const dx1 = q[1][0] - q[2][0];
        const dx2 = q[3][0] - q[2][0];
        const dx3 = q[0][0] - q[1][0] + q[2][0] - q[3][0];
        const dy1 = q[1][1] - q[2][1];
        const dy2 = q[3][1] - q[2][1];
        const dy3 = q[0][1] - q[1][1] + q[2][1] - q[3][1];

        if (dx3 === 0 && dy3 === 0) {
            return [
                q[1][0] - q[0][0], q[2][0] - q[1][0], q[0][0],
                q[1][1] - q[0][1], q[2][1] - q[1][1], q[0][1],
                0, 0, 1,
            ];
        }
        const denominator = dx1 * dy2 - dx2 * dy1;
        if (denominator === 0) return [];
        const a13 = (dx3 * dy2 - dx2 * dy3) / denominator;
        const a23 = (dx1 * dy3 - dx3 * dy1) / denominator;
        return [
            q[1][0] - q[0][0] + a13 * q[1][0], q[3][0] - q[0][0] + a23 * q[3][0], q[0][0],
            q[1][1] - q[0][1] + a13 * q[1][1], q[3][1] - q[0][1] + a23 * q[3][1], q[0][1],
            a13, a23, 1,
        ];
    };

    const adjugate = (m: Transform): Transform => [
        m[4] * m[8] - m[5] * m[7], m[2] * m[7] - m[1] * m[8], m[1] * m[5] - m[2] * m[4],
        m[5] * m[6] - m[3] * m[8], m[0] * m[8] - m[2] * m[6], m[2] * m[3] - m[0] * m[5],
        m[3] * m[7] - m[4] * m[6], m[1] * m[6] - m[0] * m[7], m[0] * m[4] - m[1] * m[3],
    ];

    const times = (a: Transform, b: Transform): Transform => {
        const out: Transform = [];
        for (let row = 0; row < 3; row++) {
            for (let col = 0; col < 3; col++) {
                out.push(a[row * 3] * b[col] + a[row * 3 + 1] * b[col + 3] + a[row * 3 + 2] * b[col + 6]);
            }
        }
        return out;
    };

    const source = square(to);
    const target = square(from);
    if (source.length === 0 || target.length === 0) return null;
    return times(target, adjugate(source));
}

function apply(m: Transform, x: number, y: number): [number, number] {
    const w = m[6] * x + m[7] * y + m[8];
    if (w === 0) return [-1, -1];
    return [(m[0] * x + m[1] * y + m[2]) / w, (m[3] * x + m[4] * y + m[5]) / w];
}

/** Reads the format field and returns the mask, or -1 when its BCH does not
 *  come back. Level is not read: everything here writes M. */
function maskFrom(grid: Uint8Array, size: number): number {
    let raw = 0;
    for (let i = 0; i < 15; i++) {
        const [x, y] = i < 6 ? [8, i]
            : i === 6 ? [8, 7]
            : i === 7 ? [8, 8]
            : i === 8 ? [7, 8]
            : [14 - i, 8];
        raw |= grid[y * size + x] << i;
    }
    const bits = raw ^ 0b101010000010010;

    let rest = bits;
    for (let i = 4; i >= 0; i--) {
        if (rest & (1 << (i + 10))) rest ^= 0b10100110111 << i;
    }
    if (rest !== 0 || (bits >> 13) !== 0b00) return -1;
    return (bits >> 10) & 0b111;
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

/** Which modules are function patterns, and therefore not data. */
function reservedMap(version: number, size: number): Uint8Array {
    const reserved = new Uint8Array(size * size);
    const mark = (x: number, y: number) => {
        if (x >= 0 && y >= 0 && x < size && y < size) reserved[y * size + x] = 1;
    };

    for (const [ox, oy] of [[0, 0], [size - 7, 0], [0, size - 7]]) {
        for (let dy = -1; dy <= 7; dy++) {
            for (let dx = -1; dx <= 7; dx++) mark(ox + dx, oy + dy);
        }
    }
    for (let i = 0; i < size; i++) {
        mark(i, 6);
        mark(6, i);
    }
    const centres = ALIGNMENT[version - 1];
    for (const cy of centres) {
        for (const cx of centres) {
            const nearFinder = (cx < 8 && cy < 8)
                || (cx > size - 9 && cy < 8)
                || (cx < 8 && cy > size - 9);
            if (nearFinder) continue;
            for (let dy = -2; dy <= 2; dy++) {
                for (let dx = -2; dx <= 2; dx++) mark(cx + dx, cy + dy);
            }
        }
    }
    for (let i = 0; i < 9; i++) {
        mark(8, i);
        mark(i, 8);
    }
    for (let i = 0; i < 8; i++) {
        mark(8, size - 1 - i);
        mark(size - 1 - i, 8);
    }
    if (version >= 7) {
        for (let i = 0; i < 18; i++) {
            const x = i % 3;
            const y = Math.floor(i / 3);
            mark(size - 11 + x, y);
            mark(y, size - 11 + x);
        }
    }
    return reserved;
}

/** The zigzag, unmasked, into codewords. */
function readCodewords(grid: Uint8Array, size: number, version: number, mask: number): Uint8Array {
    const reserved = reservedMap(version, size);
    const bits: number[] = [];
    let upward = true;

    for (let right = size - 1; right > 0; right -= 2) {
        if (right === 6) right = 5;
        for (let step = 0; step < size; step++) {
            const y = upward ? size - 1 - step : step;
            for (const x of [right, right - 1]) {
                if (reserved[y * size + x]) continue;
                const bit = grid[y * size + x] ^ (masked(x, y, mask) ? 1 : 0);
                bits.push(bit);
            }
        }
        upward = !upward;
    }

    const out = new Uint8Array(bits.length >> 3);
    for (let i = 0; i < out.length; i++) {
        let byte = 0;
        for (let b = 0; b < 8; b++) byte = (byte << 1) | bits[i * 8 + b];
        out[i] = byte;
    }
    return out;
}

/** Undoes the interleave and corrects each block. Null is a read that failed. */
function blocksOf(stream: Uint8Array, version: number): Uint8Array | null {
    const [ec, groups] = BLOCKS[version - 1];
    const sizes: number[] = [];
    for (const [count, size] of groups) {
        for (let i = 0; i < count; i++) sizes.push(size);
    }

    const data: number[][] = sizes.map(() => []);
    let cursor = 0;
    const longest = Math.max(...sizes);
    for (let i = 0; i < longest; i++) {
        for (let b = 0; b < sizes.length; b++) {
            if (i < sizes[b]) data[b].push(stream[cursor++]);
        }
    }
    const parity: number[][] = sizes.map(() => []);
    for (let i = 0; i < ec; i++) {
        for (let b = 0; b < sizes.length; b++) parity[b].push(stream[cursor++]);
    }

    const out: number[] = [];
    for (let b = 0; b < sizes.length; b++) {
        const whole = Uint8Array.from([...data[b], ...parity[b]]);
        const fixed = correct(whole, ec);
        if (fixed === null) return null;
        out.push(...fixed);
    }
    return Uint8Array.from(out);
}

/** Mode, length, payload. Only byte mode, which is all this node writes. */
function textOf(data: Uint8Array, version: number): string | null {
    const bits: number[] = [];
    for (const byte of data) {
        for (let i = 7; i >= 0; i--) bits.push((byte >> i) & 1);
    }
    const take = (count: number): number => {
        let value = 0;
        for (let i = 0; i < count; i++) value = (value << 1) | (bits.shift() ?? 0);
        return value;
    };

    if (take(4) !== 0b0100) return null;
    const length = take(version < 10 ? 8 : 16);
    if (length === 0 || length * 8 > bits.length) return null;

    const bytes = new Uint8Array(length);
    for (let i = 0; i < length; i++) bytes[i] = take(8);
    return new TextDecoder().decode(bytes);
}

/**
 * Reads one frame. Null is a frame that did not decode, which is the ordinary
 * case and not a failure — the next frame is along in a thirtieth of a second.
 */
export function scanFrame(data: Uint8ClampedArray, width: number, height: number): string | null {
    const found = sampleGrid(binarize(data, width, height), width, height);
    if (found === null) return null;
    return decodeGrid(found.grid, found.size, found.version);
}

/** Modules to text. */
function decodeGrid(grid: Uint8Array, size: number, version: number): string | null {
    const mask = maskFrom(grid, size);
    if (mask < 0) return null;

    const corrected = blocksOf(readCodewords(grid, size, version, mask), version);
    if (corrected === null) return null;
    return textOf(corrected, version);
}

/** Locates the code and reads its modules. */
function sampleGrid(bits: Uint8Array, width: number, height: number): {
    grid: Uint8Array;
    size: number;
    version: number;
} | null {
    for (const { corner, right, below, version, size } of rankTriples(findFinders(bits, width, height))) {
        // The fourth corner, estimated from the other three. A phone held
        // roughly square at a screen is nearly affine, and the alignment
        // pattern would only matter for a code much larger than these.
        const fourth: [number, number] = [
            right.x + below.x - corner.x,
            right.y + below.y - corner.y,
        ];

        const edge = 3.5;
        const map = homography(
            [[edge, edge], [size - edge, edge], [size - edge, size - edge], [edge, size - edge]],
            [[corner.x, corner.y], [right.x, right.y], fourth, [below.x, below.y]],
        );
        if (map === null) continue;

        const grid = new Uint8Array(size * size);
        let outside = false;
        for (let y = 0; y < size && !outside; y++) {
            for (let x = 0; x < size; x++) {
                const [sx, sy] = apply(map, x + 0.5, y + 0.5);
                const ix = Math.round(sx);
                const iy = Math.round(sy);
                if (ix < 0 || iy < 0 || ix >= width || iy >= height) {
                    outside = true;
                    break;
                }
                grid[y * size + x] = bits[iy * width + ix];
            }
        }
        if (outside || !looksLikeACode(grid, size)) continue;

        return { grid, size, version };
    }
    return null;
}
