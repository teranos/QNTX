/**
 * The manifestations a glyph can take.
 *
 * AXIOMAS.md: "A morph is a state transition of a glyph between manifestations."
 * That names the noun and the verb. This names the manifestations, once, so the
 * type, the stylesheets and the morph functions stop each keeping their own list.
 *
 * Every entry here is what a file in this package already calls itself:
 *
 *   dot           the resting form in the tray. applyRestingDotGeometry() puts a
 *                 glyph here at birth (run.ts), when an existing element joins the
 *                 tray (run.ts adopt), and on the way back from panel and window
 *                 (manifestations/panel.ts, manifestations/morphology.ts).
 *   proximity     the dot expanded by pointer nearness. proximity.ts calls itself
 *                 "Proximity morphing" and states the sequence this list follows:
 *                 "The element persists through: dot → proximity → window → dot".
 *   window        floating, with chrome. manifestations/window.ts.
 *   panel         anchored to an edge at full height, snapping to fullscreen past
 *                 90% of the viewport. manifestations/panel.ts.
 *   canvas        "Canvas Manifestation - Fullscreen, no chrome" — the workspace
 *                 itself, which is a glyph. manifestations/canvas.ts.
 *   canvasPlaced  "Canvas-Placed Manifestation" — a glyph sitting on that
 *                 workspace, with container, position, drag, title bar and resize.
 *                 manifestations/canvas-placed.ts.
 *   cursor        following the pointer during placement. cursor.ts: "not
 *                 persisted, have no chrome, and do not participate in the tray
 *                 morph lifecycle."
 *
 * Fullscreen is not on the list. It is what `canvas` means, and a height a
 * `panel` reaches by being dragged (.glyph-panel--fullscreen).
 */

/** Every manifestation, in the order a glyph meets them. */
export const MANIFESTATIONS = Object.freeze([
    'dot',
    'proximity',
    'window',
    'panel',
    'canvas',
    'canvasPlaced',
    'cursor',
] as const);

export type Manifestation = (typeof MANIFESTATIONS)[number];

/**
 * What a tray dot opens as.
 *
 * Narrower than the list, and deliberately: `dot` is where the morph starts,
 * `proximity` is the way there, `canvasPlaced` is reached by placing rather than
 * by opening, and `cursor` stands outside the tray morph lifecycle entirely.
 * GlyphRun.morphGlyph() dispatches on exactly these three.
 */
export const TRAY_DESTINATIONS = Object.freeze([
    'window',
    'panel',
    'canvas',
] as const);

export type TrayDestination = (typeof TRAY_DESTINATIONS)[number];

/** Whether a name is on the list. */
export function isManifestation(name: string): name is Manifestation {
    return (MANIFESTATIONS as readonly string[]).includes(name);
}

/** Whether a name is something a tray dot can open as. */
export function isTrayDestination(name: string): name is TrayDestination {
    return (TRAY_DESTINATIONS as readonly string[]).includes(name);
}
