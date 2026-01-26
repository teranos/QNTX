/**
 * Morph transaction using Web Animations API
 *
 * Implements the "one law": animation as a state transition of a persistent object,
 * with a begin, an exclusive running period, and a commit or rollback.
 *
 * Used for embodiment transitions where the Glyph must maintain identity.
 */

import { log, SEG } from '../../logger';

// Track active animations to ensure exclusivity per element
const activeAnimations = new WeakMap<HTMLElement, Animation>();

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
    // Cancel any existing animation for this element (exclusivity)
    const existing = activeAnimations.get(element);
    if (existing) {
        log.debug(SEG.UI, '[MorphTransaction] Cancelling existing animation');
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

    // Return a promise that represents the transaction
    return new Promise((resolve, reject) => {
        const handleFinish = () => {
            // COMMIT: Animation completed successfully
            log.debug(SEG.UI, `[MorphTransaction] ${transactionName} committed`);
            activeAnimations.delete(element);
            // Clean up event listeners to prevent memory leaks
            animation.removeEventListener('finish', handleFinish);
            animation.removeEventListener('cancel', handleCancel);
            resolve();
        };

        const handleCancel = () => {
            // ROLLBACK: Animation was cancelled
            log.debug(SEG.UI, `[MorphTransaction] ${transactionName} rolled back`);
            activeAnimations.delete(element);
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
    // Define the morph keyframes
    const keyframes: Keyframe[] = [
        // From: Window state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: '8px',
            backgroundColor: 'var(--bg-primary)',
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
            backgroundColor: 'var(--bg-gray)',
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

    // Define the morph keyframes
    const keyframes: Keyframe[] = [
        // From: Dot/proximity-expanded state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: computedStyle.borderRadius,
            backgroundColor: computedStyle.backgroundColor,
            boxShadow: 'none',
            opacity: computedStyle.opacity
        },
        // To: Window state
        {
            left: `${toPosition.x}px`,
            top: `${toPosition.y}px`,
            width: `${toPosition.width}px`,
            height: `${toPosition.height}px`,
            borderRadius: '8px',
            backgroundColor: 'var(--bg-primary)',
            boxShadow: '0 8px 32px rgba(0, 0, 0, 0.3)',
            opacity: '1'
        }
    ];

    return createMorphAnimation(element, keyframes, duration, 'Maximize');
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