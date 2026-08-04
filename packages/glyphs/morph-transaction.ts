/**
 * Morph transaction using Web Animations API
 *
 * Implements the "one law": animation as a state transition of a persistent object,
 * with a begin, an exclusive running period, and a commit or rollback.
 *
 * Used for embodiment transitions where the Glyph must maintain identity.
 */

import { getLogger, getLogSegment, getWindowBorderRadius } from './config';

// Track active animations to ensure exclusivity per element
const activeAnimations = new WeakMap<HTMLElement, Animation>();

// A morph moves the DOM under a held mouse button, which the browser reads as
// a text-selection drag. Counted, because morphs overlap.
let morphsInFlight = 0;
let selectBefore = '';

function suppressSelection(): void {
    if (morphsInFlight++ > 0) return;
    selectBefore = document.body.style.userSelect;
    document.body.style.userSelect = 'none';
    document.getSelection()?.removeAllRanges();
}

function restoreSelection(): void {
    if (--morphsInFlight > 0) return;
    morphsInFlight = 0;
    document.body.style.userSelect = selectBefore;
}

/**
 * Suppress from the press itself. A tray dot morphs on click, which fires on
 * mouseup — by then the drag has already selected whatever moved under it.
 */
export function suppressSelectionUntilRelease(): void {
    suppressSelection();
    const release = () => {
        document.removeEventListener('mouseup', release, true);
        restoreSelection();
    };
    document.addEventListener('mouseup', release, true);
}

/**
 * Core animation transaction helper
 * Handles exclusivity, promise wrapping, and event listener cleanup
 */
function createMorphAnimation(
    element: HTMLElement,
    keyframes: Keyframe[],
    duration: number,
    transactionName: string
): Promise<void> {
    const log = getLogger();
    const seg = getLogSegment();

    // Cancel any existing animation for this element (exclusivity)
    const existing = activeAnimations.get(element);
    if (existing) {
        log.debug(seg, '[MorphTransaction] Cancelling existing animation');
        existing.cancel();
    }

    // Create and configure the animation
    const animation = element.animate(keyframes, {
        duration,
        easing: 'cubic-bezier(0.4, 0, 0.2, 1)',
        fill: 'none' // Don't hold final state - we'll commit it manually
    });

    // Track this as the exclusive animation for this element
    activeAnimations.set(element, animation);
    suppressSelection();

    // Return a promise that represents the transaction
    return new Promise((resolve, reject) => {
        const handleFinish = () => {
            // COMMIT: Animation completed successfully
            log.debug(seg, `[MorphTransaction] ${transactionName} committed`);
            activeAnimations.delete(element);
            restoreSelection();
            // Clean up event listeners to prevent memory leaks
            animation.removeEventListener('finish', handleFinish);
            animation.removeEventListener('cancel', handleCancel);
            resolve();
        };

        const handleCancel = () => {
            // ROLLBACK: Animation was cancelled
            log.debug(seg, `[MorphTransaction] ${transactionName} rolled back`);
            activeAnimations.delete(element);
            restoreSelection();
            // Clean up event listeners to prevent memory leaks
            animation.removeEventListener('finish', handleFinish);
            animation.removeEventListener('cancel', handleCancel);
            reject(new Error('Animation cancelled'));
        };

        animation.addEventListener('finish', handleFinish);
        animation.addEventListener('cancel', handleCancel);
    });
}

/**
 * Begin a morph transaction for minimize
 * Ensures exclusive animation and provides commit/rollback semantics
 */
export function beginMinimizeMorph(
    element: HTMLElement,
    fromRect: DOMRect,
    toPosition: { x: number; y: number },
    duration: number
): Promise<void> {
    const computedStyle = window.getComputedStyle(element);
    const bgColor = computedStyle.backgroundColor;

    const keyframes: Keyframe[] = [
        // From: Window state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: getWindowBorderRadius(),
            backgroundColor: bgColor,
            boxShadow: '0 8px 32px rgba(0, 0, 0, 0.3)',
            opacity: '1'
        },
        // To: Dot state
        {
            left: `${toPosition.x}px`,
            top: `${toPosition.y}px`,
            width: '8px',
            height: '8px',
            borderRadius: '2px',
            backgroundColor: bgColor,
            boxShadow: 'none',
            opacity: '1'
        }
    ];

    return createMorphAnimation(element, keyframes, duration, 'Minimize');
}

/**
 * Begin a morph transaction for maximize (dot to window)
 * Ensures exclusive animation and provides commit/rollback semantics
 */
export function beginMaximizeMorph(
    element: HTMLElement,
    fromRect: DOMRect,
    toPosition: { x: number; y: number; width: number; height: number },
    duration: number
): Promise<void> {
    // Capture current computed styles (may be proximity-expanded)
    const computedStyle = window.getComputedStyle(element);

    const bgColor = computedStyle.backgroundColor;

    const keyframes: Keyframe[] = [
        // From: Dot/proximity-expanded state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: computedStyle.borderRadius,
            backgroundColor: bgColor,
            boxShadow: 'none',
            opacity: computedStyle.opacity
        },
        // To: Window state
        {
            left: `${toPosition.x}px`,
            top: `${toPosition.y}px`,
            width: `${toPosition.width}px`,
            height: `${toPosition.height}px`,
            borderRadius: getWindowBorderRadius(),
            backgroundColor: bgColor,
            boxShadow: '0 8px 32px rgba(0, 0, 0, 0.3)',
            opacity: '1'
        }
    ];

    return createMorphAnimation(element, keyframes, duration, 'Maximize');
}


/**
 * Begin a morph transaction for restore (fullscreen → canvas-placed rect)
 * Unlike minimize (which targets an 8px dot), restore animates to a specific width/height
 */
export function beginRestoreMorph(
    element: HTMLElement,
    fromRect: DOMRect,
    toRect: { x: number; y: number; width: number; height: number },
    duration: number
): Promise<void> {
    const computedStyle = window.getComputedStyle(element);
    const bgColor = computedStyle.backgroundColor;

    const keyframes: Keyframe[] = [
        // From: Fullscreen state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: '0',
            backgroundColor: bgColor,
            boxShadow: 'none',
            opacity: '1'
        },
        // To: Canvas-placed rect
        {
            left: `${toRect.x}px`,
            top: `${toRect.y}px`,
            width: `${toRect.width}px`,
            height: `${toRect.height}px`,
            borderRadius: getWindowBorderRadius(),
            backgroundColor: bgColor,
            boxShadow: '0 4px 12px rgba(0, 0, 0, 0.3)',
            opacity: '1'
        }
    ];

    return createMorphAnimation(element, keyframes, duration, 'Restore');
}

/**
 * Cancel any active morph for an element
 * Used when element is being removed or state is changing unexpectedly
 */
export function cancelMorph(element: HTMLElement): void {
    const animation = activeAnimations.get(element);
    if (animation) {
        animation.cancel();
        activeAnimations.delete(element);
    }
}
