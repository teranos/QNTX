/**
 * Web Animations API for glyph morphing
 *
 * Replaces CSS transitions with Web Animations API for better control
 * and smoother morphing between glyph states.
 *
 * Maintains the single DOM element axiom - animations modify the same element.
 */

import { log, SEG } from '../../logger';

// Animation durations matching existing CSS transitions
const MAXIMIZE_DURATION = 250;
const MINIMIZE_DURATION = 200;
const PROXIMITY_DURATION = 150;

// Easing function matching CSS cubic-bezier
const MORPH_EASING = 'cubic-bezier(0.4, 0, 0.2, 1)';

/**
 * Animate glyph to window state using Web Animations API
 */
export function animateToWindow(
    element: HTMLElement,
    fromRect: DOMRect,
    toPosition: { x: number; y: number; width: string; height: string }
): Animation {
    // Define keyframes for the morphing animation
    const keyframes: Keyframe[] = [
        // From: Current dot/glyph state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: '2px',
            backgroundColor: 'rgb(153, 153, 153)', // --bg-gray
            boxShadow: 'none',
            border: '1px solid rgba(255,255,255,0.1)',
            opacity: '1',
            transform: 'scale(1)'
        },
        // To: Window state
        {
            left: `${toPosition.x}px`,
            top: `${toPosition.y}px`,
            width: toPosition.width,
            height: toPosition.height,
            borderRadius: '8px',
            backgroundColor: 'var(--bg-black)',
            boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
            border: '1px solid var(--border)',
            opacity: '1',
            transform: 'scale(1)'
        }
    ];

    // Create and return the animation
    const animation = element.animate(keyframes, {
        duration: MAXIMIZE_DURATION,
        easing: MORPH_EASING,
        fill: 'forwards' // Keep final state after animation
    });

    log.debug(SEG.UI, `[Animation] Started window morph, duration: ${MAXIMIZE_DURATION}ms`);

    return animation;
}

/**
 * Animate window to glyph/dot state using Web Animations API
 */
export function animateToGlyph(
    element: HTMLElement,
    fromRect: DOMRect,
    toPosition: { x: number; y: number }
): Animation {
    // Define keyframes for the minimize animation
    const keyframes: Keyframe[] = [
        // From: Window state
        {
            left: `${fromRect.left}px`,
            top: `${fromRect.top}px`,
            width: `${fromRect.width}px`,
            height: `${fromRect.height}px`,
            borderRadius: '8px',
            backgroundColor: 'var(--bg-primary)',
            boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
            border: 'none',
            opacity: '1',
            transform: 'scale(1)'
        },
        // To: Dot state
        {
            left: `${toPosition.x}px`,
            top: `${toPosition.y}px`,
            width: '8px',
            height: '8px',
            borderRadius: '2px',
            backgroundColor: 'rgb(153, 153, 153)', // --bg-gray
            boxShadow: 'none',
            border: '1px solid rgba(255,255,255,0.1)',
            opacity: '1',
            transform: 'scale(1)'
        }
    ];

    // Create and return the animation
    const animation = element.animate(keyframes, {
        duration: MINIMIZE_DURATION,
        easing: MORPH_EASING,
        fill: 'forwards'
    });

    log.debug(SEG.UI, `[Animation] Started minimize morph, duration: ${MINIMIZE_DURATION}ms`);

    return animation;
}

/**
 * Animate proximity-based morphing (dot to expanded text)
 * This replaces the CSS-based proximity morphing
 */
export function animateProximity(
    element: HTMLElement,
    proximity: number // 0.0 = dot, 1.0 = fully expanded
): Animation | null {
    // Calculate interpolated values based on proximity
    const width = 8 + (220 - 8) * proximity;
    const height = 8 + (32 - 8) * proximity;
    const borderRadius = 2 * (1 - proximity);

    // Interpolate colors
    const startR = 153, startG = 153, startB = 153;
    const endR = 26, endG = 26, endB = 26;
    const r = Math.round(startR + (endR - startR) * proximity);
    const g = Math.round(startG + (endG - startG) * proximity);
    const b = Math.round(startB + (endB - startB) * proximity);

    // Check if element is hovered for brightness adjustment
    const isHovered = element.matches(':hover');
    const finalR = isHovered ? Math.min(255, Math.round(r + (255 - r) * 0.1)) : r;
    const finalG = isHovered ? Math.min(255, Math.round(g + (255 - g) * 0.1)) : g;
    const finalB = isHovered ? Math.min(255, Math.round(b + (255 - b) * 0.1)) : b;

    // Get current computed styles to animate from
    const computedStyle = getComputedStyle(element);
    const currentWidth = parseFloat(computedStyle.width);
    const currentHeight = parseFloat(computedStyle.height);

    // Skip if change is too small (prevents jitter)
    if (Math.abs(currentWidth - width) < 0.5 && Math.abs(currentHeight - height) < 0.5) {
        return null;
    }

    const keyframes: Keyframe[] = [
        // From: Current state
        {
            width: `${currentWidth}px`,
            height: `${currentHeight}px`,
            borderRadius: computedStyle.borderRadius,
            backgroundColor: computedStyle.backgroundColor
        },
        // To: Target proximity state
        {
            width: `${width}px`,
            height: `${height}px`,
            borderRadius: `${borderRadius}px`,
            backgroundColor: `rgb(${finalR}, ${finalG}, ${finalB})`
        }
    ];

    // Create very fast animation for smooth morphing
    const animation = element.animate(keyframes, {
        duration: PROXIMITY_DURATION,
        easing: 'ease-out',
        fill: 'forwards'
    });

    return animation;
}

/**
 * Cancel any running animation on element
 */
export function cancelAnimation(element: HTMLElement): void {
    const animations = element.getAnimations();
    animations.forEach(animation => {
        if (animation.playState === 'running') {
            animation.cancel();
            log.debug(SEG.UI, '[Animation] Cancelled running animation');
        }
    });
}

/**
 * Check if element has running animations
 */
export function isAnimating(element: HTMLElement): boolean {
    const animations = element.getAnimations();
    return animations.some(animation => animation.playState === 'running');
}

/**
 * Wait for animation to complete with timeout fallback
 */
export async function waitForAnimation(
    animation: Animation,
    timeoutMs: number = 500
): Promise<void> {
    return new Promise((resolve) => {
        let timeoutId: ReturnType<typeof setTimeout> | null = null;

        const cleanup = () => {
            if (timeoutId) {
                clearTimeout(timeoutId);
            }
            resolve();
        };

        // Set up timeout fallback
        timeoutId = setTimeout(() => {
            log.warn(SEG.UI, '[Animation] Animation timeout, continuing anyway');
            cleanup();
        }, timeoutMs);

        // Wait for animation to finish
        animation.addEventListener('finish', () => {
            log.debug(SEG.UI, '[Animation] Animation finished successfully');
            cleanup();
        }, { once: true });

        animation.addEventListener('cancel', () => {
            log.debug(SEG.UI, '[Animation] Animation cancelled');
            cleanup();
        }, { once: true });
    });
}

/**
 * Get animation durations for external use
 */
export function getMaximizeDuration(): number {
    return MAXIMIZE_DURATION;
}

export function getMinimizeDuration(): number {
    return MINIMIZE_DURATION;
}

export function getProximityDuration(): number {
    return PROXIMITY_DURATION;
}