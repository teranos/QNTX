// "TABLES BEING MAX 4 col": what a fifth column said is said under a cell
// that keeps its place.
import { createTable, tableCell } from '@teranos/elements';

/** A cell of two lines: what it says, and under it what goes with it, with
 *  that line's own hover. */
export function stacked(text: string, under: string, underTitle = ''): HTMLTableCellElement {
    const td = tableCell(text);
    const line = document.createElement('div');
    line.className = 'element-under';
    line.textContent = under;
    if (underTitle) line.title = underTitle;
    td.appendChild(line);
    return td;
}

// "Leve l Door Emai l Acco unts ?????????? i wish very deeply that this was made structurally impossible"
/** A record: a label beside its value. A label never breaks inside a word; a value wraps. */
export function record(name: string, heads: readonly [string, string], rows: readonly (readonly [string, string])[]): HTMLTableElement {
    return createTable(name, heads, rows.map(([label, value]) => {
        const said = tableCell(label);
        said.style.whiteSpace = 'nowrap';
        said.style.wordBreak = 'normal';
        said.style.overflowWrap = 'normal';
        // A label reads as the head does.
        said.style.color = 'var(--text-on-dark-tertiary)';
        said.style.paddingRight = '12px';
        said.style.verticalAlign = 'top';
        const td = tableCell(value);
        td.style.overflowWrap = 'break-word';
        return { cells: [said, td] as const };
    }));
}
