/**
 * Symbol rendering — the one way a glyph.symbol becomes DOM.
 *
 * `glyph.symbol` is the datum; this span is its visual expression. Every
 * native renderer (generic title bars, canvas-placed) goes through here,
 * so there is a single mechanism for one field.
 */

export function createSymbolSpan(symbol: string): HTMLElement {
    const span = document.createElement('span');
    span.className = 'glyph-symbol';
    // Title spans take flex: 1; the symbol keeps its natural width.
    span.style.flex = 'none';
    span.textContent = symbol;
    return span;
}

/**
 * Turn a symbol span carried across a morph (cursor → placed) into the
 * settled .glyph-symbol form. Same element, new manifestation — the span
 * itself honors the Element Axioma.
 */
export function settleSymbolSpan(span: HTMLElement): HTMLElement {
    span.classList.remove('glyph-cursor-symbol');
    span.classList.add('glyph-symbol');
    span.style.flex = 'none';
    return span;
}
