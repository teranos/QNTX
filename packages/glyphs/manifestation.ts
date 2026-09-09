/**
 * The manifestations a glyph can take.
 *
 * AXIOMAS.md: "A morph is a state transition of a glyph between manifestations."
 * That names the noun and the verb. This names the manifestations, once, so the
 * type, the stylesheets and the morph functions stop each keeping their own list.
 *
 * Fullscreen is not among them. It is what `canvas` means, and a height a
 * `panel` reaches by being dragged (.glyph-panel--fullscreen).
 */

/** What the table records about a manifestation. */
export interface ManifestationFacts {
    /**
     * Whether a tray dot opens as this. GlyphRun.morphGlyph() dispatches on
     * exactly the entries marked true; the rest are reached another way, or are
     * where a morph begins.
     */
    readonly opensFromTray: boolean;
}

/**
 * Every manifestation, in the order a glyph meets them.
 *
 * Each entry is what a file in this package already calls itself — the comment
 * says which file, so a name here can always be checked against the code that
 * implements it.
 */
export const MANIFESTATIONS = Object.freeze({
    /**
     * Resting in the tray. `applyRestingDotGeometry()` puts a glyph here at birth
     * and when an existing element joins the tray (run.ts), and on the way back
     * from panel (manifestations/panel.ts) and window (manifestations/morphology.ts).
     * Where a morph starts, never where one ends.
     */
    dot: { opensFromTray: false },

    /**
     * The dot expanded by pointer nearness. proximity.ts calls itself "Proximity
     * morphing" and states the sequence this list follows: "The element persists
     * through: dot → proximity → window → dot". A way there, not a destination.
     */
    proximity: { opensFromTray: false },

    /** Floating, with chrome. manifestations/window.ts. */
    window: { opensFromTray: true },

    /**
     * Anchored to an edge at full height, snapping to fullscreen when dragged past
     * 90% of the viewport. manifestations/panel.ts.
     */
    panel: { opensFromTray: true },

    /**
     * "Canvas Manifestation - Fullscreen, no chrome" — the workspace itself,
     * which is a glyph. manifestations/canvas.ts.
     */
    canvas: { opensFromTray: true },

    /**
     * "Canvas-Placed Manifestation" — a glyph sitting on that workspace, with
     * container, position, drag, title bar and resize. manifestations/canvas-placed.ts.
     * Reached by being placed, not by a dot being opened.
     */
    canvasPlaced: { opensFromTray: false },

    /**
     * Following the pointer during placement. cursor.ts: "not persisted, have no
     * chrome, and do not participate in the tray morph lifecycle."
     */
    cursor: { opensFromTray: false },
} as const satisfies Record<string, ManifestationFacts>);

export type Manifestation = keyof typeof MANIFESTATIONS;

/**
 * What a tray dot opens as — the entries above marked `opensFromTray`.
 *
 * Derived rather than listed, so it cannot drift from the table it narrows.
 */
export type TrayDestination = {
    [M in Manifestation]: (typeof MANIFESTATIONS)[M]['opensFromTray'] extends true ? M : never;
}[Manifestation];

export const TRAY_DESTINATIONS: readonly TrayDestination[] = Object.freeze(
    (Object.keys(MANIFESTATIONS) as Manifestation[]).filter(
        (m): m is TrayDestination => MANIFESTATIONS[m].opensFromTray,
    ),
);

/** Whether a name is in the table. Own keys only — `toString` is not a manifestation. */
export function isManifestation(name: string): name is Manifestation {
    return Object.prototype.hasOwnProperty.call(MANIFESTATIONS, name);
}

/** Whether a name is something a tray dot can open as. */
export function isTrayDestination(name: string): name is TrayDestination {
    return isManifestation(name) && MANIFESTATIONS[name].opensFromTray;
}
