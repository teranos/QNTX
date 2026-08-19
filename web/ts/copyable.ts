/**
 * An error you cannot copy is an error you cannot report. Click anything
 * carrying data-copyable and its text goes to the clipboard.
 */

const COPYABLE = 'data-copyable';
const RESTORE_MS = 1000;

/** Mark an element so a click copies its text. */
export function copyable(element: HTMLElement): HTMLElement {
    element.setAttribute(COPYABLE, '');
    element.style.userSelect = 'text';
    element.style.cursor = 'pointer';
    // A drag that starts on a character never selects it.
    element.addEventListener('mousedown', (e) => { e.stopPropagation(); });
    return element;
}

function nearestCopyable(target: EventTarget | null): HTMLElement | null {
    if (!(target instanceof HTMLElement)) {
        return null;
    }
    return target.closest(`[${COPYABLE}]`);
}

/** One listener for the whole document; a surface opts in by attribute. */
export function installCopyable(root: Document = document): void {
    root.addEventListener('click', (event) => {
        const element = nearestCopyable(event.target);
        if (!element) {
            return;
        }
        const text = element.textContent;
        if (!text || text === 'copied') {
            return;
        }
        navigator.clipboard.writeText(text);
        const shown = text;
        element.textContent = 'copied';
        setTimeout(() => { element.textContent = shown; }, RESTORE_MS);
    });
}
