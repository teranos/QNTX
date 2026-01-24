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
 * Begin a morph transaction for minimize
 * Ensures exclusive animation and provides commit/rollback semantics
 */
export function beginMinimizeMorph(
    element: HTMLElement,
    fromRect: DOMRect,
    toPosition: { x: number; y: number }
): Promise<void> {
    // Cancel any existing animation for this element (exclusivity)
    const existing = activeAnimations.get(element);
    if (existing) {
        log.debug(SEG.UI, '[MorphTransaction] Cancelling existing animation');
        existing.cancel();
    }

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

    // Begin the transaction
    const animation = element.animate(keyframes, {
        duration: 200, // Match existing minimize duration
        easing: 'cubic-bezier(0.4, 0, 0.2, 1)',
        fill: 'none' // Don't hold final state - we'll commit it manually
    });

    // Track this as the exclusive animation for this element
    activeAnimations.set(element, animation);

    // Return a promise that represents the transaction
    return new Promise((resolve, reject) => {
        animation.addEventListener('finish', () => {
            // COMMIT: Animation completed successfully
            log.debug(SEG.UI, '[MorphTransaction] Minimize committed');
            activeAnimations.delete(element);
            resolve();
        });

        animation.addEventListener('cancel', () => {
            // ROLLBACK: Animation was cancelled
            log.debug(SEG.UI, '[MorphTransaction] Minimize rolled back');
            activeAnimations.delete(element);
            reject(new Error('Animation cancelled'));
        });
    });
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