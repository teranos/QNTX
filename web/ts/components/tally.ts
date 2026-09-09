/**
 * A tally: named things and how many of each, read down rather than across.
 *
 * Across, a tally is one comma-joined string with no natural length inside a
 * panel with a fixed width, and its tail runs off the right edge. Down, every
 * entry has a line and the counts sit in their own column.
 *
 * The caller decides what a name is — text, or something you can press. The
 * tally does not know and must not: it is what makes it usable from both the
 * stand panel, where a page opens, and the page glyph, where it is already open.
 */

const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

export interface TallyItem {
    name: string;
    count: number;
}

/** How a name is drawn. The default is text and nothing else. */
export type NameCell = (name: string) => HTMLElement;

function plainName(name: string): HTMLElement {
    const span = document.createElement('span');
    span.textContent = name;
    return span;
}

export function renderTally(
    container: HTMLElement,
    label: string,
    items: TallyItem[],
    renderName: NameCell = plainName,
): void {
    const heading = document.createElement('div');
    heading.textContent = label;
    heading.style.color = MUTE;
    heading.style.padding = '0 0 4px';
    heading.style.borderBottom = '1px solid ' + LINE;
    heading.style.marginBottom = '6px';
    container.appendChild(heading);

    if (items.length === 0) {
        const none = document.createElement('div');
        none.textContent = 'nothing recorded';
        none.style.color = MUTE;
        container.appendChild(none);
        return;
    }

    for (const item of items) {
        const line = document.createElement('div');
        line.className = 'stand-tally';
        line.style.display = 'flex';
        line.style.alignItems = 'baseline';
        line.style.gap = '10px';
        line.style.padding = '2px 0';

        const name = renderName(item.name);
        name.style.flex = '1';
        name.style.minWidth = '0';
        name.style.overflowWrap = 'break-word';
        name.style.wordBreak = 'break-word';

        const count = document.createElement('span');
        count.textContent = String(item.count);
        count.style.flexShrink = '0';
        count.style.minWidth = '3em';
        count.style.textAlign = 'right';
        count.style.color = MUTE;

        line.appendChild(name);
        line.appendChild(count);
        container.appendChild(line);
    }
}
