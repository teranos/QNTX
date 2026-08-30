/**
 * What the door is lit by.
 */

import createREGL from 'regl';
import { log, SEG } from './logger';

// The door is a page someone is reading, not a game. Thirty is plenty and it
// halves the work.
const FPS = 30;

// One triangle bigger than the screen, so every pixel is a fragment and nothing
// is stitched across a seam.
const COVER = [[-1, -1], [3, -1], [-1, 3]];

const COVER_VERT = `
    precision highp float;
    attribute vec2 xy;
    void main() {
        gl_Position = vec4(xy, 0.0, 1.0);
    }
`;

/** How far the door has come towards letting you in. */
export type Mood = 'rest' | 'hover' | 'committed' | 'admitted' | 'refused' | 'stricken';

// Random samples clump, and nine of them leave a third of their noise behind.
// A Halton sequence spreads the same nine evenly across the pixel.
function halton(index: number, base: number): number {
    let out = 0;
    let f = 1 / base;
    let i = index;
    while (i > 0) {
        out += f * (i % base);
        i = Math.floor(i / base);
        f /= base;
    }
    return out;
}

interface Dials {
    sat: number;
    pace: number;
    steps: number;
    exposure: number;
    spectrum: number;
    zoom: number;
    halo: number;
    haloAmp: number;
    decay: number;
}

// Where the field is before it is anywhere: far out, nearly still, barely
// gathered. Nothing moves to it — it is only what everything comes from.
const DAWN: Dials = {
    sat: 0, pace: 0.04, steps: 22, exposure: 0.04, spectrum: 0,
    zoom: 9, halo: 30, haloAmp: 1, decay: 0.5,
};

// Coming into being is the shortest thing the door does. Anything longer is
// the door holding someone who came to go through it.
const DAWN_MS = 1000;

function easeOut(t: number): number {
    return 1 - Math.pow(1 - t, 3);
}

function between(from: Dials, to: Dials, k: number): Dials {
    const mix = (a: number, b: number) => a + (b - a) * k;
    return {
        sat: mix(from.sat, to.sat),
        pace: mix(from.pace, to.pace),
        steps: mix(from.steps, to.steps),
        exposure: mix(from.exposure, to.exposure),
        spectrum: mix(from.spectrum, to.spectrum),
        zoom: mix(from.zoom, to.zoom),
        halo: mix(from.halo, to.halo),
        haloAmp: mix(from.haloAmp, to.haloAmp),
        decay: mix(from.decay, to.decay),
    };
}

// Reaching for the door pushes into the set as well as lighting it. Detail that
// arrives by getting closer is detail you can see arriving.
const MOODS: Record<Mood, Dials> = {
    rest: { sat: 0, pace: 0.22, steps: 46, exposure: 0.2, spectrum: 0, zoom: 3, halo: 6, haloAmp: 0.7, decay: 0.89 },
    hover: { sat: 0.5, pace: 0.6, steps: 78, exposure: 0.8, spectrum: 0, zoom: 1.5, halo: 12, haloAmp: 0.6, decay: 0.84 },
    committed: { sat: 1, pace: 1, steps: 120, exposure: 1.3, spectrum: 0, zoom: 0.9, halo: 20, haloAmp: 0.5, decay: 0.8 },
    admitted: { sat: 1, pace: 1, steps: 120, exposure: 1.8, spectrum: 1, zoom: 0.3, halo: 30, haloAmp: 0.2, decay: 0.7 },
    // A refusal is loud too, and the wrong colour for it.
    refused: { sat: 1, pace: 1, steps: 120, exposure: 1.5, spectrum: 0, zoom: 0.9, halo: 24, haloAmp: 0.4, decay: 0.7 },
    // A node that never answered. The same wrong colour, further out and barely
    // moving — a refusal is an answer, and this is the absence of one.
    stricken: { sat: 1, pace: 0.15, steps: 120, exposure: 1.5, spectrum: 0.12, zoom: 2.45, halo: 100, haloAmp: 0.4, decay: 0.5 },
};

/** The colour a refusal is said in, whatever this node's own colour is. */
const NO = [1, 0.16, 0.12] as const;

// Reaching a mood takes about a third of a second, except arriving, which is
// the one moment the door is allowed to be sudden.
const EASE = 0.12;
const EASE_IN_FULL = 0.5;

// How far the constant strays from the node's own point.
const ORBIT = 0.014;

/** Julia constants that are known to be worth looking at. */
export const PRESETS: { name: string; c: [number, number] }[] = [
    { name: 'dendrite', c: [0, 1] },
    { name: 'rabbit', c: [-0.123, 0.745] },
    { name: 'san marco', c: [-0.75, 0] },
    { name: 'siegel', c: [-0.391, -0.587] },
    { name: 'airplane', c: [-1.7549, 0] },
    { name: 'dragon', c: [-0.8, 0.156] },
    { name: 'spiral', c: [-0.4, 0.6] },
    { name: 'feather', c: [0.285, 0.01] },
    { name: 'lightning', c: [-0.70176, -0.3842] },
    { name: 'galaxy', c: [-0.835, -0.2321] },
    { name: 'filament', c: [0.285, 0.535] },
    { name: 'cauliflower', c: [0.25, 0] },
    { name: 'frost', c: [-0.038088, 0.9754633] },
    { name: 'nebula', c: [-0.6, 0.6] },
    { name: 'thorn', c: [0.32, 0.043] },
    { name: 'coral', c: [-0.194, 0.6557] },
];

/** Everything about the look that is a number rather than a decision. */
export interface Knobs extends Dials {
    core: number;
    /** Where the set sits up or down the door, in the plane's own units. */
    lift: number;
    orbit: number;
    cx: number;
    cy: number;
}

export interface Field {
    /** The Julia constant this node's door is drawn from, and its colour. */
    seed(
        c: readonly [number, number],
        hue: readonly [number, number, number],
        stand: number,
        circle: number,
    ): void;
    mood(next: Mood): void;
    /** Dev only: what the field is actually rendering at, right now. */
    grain(): string;
    /** Dev only: hold a mood and read or overwrite what it is made of. */
    knobs(): Knobs;
    tune(patch: Partial<Knobs>): void;
    pin(held: boolean): void;
    stop(): void;
}

const DARK: Field = {
    seed() {}, mood() {}, pin() {}, tune() {}, stop() {}, grain: () => 'unlit',
    knobs: () => ({ ...MOODS.rest, core: 0, lift: 0, orbit: 0, cx: 0, cy: 0 }),
};

export function startField(canvas: HTMLCanvasElement): Field {
    const regl = createREGL({
        canvas,
        pixelRatio: window.devicePixelRatio || 1,
        extensions: [],
        optionalExtensions: [
            'oes_texture_half_float',
            'ext_color_buffer_half_float',
            'oes_texture_float',
            'webgl_color_buffer_float',
            // Blending into a float target. Implicitly enabled, and the console
            // asks for it to be said out loud.
            'ext_float_blend',
        ],
    });

    const halfFloat = regl.hasExtension('oes_texture_half_float')
        && regl.hasExtension('ext_color_buffer_half_float');
    const fullFloat = regl.hasExtension('oes_texture_float')
        && regl.hasExtension('webgl_color_buffer_float');

    log.info(SEG.UI, '[Door] field', {
        css: `${canvas.clientWidth}x${canvas.clientHeight}`,
        buffer: `${canvas.width}x${canvas.height}`,
        dpr: window.devicePixelRatio,
        depth: fullFloat ? 'float' : 'half float',
    });

    // Light that cannot exceed 1.0 cannot bloom, and a blur standing in for
    // bloom is the fake this door does not get to have.
    if (!halfFloat && !fullFloat) {
        log.warn(SEG.UI, '[Door] no float render target — the door stays unlit');
        regl.destroy();
        return DARK;
    }

    let base: readonly [number, number] = [-0.8, 0.156];
    let hue: readonly [number, number, number] = [0.21, 0.88, 0.54];
    let stale = false;

    let now: Dials = { ...DAWN };
    let want: Dials = { ...MOODS.rest };
    let ease = EASE;
    let settled = false;
    let born = 0;

    let core = 240;
    let lift = 0.04;
    let orbit = ORBIT;

    let stand = 1;
    let circle = 1;
    let held = false;
    let sample = 0;
    let refusing = false;
    let red = 0;

    // Advanced by pace rather than by the clock, so changing speed bends the
    // drift instead of jumping it.
    let drift = 0;
    let lastTime = 0;

    const store = regl.framebuffer({
        color: regl.texture({
            // Float targets are not filterable without another extension, and
            // this is read back one texel per pixel, so there is nothing to filter.
            // 32-bit where it exists. Sixteen bits of mantissa quantises into
            // visible mush once a small accumulated value is multiplied up.
            type: fullFloat ? 'float' : 'half float',
            min: 'nearest',
            mag: 'nearest',
            wrap: 'clamp',
        }),
        depth: false,
        stencil: false,
    });

    const gather = regl({
        framebuffer: store,
        vert: COVER_VERT,
        frag: `
            precision highp float;
            uniform vec2 size;
            uniform vec2 c;
            uniform vec2 jitter;
            uniform vec3 hue;
            uniform float zoom;
            uniform float steps;
            uniform float sat;
            uniform float spectrum;
            uniform float core;
            uniform float halo;
            uniform float haloAmp;
            uniform float lift;

            const int CEILING = 128;
            const float ESCAPE = 1024.0;

            void main() {
                // A different point inside the same pixel every frame. The
                // boundary is thinner than a pixel, so sampling only the centre
                // is what breaks it into dots.
                vec2 at = gl_FragCoord.xy + jitter;
                vec2 p = (at - 0.5 * size) / min(size.x, size.y) * zoom;
                p.y -= lift;

                vec2 z = p;
                vec2 dz = vec2(1.0, 0.0);
                float m = dot(z, z);
                float n = 0.0;

                for (int i = 0; i < CEILING; i++) {
                    if (m > ESCAPE || float(i) >= steps) break;
                    dz = 2.0 * vec2(z.x * dz.x - z.y * dz.y, z.x * dz.y + z.y * dz.x);
                    z = vec2(z.x * z.x - z.y * z.y, 2.0 * z.x * z.y) + c;
                    m = dot(z, z);
                    n += 1.0;
                }

                // Distance to the boundary. Points that never escaped are on or
                // inside it and have no distance to report.
                float r = sqrt(m);
                float d = r > 2.0 ? r * log(r) / length(dz) : 0.0;

                // A bright filament inside a soft surround. The core alone is a
                // hairline; the surround is what makes it read as light.
                float glow = d > 0.0
                    ? exp(-d * core) + exp(-d * halo) * haloAmp
                    : 0.0;

                float esc = n / max(steps, 1.0);
                vec3 wheel = 0.5 + 0.5 * cos(6.28318 * (esc * 4.0 + vec3(0.0, 0.33, 0.67)));
                vec3 tone = mix(vec3(1.0), hue, sat);

                gl_FragColor = vec4(mix(tone, wheel, spectrum) * glow, 1.0);
            }
        `,
        attributes: { xy: COVER },
        uniforms: {
            size: () => [canvas.width, canvas.height],
            // A small circle around the node's own point. The shape breathes
            // without wandering off to become some other node's.
            c: () => [
                base[0] + Math.cos(drift) * orbit * circle,
                base[1] + Math.sin(drift) * orbit * circle,
            ],
            jitter: () => {
                sample = (sample + 1) % 4096;
                return [halton(sample, 2) - 0.5, halton(sample, 3) - 0.5];
            },
            // A refusal is said in red whatever colour this node is.
            hue: () => [
                hue[0] + (NO[0] - hue[0]) * red,
                hue[1] + (NO[1] - hue[1]) * red,
                hue[2] + (NO[2] - hue[2]) * red,
            ],
            zoom: () => now.zoom * stand,
            steps: () => now.steps,
            sat: () => now.sat,
            spectrum: () => now.spectrum,
            core: () => core,
            halo: () => now.halo,
            haloAmp: () => now.haloAmp,
            lift: () => lift,
        },
        count: 3,
        depth: { enable: false },
        // Light lands on what is already there and what is already there fades.
        // Without the fade a still image only ever gets brighter.
        blend: {
            enable: true,
            func: { src: 'one', dst: 'constant color' },
            // regl resolves a function per draw; its types only admit the literal.
            color: (() => [now.decay, now.decay, now.decay, now.decay]) as unknown as [number, number, number, number],
        },
    });

    const present = regl({
        vert: COVER_VERT,
        frag: `
            precision highp float;
            uniform sampler2D store;
            uniform vec2 size;
            uniform float exposure;
            void main() {
                vec3 light = texture2D(store, gl_FragCoord.xy / size).rgb;
                gl_FragColor = vec4(vec3(1.0) - exp(-light * exposure), 1.0);
            }
        `,
        attributes: { xy: COVER },
        uniforms: {
            store,
            size: () => [canvas.width, canvas.height],
            exposure: () => now.exposure,
        },
        count: 3,
        depth: { enable: false },
    });

    let last = 0;
    let sized = '';

    // regl leaves a canvas it did not create at the HTML default of 300x150,
    // which is then stretched across the whole door. The element's real size in
    // device pixels is the only resolution worth rendering.
    function fit(): void {
        const dpr = window.devicePixelRatio || 1;
        const wide = Math.max(1, Math.round(canvas.clientWidth * dpr));
        const tall = Math.max(1, Math.round(canvas.clientHeight * dpr));
        if (canvas.width === wide && canvas.height === tall) return;
        canvas.width = wide;
        canvas.height = tall;
    }

    const loop = regl.frame(({ time }) => {
        if (time - last < 1 / FPS) return;
        const step = lastTime === 0 ? 0 : time - lastTime;
        lastTime = time;
        last = time;

        fit();

        const shape = `${canvas.width}x${canvas.height}`;
        if (shape !== sized) {
            sized = shape;
            store.resize(canvas.width, canvas.height);
            stale = true;
        }

        // Coming into being is measured against the clock, not fed a fraction
        // per frame — that is the only way the end of it can be the slow part.
        if (!settled && !held) {
            if (born === 0) born = time;
            const t = Math.min(1, (time - born) / (DAWN_MS / 1000));
            now = between(DAWN, want, easeOut(t));
            if (t >= 1) settled = true;
        } else if (!held) {
            now = {
                sat: now.sat + (want.sat - now.sat) * ease,
                pace: now.pace + (want.pace - now.pace) * ease,
                steps: now.steps + (want.steps - now.steps) * ease,
                exposure: now.exposure + (want.exposure - now.exposure) * ease,
                spectrum: now.spectrum + (want.spectrum - now.spectrum) * ease,
                zoom: now.zoom + (want.zoom - now.zoom) * ease,
                halo: now.halo + (want.halo - now.halo) * ease,
                haloAmp: now.haloAmp + (want.haloAmp - now.haloAmp) * ease,
                decay: now.decay + (want.decay - now.decay) * ease,
            };
        }

        drift += step * now.pace * 0.25;
        red += ((refusing ? 1 : 0) - red) * ease;

        // Light collected from a different fractal is not this one's.
        if (stale) {
            stale = false;
            regl.clear({ framebuffer: store, color: [0, 0, 0, 1] });
        }

        gather();
        present();
    });

    return {
        seed(nextC, nextHue, nextStand, nextCircle) {
            hue = nextHue;
            stand = nextStand;
            circle = nextCircle;
            if (nextC[0] === base[0] && nextC[1] === base[1]) return;
            base = nextC;
            stale = true;
        },
        mood(next) {
            if (held) return;
            want = MOODS[next];
            // Both wear the red: one was told no, the other got no answer.
            refusing = next === 'refused' || next === 'stricken';
            // While it is still coming into being it keeps its own pace, or the
            // first mood set snaps it into place mid-arrival.
            if (!settled) return;
            // Arriving and being turned away are the two moments the door is
            // allowed to be sudden.
            ease = next === 'admitted' || next === 'refused' ? EASE_IN_FULL : EASE;
        },
        grain: () => [
            `css ${canvas.clientWidth}x${canvas.clientHeight}`,
            `buf ${canvas.width}x${canvas.height}`,
            `dpr ${window.devicePixelRatio}`,
            fullFloat ? 'f32' : 'f16',
        ].join('  '),
        knobs: () => ({ ...now, core, lift, orbit, cx: base[0], cy: base[1] }),
        tune(patch) {
            if (patch.core !== undefined) core = patch.core;
            if (patch.lift !== undefined) lift = patch.lift;
            if (patch.orbit !== undefined) orbit = patch.orbit;

            if (patch.cx !== undefined || patch.cy !== undefined) {
                base = [patch.cx ?? base[0], patch.cy ?? base[1]];
                stale = true;
            }

            now = {
                sat: patch.sat ?? now.sat,
                pace: patch.pace ?? now.pace,
                steps: patch.steps ?? now.steps,
                exposure: patch.exposure ?? now.exposure,
                spectrum: patch.spectrum ?? now.spectrum,
                zoom: patch.zoom ?? now.zoom,
                halo: patch.halo ?? now.halo,
                haloAmp: patch.haloAmp ?? now.haloAmp,
                decay: patch.decay ?? now.decay,
            };
        },
        pin(next) {
            held = next;
        },
        stop() {
            loop.cancel();
            regl.destroy();
        },
    };
}
