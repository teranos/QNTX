/**
 * The glyph's background answers for its last response: error red, warning
 * yellow, or its own identity color when the answer stood.
 *
 * Identity color is an inline style (packages/glyphs run.ts), so the tint is
 * inline too — the identity is saved on first tint and restored on clear.
 * The data-response-state attribute is the queryable record of the tint.
 */

export type ResponseState = 'error' | 'warning';

export function setResponseState(element: HTMLElement, state: ResponseState | null): void {
    const titleBar = element.querySelector('.glyph-title-bar') as HTMLElement | null;

    if (state === null) {
        if (element.dataset.responseState === undefined) return;
        delete element.dataset.responseState;
        element.style.backgroundColor = element.dataset.identityBg ?? '';
        element.style.outlineColor = element.dataset.identityOutline ?? '';
        delete element.dataset.identityBg;
        delete element.dataset.identityOutline;
        if (titleBar) {
            titleBar.style.backgroundColor = titleBar.dataset.identityBg ?? '';
            delete titleBar.dataset.identityBg;
        }
        return;
    }

    if (element.dataset.responseState === undefined) {
        element.dataset.identityBg = element.style.backgroundColor;
        element.dataset.identityOutline = element.style.outlineColor;
        if (titleBar) titleBar.dataset.identityBg = titleBar.style.backgroundColor;
    }
    element.dataset.responseState = state;
    element.style.backgroundColor = `var(--glyph-status-${state}-bg)`;
    element.style.outlineColor = `var(--glyph-status-${state}-text)`;
    if (titleBar) titleBar.style.backgroundColor = `var(--glyph-status-${state}-section-bg)`;
}
