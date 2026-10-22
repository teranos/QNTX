// "Design System should cease to exist as its own project."
// "The Design Element should take over its role."

// ◈ — what QNTX is drawn with, drawn inside QNTX: the tokens, the components, the element specimens.
// The tokens are tokens.css's own text, bundled with the page that links it; what each resolves to is
// read off :root, as the page has it. Nothing here asks the node.

import type { Element } from '@teranos/elements';
import { createInput, sizeText, TEXT_SIZES, type TextSize } from '@teranos/elements';
import tokensCss from '../css/tokens.css' with { type: 'text' };
import { renderComponentGallery, sectionTitle } from './design-gallery.ts';
import { renderElementSpecimens } from './design-specimens.ts';

export const ELEMENT_ID = 'design-element';

/** One declaration: its name, what tokens.css says, and the comment beside it. */
export interface Token {
    name: string;
    value: string;
    comment?: string;
}

/** The declarations under one comment heading of tokens.css. */
export interface TokenGroup {
    heading: string;
    tokens: Token[];
}

/** tokens.css read into its groups: a comment on a line of its own heads the declarations under it. */
export function parseTokens(css: string): TokenGroup[] {
    const groups: TokenGroup[] = [];
    let current: TokenGroup = { heading: 'Ungrouped', tokens: [] };

    for (const line of css.split('\n')) {
        const trimmed = line.trim();

        if (trimmed.startsWith('/*') && trimmed.endsWith('*/')) {
            const heading = trimmed.substring(2, trimmed.length - 2).trim();
            if (current.tokens.length >= 1) groups.push(current);
            current = { heading, tokens: [] };
            continue;
        }

        const dashAt = trimmed.indexOf('--');
        if (dashAt === -1) continue;
        const colonAt = trimmed.indexOf(':', dashAt);
        if (colonAt === -1) continue;

        const name = trimmed.substring(dashAt, colonAt).trim();
        let rest = trimmed.substring(colonAt + 1).trim();
        let comment: string | undefined;
        const commentAt = rest.indexOf('/*');
        if (commentAt !== -1) {
            const endAt = rest.indexOf('*/', commentAt);
            if (endAt !== -1) {
                comment = rest.substring(commentAt + 2, endAt).trim();
                rest = rest.substring(0, commentAt).trim();
            }
        }
        if (rest.endsWith(';')) rest = rest.substring(0, rest.length - 1).trim();

        current.tokens.push(comment ? { name, value: rest, comment } : { name, value: rest });
    }

    if (current.tokens.length >= 1) groups.push(current);
    return groups;
}

function isColor(value: string): boolean {
    return value.startsWith('#') || value.startsWith('rgb') || value.startsWith('hsl');
}

function isVarRef(value: string): boolean {
    return value.startsWith('var(');
}

/** What the page resolves a token to, on :root. */
function resolved(name: string): string {
    return window.getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function isLightColor(hex: string): boolean {
    if (!hex.startsWith('#')) return false;
    const clean = hex.length === 4 ? hex[1] + hex[1] + hex[2] + hex[2] + hex[3] + hex[3] : hex.substring(1);
    const r = parseInt(clean.substring(0, 2), 16);
    const g = parseInt(clean.substring(2, 4), 16);
    const b = parseInt(clean.substring(4, 6), 16);
    return (r * 299 + g * 587 + b * 114) / 1000 > 200;
}

function span(text: string, color: string, size: TextSize): HTMLSpanElement {
    const node = document.createElement('span');
    node.textContent = text;
    node.style.color = color;
    sizeText(node, size);
    return node;
}

function label(token: Token): HTMLElement {
    const info = document.createElement('div');
    info.className = 'design-token-info';
    info.style.display = 'flex';
    info.style.flexDirection = 'column';
    info.style.padding = '2px 6px';
    info.style.minWidth = '0';
    info.appendChild(span(token.name, 'var(--accent-on-dark)', 'xs'));
    info.appendChild(span(token.value, 'var(--text-on-dark)', 'xs'));
    if (isVarRef(token.value)) {
        const to = resolved(token.name);
        if (to) info.appendChild(span(to, 'var(--text-on-dark-tertiary)', 'xs'));
    }
    if (token.comment) {
        const said = span(token.comment, 'var(--text-on-dark-tertiary)', 'xs');
        said.style.fontStyle = 'italic';
        info.appendChild(said);
    }
    return info;
}

function card(token: Token, demo: HTMLElement | null): HTMLElement {
    const node = document.createElement('div');
    node.className = 'design-token';
    node.dataset.token = token.name;
    node.style.display = 'flex';
    node.style.flexDirection = demo && !demo.classList.contains('design-swatch') ? 'column' : 'row';
    node.style.background = 'var(--bg-secondary)';
    node.style.border = 'var(--panel-border)';
    node.style.borderRadius = 'var(--border-radius)';
    if (demo) node.appendChild(demo);
    node.appendChild(label(token));
    return node;
}

function stage(): HTMLDivElement {
    const node = document.createElement('div');
    node.className = 'design-token-demo';
    node.style.display = 'flex';
    node.style.flexWrap = 'wrap';
    node.style.alignItems = 'center';
    node.style.gap = '4px';
    node.style.padding = '4px 8px';
    node.style.minHeight = '28px';
    return node;
}

function box(width: string, height: string): HTMLDivElement {
    const node = document.createElement('div');
    node.style.width = width;
    node.style.height = height;
    node.style.background = 'var(--bg-tertiary)';
    node.style.border = 'var(--panel-border)';
    node.style.borderRadius = 'var(--border-radius)';
    return node;
}

/** What a token looks like, shown by what it is: a shadow cast, a radius rounded, a colour worn. */
function demoOf(token: Token): HTMLElement | null {
    const name = token.name;

    if (name.includes('shadow')) {
        const demo = stage();
        const shadowed = box('60px', '36px');
        shadowed.style.boxShadow = `var(${name})`;
        demo.appendChild(shadowed);
        return demo;
    }

    if (name.includes('border-radius')) {
        const demo = stage();
        for (const size of ['24px', '40px', '64px']) {
            const rounded = box(size, size);
            rounded.style.background = 'var(--accent-on-dark)';
            rounded.style.borderRadius = `var(${name})`;
            demo.appendChild(rounded);
        }
        return demo;
    }

    if (name.includes('border') && token.value.includes('solid')) {
        const demo = stage();
        const bordered = box('60px', '28px');
        bordered.style.background = 'var(--bg-secondary)';
        bordered.style.border = `var(${name})`;
        demo.appendChild(bordered);
        return demo;
    }

    if (name.includes('padding')) {
        const demo = stage();
        const padded = span('content', 'var(--text-on-dark-tertiary)', 'xs');
        padded.style.background = 'var(--bg-tertiary)';
        padded.style.border = '1px dashed var(--border-on-dark)';
        padded.style.padding = `var(${name})`;
        demo.appendChild(padded);
        return demo;
    }

    if (name.includes('gap')) {
        const demo = stage();
        const bar = document.createElement('div');
        bar.style.height = '12px';
        bar.style.width = `var(${name})`;
        bar.style.background = 'var(--accent-on-dark)';
        bar.style.opacity = '0.5';
        demo.appendChild(bar);
        return demo;
    }

    // Pointed at, the box takes the accent at the token's timing.
    if (name.includes('transition')) {
        const demo = stage();
        const timed = box('36px', '24px');
        timed.style.transition = `var(${name})`;
        timed.addEventListener('pointerenter', () => { timed.style.background = 'var(--accent-on-dark)'; });
        timed.addEventListener('pointerleave', () => { timed.style.background = 'var(--bg-tertiary)'; });
        demo.appendChild(timed);
        return demo;
    }

    // A size token: the sizes are the package's, a token of one is shown at it, and the field's in a field.
    const font = name.startsWith('--font-') ? name.substring('--font-'.length) : '';
    if (font.startsWith('size-')) {
        const size = font.substring('size-'.length);
        const demo = stage();
        if (Object.prototype.hasOwnProperty.call(TEXT_SIZES, size)) {
            demo.appendChild(span('The quick brown fox jumps over the lazy dog', 'var(--text-on-dark)', size as TextSize));
            return demo;
        }
        if (size === 'field') {
            demo.appendChild(createInput({ value: 'The quick brown fox' }));
            return demo;
        }
        return null;
    }

    if (font) {
        const demo = stage();
        const sample = span('ABCDEFGHIJKLM 0123456789 {}[]()<>', 'var(--text-on-dark)', 'md');
        sample.style.fontFamily = `var(${name})`;
        demo.appendChild(sample);
        return demo;
    }

    const to = resolved(name);
    if (!isColor(token.value) && !isColor(to) && !isVarRef(token.value)) return null;
    const swatch = document.createElement('div');
    swatch.className = 'design-swatch';
    swatch.style.width = '36px';
    swatch.style.minHeight = '28px';
    swatch.style.flexShrink = '0';
    swatch.style.background = `var(${name})`;
    if (isLightColor(to)) swatch.style.border = '1px solid var(--border-on-dark)';
    return swatch;
}

/** Every group of tokens.css, each token shown by what it is. */
export function renderTokens(root: HTMLElement, groups: readonly TokenGroup[]): void {
    const section = document.createElement('section');
    section.className = 'element-section design-tokens';
    section.appendChild(sectionTitle('Tokens'));

    const count = groups.reduce((n, g) => n + g.tokens.length, 0);
    const said = span(`${count} tokens across ${groups.length} groups, from tokens.css`, 'var(--text-on-dark-tertiary)', 'xs');
    said.style.display = 'block';
    said.style.padding = '0 8px 8px';
    section.appendChild(said);

    for (const group of groups) {
        const part = document.createElement('div');
        part.className = 'design-token-group';
        part.dataset.group = group.heading;
        part.style.marginBottom = '14px';

        const heading = span(group.heading, 'var(--text-on-dark-secondary)', 'sm');
        heading.style.display = 'block';
        heading.style.padding = '0 0 2px';
        heading.style.marginBottom = '4px';
        heading.style.borderBottom = '1px solid var(--border-on-dark)';
        part.appendChild(heading);

        const grid = document.createElement('div');
        grid.style.display = 'grid';
        grid.style.gridTemplateColumns = 'repeat(auto-fill, minmax(min(100%, 220px), 1fr))';
        grid.style.gap = '3px';
        for (const token of group.tokens) grid.appendChild(card(token, demoOf(token)));
        part.appendChild(grid);

        section.appendChild(part);
    }

    root.appendChild(section);
}

/** The body: the tokens, then the components, then the element specimens. */
export function renderDesign(root: HTMLElement): void {
    renderTokens(root, parseTokens(tokensCss));
    renderComponentGallery(root);
    renderElementSpecimens(root);
}

export function createDesignElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Design',
        symbol: '◈',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'design-element-content';
            content.style.padding = '12px';
            renderDesign(content);
            return content;
        },
    };
}
