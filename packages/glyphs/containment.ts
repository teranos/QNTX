/**
 * The Containment Axioma, held by the package that states it.
 *
 * A glyph shows everything it holds. What a caller puts inside gives way until
 * it fits, and the caller knows nothing about that.
 *
 * A glyph carries these rules wherever it goes. An axiom a consumer has to
 * remember belongs to the consumer.
 */

const STYLE_ID = 'glyphs-containment';

/** The rules a glyph holds its content with. */
export const CONTAINMENT_CSS = `
.glyph-content-area {
    min-width: 0;
    min-height: 0;
}
.glyph-content-area * {
    max-width: 100%;
    box-sizing: border-box;
    /* anywhere, not break-word: both wrap at an overflow, and only this one
       lets the box report a smaller minimum, which is what makes a long
       unbroken string stop deciding how wide a glyph has to be. */
    overflow-wrap: anywhere;
}
.glyph-content-area table {
    max-width: 100%;
}
`;

/**
 * Put the containment rules in the document, once.
 *
 * Called wherever a glyph builds its content area. Being idempotent is the
 * point: every manifestation may ask, and the document gets one copy.
 */
export function ensureContainment(doc: Document = document): void {
    if (doc.getElementById(STYLE_ID)) return;
    const style = doc.createElement('style');
    style.id = STYLE_ID;
    style.textContent = CONTAINMENT_CSS;
    // Head first, so a consumer's own stylesheet can still say more specific
    // things about its own glyphs without fighting an injected rule.
    (doc.head ?? doc.documentElement).prepend(style);
}
