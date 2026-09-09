/**
 * The manifestations a glyph can take.
 *
 * AXIOMAS.md: "A morph is a state transition of a glyph between manifestations."
 * That names the noun and the verb. This names the manifestations, once, so the
 * type, the stylesheets and the morph functions stop each keeping their own list.
 *
 * Fullscreen is not among them, because three of them are it: `workspace` is
 * edge to edge, `canvasExpanded` is edge to edge, and a `panel` dragged past
 * 90% of the viewport becomes edge to edge (.glyph-panel--fullscreen). One
 * word for three manifestations names none of them.
 */

/**
 * Every manifestation, in the order a glyph meets them, and whether a tray dot
 * opens as it.
 *
 * `opensFromTray` is what GlyphRun.morphGlyph() dispatches on: exactly the
 * entries marked true; the rest are reached another way, or are where a morph
 * begins. It is written as a literal `true`/`false` rather than `boolean` so
 * TrayDestination can derive from it.
 *
 * Each name is what a file in this package already calls itself — the comment
 * says which file, so a name here can always be checked against the code that
 * implements it.
 *
 * The values live in MANIFESTATIONS below, and the compiler holds the two
 * together both ways: a row here with no value there fails, and a value there
 * with no row here fails too.
 */
export interface ManifestationTable {
    /**
     * Resting in the tray. `applyRestingDotGeometry()` puts a glyph here at birth
     * and when an existing element joins the tray (run.ts), and on the way back
     * from panel (manifestations/panel.ts) and window (manifestations/morphology.ts).
     * Where a morph starts, never where one ends.
     */
    readonly dot: { readonly opensFromTray: false };

    /**
     * The dot expanded by pointer nearness. proximity.ts calls itself "Proximity
     * morphing" and states the sequence this list follows: "The element persists
     * through: dot → proximity → window → dot". A way there, not a destination.
     */
    readonly proximity: { readonly opensFromTray: false };

    /** Floating, with chrome. manifestations/window.ts. */
    readonly window: { readonly opensFromTray: true };

    /**
     * Anchored to an edge at full height, snapping to fullscreen when dragged past
     * 90% of the viewport. manifestations/panel.ts.
     */
    readonly panel: { readonly opensFromTray: true };

    /**
     * "Canvas Manifestation - Fullscreen, no chrome" — the workspace itself,
     * which is a glyph. manifestations/canvas.ts.
     *
     * Named for what it is rather than for its file, the one row where those
     * differ. `canvas` sat one suffix from `canvasPlaced` while meaning the
     * opposite thing — the surface, not a glyph on it. The word was already
     * here: canvas-glyph.ts:81 gives it `id: 'canvas-workspace'`, and
     * canvas-placed.ts calls its subjects "glyphs on the canvas workspace".
     */
    readonly workspace: { readonly opensFromTray: true };

    /**
     * "Canvas-Placed Manifestation" — a glyph sitting on that workspace, with
     * container, position, drag, title bar and resize. manifestations/canvas-placed.ts.
     * Reached by being placed, not by a dot being opened.
     */
    readonly canvasPlaced: { readonly opensFromTray: false };

    /**
     * "Canvas-Expanded Manifestation" — a canvas-placed glyph filling the
     * viewport, reparented to document.body. The host's, not the package's:
     * web/ts/components/glyph/manifestations/canvas-expanded.ts.
     *
     * It fills the viewport like `workspace` and shares nothing else with it —
     * it comes from a placed glyph rather than from the tray, and goes back to
     * one. Listed because it is a manifestation a glyph can be in, and a list
     * that omits one is how "fullscreen" ended up meaning two things.
     */
    readonly canvasExpanded: { readonly opensFromTray: false };

    /**
     * Following the pointer during placement. cursor.ts: "not persisted, have no
     * chrome, and do not participate in the tray morph lifecycle."
     */
    readonly cursor: { readonly opensFromTray: false };
}

// Annotated here rather than only on MANIFESTATIONS: a literal assigned straight
// to an annotated name is checked for excess properties too, so a value with no
// row above is caught. Passed through Object.freeze() it would not be.
const TABLE: ManifestationTable = {
    dot: { opensFromTray: false },
    proximity: { opensFromTray: false },
    window: { opensFromTray: true },
    panel: { opensFromTray: true },
    workspace: { opensFromTray: true },
    canvasPlaced: { opensFromTray: false },
    canvasExpanded: { opensFromTray: false },
    cursor: { opensFromTray: false },
};

export const MANIFESTATIONS: ManifestationTable = Object.freeze(TABLE);

// Each row too, not just the table. TRAY_DESTINATIONS is computed once below
// while isTrayDestination reads the table live, so a writable row lets the two
// answer differently for the same name — the drift this file exists to end.
for (const row of Object.values(MANIFESTATIONS)) Object.freeze(row);

export type Manifestation = keyof ManifestationTable;

/**
 * What a tray dot opens as — the entries above marked `opensFromTray`.
 *
 * Derived rather than listed, so it cannot drift from the table it narrows.
 */
export type TrayDestination = {
    [M in Manifestation]: ManifestationTable[M]['opensFromTray'] extends true ? M : never;
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
