/**
 * The dials, on the door, while the dev server is serving it.
 */

// Every number in the look is a guess until someone who can see it moves it.
// This is how they get moved without going through a second person.

import { PRESETS, type Field, type Knobs, type Mood } from './door-field';

interface Slider {
    key: keyof Knobs;
    min: number;
    max: number;
    step: number;
}

const SLIDERS: Slider[] = [
    { key: 'exposure', min: 0.005, max: 2, step: 0.005 },
    { key: 'zoom', min: 0.2, max: 6, step: 0.05 },
    { key: 'steps', min: 8, max: 128, step: 1 },
    { key: 'pace', min: 0, max: 3, step: 0.02 },
    { key: 'sat', min: 0, max: 1, step: 0.01 },
    { key: 'spectrum', min: 0, max: 1, step: 0.01 },
    { key: 'decay', min: 0.5, max: 0.999, step: 0.001 },
    { key: 'core', min: 10, max: 600, step: 5 },
    { key: 'lift', min: -2, max: 2, step: 0.01 },
    { key: 'halo', min: 2, max: 200, step: 1 },
    { key: 'haloAmp', min: 0, max: 2, step: 0.02 },
    { key: 'orbit', min: 0, max: 0.06, step: 0.001 },
    { key: 'cx', min: -2, max: 1, step: 0.0005 },
    { key: 'cy', min: -1.5, max: 1.5, step: 0.0005 },
];

const MOODS: Mood[] = ['rest', 'hover', 'committed', 'admitted'];

function trim(value: number): string {
    let text = value.toFixed(4);
    while (text.endsWith('0')) text = text.slice(0, -1);
    if (text.endsWith('.')) text = text.slice(0, -1);
    return text;
}

export function mountDials(door: HTMLElement, field: Field): void {
    const panel = document.createElement('div');
    panel.className = 'door-dials';

    const head = document.createElement('div');
    head.className = 'door-dials-head';

    const hold = document.createElement('button');
    let held = false;
    hold.textContent = 'live';
    hold.addEventListener('click', () => {
        held = !held;
        field.pin(held);
        hold.textContent = held ? 'held' : 'live';
        hold.classList.toggle('door-dials-on', held);
    });

    const copy = document.createElement('button');
    copy.textContent = 'copy';
    copy.addEventListener('click', () => {
        void navigator.clipboard.writeText(JSON.stringify(field.knobs(), null, 2));
        copy.textContent = 'copied';
        setTimeout(() => { copy.textContent = 'copy'; }, 1200);
    });

    head.append(hold, copy);

    const grain = document.createElement('div');
    grain.className = 'door-dials-grain';

    for (const mood of MOODS) {
        const jump = document.createElement('button');
        jump.textContent = mood;
        jump.addEventListener('click', () => {
            field.pin(false);
            field.mood(mood);
            field.pin(held);
        });
        head.append(jump);
    }

    const rows = document.createElement('div');
    rows.className = 'door-dials-rows';

    const inputs = new Map<keyof Knobs, { range: HTMLInputElement; out: HTMLElement }>();

    for (const { key, min, max, step } of SLIDERS) {
        const row = document.createElement('label');
        row.className = 'door-dials-row';

        const name = document.createElement('span');
        name.textContent = key;

        const out = document.createElement('span');
        out.className = 'door-dials-out';

        const range = document.createElement('input');
        range.type = 'range';
        range.min = String(min);
        range.max = String(max);
        range.step = String(step);
        range.addEventListener('input', () => {
            const value = Number(range.value);
            out.textContent = range.value;
            field.tune({ [key]: value } as Partial<Knobs>);
        });

        row.append(name, out, range);
        rows.append(row);
        inputs.set(key, { range, out });
    }

    const family = document.createElement('div');
    family.className = 'door-dials-presets';
    for (const preset of PRESETS) {
        const pick = document.createElement('button');
        pick.textContent = preset.name;
        pick.addEventListener('click', () => {
            field.tune({ cx: preset.c[0], cy: preset.c[1] });
        });
        family.append(pick);
    }

    panel.append(head, grain, family, rows);
    door.append(panel);

    // The dials show what is there, except while a hand is on one of them.
    setInterval(() => {
        grain.textContent = field.grain();
        const knobs = field.knobs();
        for (const [key, { range, out }] of inputs) {
            if (document.activeElement === range) continue;
            const value = knobs[key];
            range.value = String(value);
            out.textContent = trim(value);
        }
    }, 200);
}
