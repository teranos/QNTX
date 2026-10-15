// "TABLES BEING MAX 4 col": what a fifth column said is said under a cell
// that keeps its place.
import { tableCell } from '@teranos/elements';

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
