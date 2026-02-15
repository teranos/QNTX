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
    const bar = document.createElement('div');
    bar.className = 'canvas-breadcrumb-bar';

    // Root crumb
    const root = document.createElement('span');
    root.className = 'breadcrumb-item';
    if (stack.length > 0) {
        root.classList.add('breadcrumb-clickable');
        root.textContent = 'Canvas';
        root.addEventListener('click', () => jumpToBreadcrumb(-1));
    } else {
        root.classList.add('breadcrumb-current');
        root.textContent = 'Canvas';
    }
    bar.appendChild(root);

    // Stack entries
    for (let i = 0; i < stack.length; i++) {
        const sep = document.createElement('span');
        sep.className = 'breadcrumb-separator';
        sep.textContent = ' › ';
        bar.appendChild(sep);

        const crumb = document.createElement('span');
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
        bar.appendChild(crumb);
    }

    return bar;
}

/**
 * Reset breadcrumb state (for testing)
 */
export function _resetBreadcrumbs(): void {
    stack.length = 0;
}
