/**
 * Whether a glyph's body ever arrived.
 *
 * A morph commits geometry and mounts content afterwards, so there is a state
 * the Morph Axioma does not admit: committed, and incomplete. Chrome on the
 * screen, nothing under it, nothing thrown, nothing logged — a window that is
 * a title bar and 16px of padding, which no test could fail and no error could
 * name because no error existed.
 *
 * This watches the one place that gap opens: between mounting a body and the
 * body drawing. A body that draws is `present` and the watch ends. A body still
 * showing nothing when its deadline passes is `refused`, and it says so where it
 * happened, in the box where the content should have been — logging alone is
 * hiding (web/ts/market-glyph.ts).
 *
 * Scope: a window or panel opened from a tray dot mounts through
 * manifestations/render-content.ts, and a glyph lifted off the canvas mounts
 * through manifestations/canvas-window.ts. Both arm the watch, so both are
 * covered — they are two constructors for the one `window` row, and saying
 * "every window" without saying which constructor is how they drifted in the
 * first place. Not covered: `workspace` (manifestations/canvas.ts renders
 * straight into the viewport), `canvasPlaced`, and host glyphs that build their
 * own content area. They call `declareContent` or they are not covered.
 */

import { getLogger, getLogSegment } from './config';
import { CONTENT_DEADLINE_MS } from './glyph';
import { setContentState, getContentState } from './dataset';
import { isSettled, type ContentState } from './content-state';

/**
 * Who the body belongs to. A `Glyph` satisfies it, and so does a path that has
 * only the element and a title — canvas-window.ts lifts a glyph it was never
 * handed the data for.
 */
export interface ContentSubject {
    id: string;
    title: string;
}

interface Watch {
    observer: MutationObserver | null;
    timer: ReturnType<typeof setTimeout> | null;
}

const watches = new WeakMap<HTMLElement, Watch>();

/**
 * Things that draw without words. A body holding one of these is showing
 * something even though it has no text to read.
 */
const DRAWS_WITHOUT_WORDS = 'img, svg, canvas, video, input, textarea, select, iframe';

/**
 * Whether a body is showing anything at all.
 *
 * Text first, because that is what a body usually is and because it is the one
 * answer that does not need layout — the check has to give the same verdict in
 * a test environment, where every box measures zero.
 *
 * "Loading tokens…" counts. The question is not whether the data arrived, it is
 * whether the glyph is showing the user anything while it waits.
 */
export function showsSomething(contentArea: HTMLElement): boolean {
    const text = contentArea.textContent;
    if (text !== null && text.trim() !== '') return true;
    if (contentArea.querySelector(DRAWS_WITHOUT_WORDS) !== null) return true;

    for (const child of Array.from(contentArea.children)) {
        const box = child as HTMLElement;
        if (typeof box.getBoundingClientRect !== 'function') continue;
        if (box.getBoundingClientRect().height > 0) return true;
    }
    return false;
}

/**
 * Watch a body until it draws, or until the deadline says it never will.
 *
 * Settles immediately when the glyph rendered synchronously, so a glyph that
 * shows a placeholder costs nothing. Re-arming replaces any watch the element
 * already had: a glyph reopened is one element, and one element is one watch
 * (AXIOMAS.md, Element Axioma).
 */
export function watchContent(
    element: HTMLElement,
    contentArea: HTMLElement,
    glyph: ContentSubject,
    logLabel: string,
    deadlineMs: number = CONTENT_DEADLINE_MS,
): void {
    disarmContentWatch(element);

    if (showsSomething(contentArea)) {
        setContentState(element, 'present');
        return;
    }

    setContentState(element, 'pending');

    const settle = (state: ContentState): void => {
        disarmContentWatch(element);
        setContentState(element, state);
    };

    const watch: Watch = { observer: null, timer: null };
    watches.set(element, watch);

    if (typeof MutationObserver === 'function') {
        watch.observer = new MutationObserver(() => {
            // The glyph may have declared its own state while we waited.
            const said = getContentState(element);
            if (said !== null && isSettled(said)) {
                disarmContentWatch(element);
                return;
            }
            if (showsSomething(contentArea)) settle('present');
        });
        watch.observer.observe(contentArea, {
            childList: true,
            subtree: true,
            characterData: true,
        });
    }

    watch.timer = setTimeout(() => {
        const said = getContentState(element);
        if (said !== null && isSettled(said)) {
            disarmContentWatch(element);
            return;
        }
        if (showsSomething(contentArea)) {
            settle('present');
            return;
        }
        settle('refused');
        refuse(contentArea, glyph, logLabel, deadlineMs);
    }, deadlineMs);
}

/**
 * End a watch. Called when a glyph settles, and when its content is stashed —
 * a body off the DOM is not a body that failed to draw (manifestations/stash.ts).
 */
export function disarmContentWatch(element: HTMLElement): void {
    const watch = watches.get(element);
    if (!watch) return;
    watch.observer?.disconnect();
    if (watch.timer !== null) clearTimeout(watch.timer);
    watches.delete(element);
}

/** Whether an element is still being watched. Exported for tests and the doctor. */
export function isWatched(element: HTMLElement): boolean {
    return watches.has(element);
}

/**
 * A glyph's own word on what it is showing, from anywhere inside its body.
 *
 * `empty` is the one state the runtime cannot infer: "No access tokens." draws,
 * so it reads as `present`, and only the glyph knows the difference between
 * showing data and showing that there is none.
 */
export function declareContent(node: Node, state: ContentState): void {
    const element = owningGlyph(node);
    if (!element) return;
    if (isSettled(state)) disarmContentWatch(element);
    setContentState(element, state);
}

/** The glyph element a node sits inside, or null if it sits in no glyph. */
function owningGlyph(node: Node): HTMLElement | null {
    const start = node.nodeType === 1 ? (node as HTMLElement) : node.parentElement;
    return start?.closest('[data-glyph-id]') as HTMLElement | null ?? null;
}

/**
 * Say, in the body, that nothing came.
 *
 * Every variable in scope goes on the line and into the box: which glyph, which
 * manifestation mounted it, how long it waited. A refusal a reader cannot act on
 * is the silence this file exists to end.
 */
function refuse(contentArea: HTMLElement, glyph: ContentSubject, logLabel: string, waitedMs: number): void {
    const log = getLogger();
    const seg = getLogSegment();

    // Warn, not Error: the node handled it and would rather it stopped
    // happening (docs/sentry.md, "Level means what it says").
    log.warn(seg, `[${logLabel}] ${glyph.id} drew nothing in ${waitedMs}ms`, {
        glyph: glyph.id,
        title: glyph.title,
        manifestation: logLabel,
        waitedMs,
    });

    const box = document.createElement('div');
    box.className = 'glyph-refusal';
    box.style.color = 'var(--color-warning)';
    box.style.fontFamily = 'var(--font-mono)';
    box.style.padding = '8px';
    box.style.wordBreak = 'break-word';
    box.style.overflowWrap = 'break-word';

    const what = document.createElement('div');
    what.style.marginBottom = '8px';
    what.style.fontWeight = 'bold';
    what.textContent = 'nothing was drawn, and nothing said why';
    box.appendChild(what);

    const which = document.createElement('div');
    which.style.opacity = '0.8';
    which.style.fontSize = '12px';
    which.textContent = `${glyph.id} · ${logLabel} · still empty after ${waitedMs}ms`;
    box.appendChild(which);

    contentArea.appendChild(box);
}
