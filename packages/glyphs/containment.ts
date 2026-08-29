/**
 * The Containment Axioma, enforced by the package that states it.
 *
 * A glyph never hides its own content. Whatever a caller puts inside gives way
 * and wraps rather than running under the window's edge, where it can be
 * neither read nor pressed — and the caller does not have to know any of it.
 *
 * This lives here rather than in a consumer's stylesheet because an axiom a
 * consumer has to remember is not an axiom. A glyph carries it wherever it goes.
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
}
.glyph-content-area table {
    width: 100%;
    table-layout: fixed;
}
.glyph-content-area td,
.glyph-content-area th {
    word-break: break-word;
    overflow-wrap: break-word;
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
