/**
 * Shared cache of predicates that have active watchers.
 * Used to render the ⏿ observer glyph next to watched predicates.
 *
 * Eye color follows spice saturation (Dune):
 *   - No fires ever → near-white, faded
 *   - Active + low dilation (strained) → bright spice-blue, vivid glow
 *   - Active + normal dilation → mid spice-blue
 *   - Active + high dilation (relaxed) → deep sea blue, subdued
 */

export interface WatcherInfo {
    names: string[];
    totalFires: number;
}

// Maps predicate -> watcher info
let cache: Map<string, WatcherInfo> = new Map();
let fetched = false;
let listeners: Array<() => void> = [];
let currentDilation = 1.0;

export function getWatchersByPredicate(): Map<string, WatcherInfo> {
    if (!fetched) refresh();
    return cache;
}

export function setDilation(d: number): void {
    currentDilation = d;
}

export function getDilation(): number {
    return currentDilation;
}

export function onWatcherPredicatesChanged(fn: () => void): () => void {
    listeners.push(fn);
    return () => { listeners = listeners.filter(l => l !== fn); };
}

export function refresh(): void {
    fetched = true;
    fetch('/api/watchers')
        .then(r => r.json())
        .then((watchers: any[]) => {
            const byPred = new Map<string, WatcherInfo>();
            for (const w of watchers) {
                if (!w.enabled) continue;
                const predicates = typeof w.predicates === 'string' ? JSON.parse(w.predicates) : w.predicates;
                const fires = w.fire_count || 0;
                if (Array.isArray(predicates)) {
                    for (const p of predicates) {
                        const existing = byPred.get(p);
                        if (existing) {
                            existing.names.push(w.name);
                            existing.totalFires += fires;
                        } else {
                            byPred.set(p, { names: [w.name], totalFires: fires });
                        }
                    }
                }
            }
            cache = byPred;
            for (const fn of listeners) fn();
        })
        .catch(() => { /* watchers unavailable */ });
}

/**
 * Compute eye color and glow based on fire activity and dilation.
 *
 * Dilation range: typically 0.25 (strained) to 2.0+ (relaxed), 1.0 = normal.
 * Low dilation → bright spice-blue (vivid, burning)
 * High dilation → deep sea blue (dark, calm)
 * No fires → near-white, faded
 */
export function eyeStyle(info: WatcherInfo): { color: string; shadow: string } {
    if (info.totalFires === 0) {
        return { color: 'rgba(180, 200, 230, 0.45)', shadow: 'none' };
    }

    const d = currentDilation;

    // Map dilation to a 0–1 range: 0 = strained (low dilation), 1 = relaxed (high dilation)
    // Clamp dilation between 0.25 and 2.0 for the interpolation
    const clamped = Math.max(0.25, Math.min(2.0, d));
    const t = (clamped - 0.25) / (2.0 - 0.25); // 0 = strained, 1 = relaxed

    // Spice-blue (bright, vivid) → deep sea blue (dark, calm)
    // Strained: rgb(105, 190, 255) — bright spice
    // Relaxed:  rgb(30, 80, 140)   — deep sea
    const r = Math.round(105 + (30 - 105) * t);
    const g = Math.round(190 + (80 - 190) * t);
    const b = Math.round(255 + (140 - 255) * t);

    // Glow intensity: strong when strained, subdued when relaxed
    const glowOpacity = 0.6 - 0.4 * t; // 0.6 → 0.2
    const glowRadius = 8 - 4 * t;       // 8px → 4px

    return {
        color: `rgb(${r}, ${g}, ${b})`,
        shadow: `0 0 ${glowRadius}px rgba(${r}, ${g}, ${b}, ${glowOpacity})`,
    };
}
