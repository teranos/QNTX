/**
 * Breadcrumb — nesting trail for expanded subcanvases
 *
 * Tracks the stack of expanded subcanvas levels and renders a clickable
 * breadcrumb bar. Clicking an ancestor crumb cascades instant-minimize
 * for all levels above the target.
 */

export interface BreadcrumbEntry {
    canvasId: string;
    name: string;
    minimize: (instant: boolean) => void;
}

const stack: BreadcrumbEntry[] = [];

export function pushBreadcrumb(entry: BreadcrumbEntry): void {
    stack.push(entry);
}

export function popBreadcrumb(): void {
    stack.pop();
}

export function getBreadcrumbStack(): ReadonlyArray<BreadcrumbEntry> {
    return stack;
}

/**
 * Jump to a specific breadcrumb level by collapsing everything above it.
 * targetIndex = -1 means "go to root" (minimize all).
 * Splices the stack first, then calls minimize(true) on each removed entry
 * from innermost to outermost.
 */
export function jumpToBreadcrumb(targetIndex: number): void {
    const removed = stack.splice(targetIndex + 1);
    for (let i = removed.length - 1; i >= 0; i--) {
        removed[i].minimize(true);
    }
}

/**
 * Build the breadcrumb bar DOM element.
 * "Canvas › Sub A › Sub B" — all except the last are clickable.
 */
export function buildBreadcrumbBar(): HTMLElement {
    const nav = document.createElement('nav');
    nav.className = 'canvas-breadcrumb-bar';
    nav.setAttribute('aria-label', 'Canvas nesting');

    const list = document.createElement('ol');
    list.className = 'breadcrumb-list';

    // Root crumb
    const rootItem = document.createElement('li');
    rootItem.className = 'breadcrumb-item';
    if (stack.length > 0) {
        rootItem.classList.add('breadcrumb-clickable');
        rootItem.textContent = 'Canvas';
        rootItem.addEventListener('click', () => jumpToBreadcrumb(-1));
    } else {
        rootItem.classList.add('breadcrumb-current');
        rootItem.textContent = 'Canvas';
    }
    list.appendChild(rootItem);

    // Stack entries
    for (let i = 0; i < stack.length; i++) {
        const crumb = document.createElement('li');
        crumb.className = 'breadcrumb-item';

        const isLast = i === stack.length - 1;
        if (isLast) {
            crumb.classList.add('breadcrumb-current');
        } else {
            crumb.classList.add('breadcrumb-clickable');
            const idx = i;
            crumb.addEventListener('click', () => jumpToBreadcrumb(idx));
        }

        crumb.textContent = stack[i].name || 'subcanvas';
        list.appendChild(crumb);
    }

    nav.appendChild(list);
    return nav;
}

/**
 * Reset breadcrumb state (for testing)
 */
export function _resetBreadcrumbs(): void {
    stack.length = 0;
}
