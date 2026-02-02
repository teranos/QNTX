/**
 * Meld Preview - Visual feedback for ax → prompt melding
 *
 * When dragging an ax glyph, if its right edge gets close to a prompt glyph's
 * left edge, both edges morph to show how they would fuse together.
 */

import { log, SEG } from '../../logger';
import { AX, SO } from '@generated/sym.js';

// Configuration
const PROXIMITY_THRESHOLD = 150; // px - distance at which morphing starts
const MELD_THRESHOLD = 40; // px - distance at which they would actually meld
const UNMELD_THRESHOLD = 80; // px - distance needed to break a meld (hysteresis)

// Track active preview animations and state
let activePreview: {
    axAnimation?: Animation;
    promptAnimation?: Animation;
    axElement?: HTMLElement;
    promptElement?: HTMLElement;
    isMelded?: boolean;
    lastDistance?: number;
    lastIntensity?: number;
} = {};

/**
 * Check if ax's right edge is near prompt's left edge
 */
function getAxToPromptProximity(axElement: HTMLElement, promptElement: HTMLElement): number | null {
    const axRect = axElement.getBoundingClientRect();
    const promptRect = promptElement.getBoundingClientRect();

    // Check vertical alignment (they should be roughly on the same horizontal plane)
    const verticalOverlap = Math.min(axRect.bottom, promptRect.bottom) - Math.max(axRect.top, promptRect.top);
    const minHeight = Math.min(axRect.height, promptRect.height);

    if (verticalOverlap < minHeight * 0.3) {
        // Not aligned enough vertically
        return null;
    }

    // Check if ax is to the left of prompt (correct orientation for melding)
    if (axRect.right > promptRect.left) {
        // Wrong orientation or overlapping
        return null;
    }

    // Calculate distance between ax's right edge and prompt's left edge
    return promptRect.left - axRect.right;
}

/**
 * Smooth intensity changes to reduce jitter
 */
function smoothIntensity(current: number, target: number, factor: number = 0.3): number {
    return current + (target - current) * factor;
}

/**
 * Create morphing animation for meld preview
 */
function animateMeldPreview(axElement: HTMLElement, promptElement: HTMLElement, distance: number): void {
    // If we're in melded state, keep full intensity until unmeld threshold
    let targetIntensity: number;
    if (activePreview.isMelded) {
        // Stay at full intensity while melded
        targetIntensity = 1.0;
    } else {
        // Normal distance-based intensity
        targetIntensity = Math.max(0, 1 - (distance / PROXIMITY_THRESHOLD));
    }

    // Smooth the intensity change to reduce jitter (unless we're popping out of meld)
    const currentIntensity = activePreview.lastIntensity ?? 0;
    const wasJustUnmelded = currentIntensity > 0.8 && !activePreview.isMelded && distance > UNMELD_THRESHOLD;

    // If we just unmelded (popped out), snap back instead of smooth transition
    const intensity = wasJustUnmelded
        ? targetIntensity
        : smoothIntensity(currentIntensity, targetIntensity);

    activePreview.lastIntensity = intensity;

    // Skip animation update if change is too small (reduces jitter)
    if (Math.abs(intensity - currentIntensity) < 0.02 && activePreview.axAnimation && !wasJustUnmelded) {
        return;
    }

    // How much to "pull" toward each other visually (stronger effect)
    const pullAmount = Math.round(15 * intensity);

    // Different visual states with heat-based colors
    const isMelded = activePreview.isMelded;
    const glowColor = isMelded
        ? 'rgba(255, 100, 50, 0.7)'  // Reddish orange when melded/locked
        : distance < MELD_THRESHOLD
        ? 'rgba(255, 150, 50, 0.5)'  // Orange when ready to meld
        : `rgba(255, 240, 150, ${intensity * 0.15})`;  // Very faint yellow when distant
    const glowSize = isMelded ? 16 : 10;

    // Ax moving toward prompt (glow on top, bottom, left - NOT right where it melds)
    const axKeyframes = [
        {
            transform: 'translateX(0)',
            boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
            filter: 'brightness(1)'
        },
        {
            transform: `translateX(${pullAmount}px)`,
            // Only glow on left side - absolutely no glow on right (melding) side
            boxShadow: `${-glowSize * intensity * 1.2}px 0 ${glowSize * intensity}px ${glowColor}`,
            filter: isMelded ? 'brightness(1.15)' : 'brightness(1.05)'
        }
    ];

    // Prompt moving toward ax (glow on top, bottom, right - NOT left where it melds)
    const promptKeyframes = [
        {
            transform: 'translateX(0)',
            boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
            filter: 'brightness(1)'
        },
        {
            transform: `translateX(-${pullAmount}px)`,
            // Only glow on right side - absolutely no glow on left (melding) side
            boxShadow: `${glowSize * intensity * 1.2}px 0 ${glowSize * intensity}px ${glowColor}`,
            filter: isMelded ? 'brightness(1.15)' : 'brightness(1.05)'
        }
    ];

    // Cancel any existing animations
    activePreview.axAnimation?.cancel();
    activePreview.promptAnimation?.cancel();

    // Use faster animation when "popping" out of meld
    const animDuration = wasJustUnmelded ? 150 : 300;
    const animEasing = wasJustUnmelded
        ? 'cubic-bezier(0.0, 0.0, 0.2, 1)'  // Faster out for pop effect
        : 'cubic-bezier(0.4, 0.0, 0.2, 1)';  // Normal smooth easing

    // Create new animations with appropriate timing
    activePreview.axAnimation = axElement.animate(axKeyframes, {
        duration: animDuration,
        easing: animEasing,
        fill: 'forwards'
    });

    activePreview.promptAnimation = promptElement.animate(promptKeyframes, {
        duration: animDuration,
        easing: animEasing,
        fill: 'forwards'
    });

    // Store references
    activePreview.axElement = axElement;
    activePreview.promptElement = promptElement;

    // Only log significant changes to reduce console spam
    if (!activePreview.lastDistance || Math.abs(distance - activePreview.lastDistance) > 5) {
        log.debug(SEG.UI, `[MeldPreview] AX→Prompt distance: ${Math.round(distance)}px, intensity: ${intensity.toFixed(2)}`);
    }
}

/**
 * Clear the meld preview animations
 */
function clearMeldPreview(): void {
    // Reset ax element
    if (activePreview.axElement) {
        activePreview.axAnimation?.cancel();
        activePreview.axElement.animate([
            {
                transform: activePreview.axElement.style.transform || 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
                filter: 'brightness(1)'
            },
            {
                transform: 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
                filter: 'brightness(1)'
            }
        ], {
            duration: 200,
            easing: 'ease-out',
            fill: 'forwards'
        });
    }

    // Reset prompt element
    if (activePreview.promptElement) {
        activePreview.promptAnimation?.cancel();
        activePreview.promptElement.animate([
            {
                transform: activePreview.promptElement.style.transform || 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
                filter: 'brightness(1)'
            },
            {
                transform: 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
                filter: 'brightness(1)'
            }
        ], {
            duration: 200,
            easing: 'ease-out',
            fill: 'forwards'
        });
    }

    // Clear all state
    activePreview = {
        isMelded: false,
        lastDistance: undefined,
        lastIntensity: 0
    };
}

/**
 * Update meld preview during drag with hysteresis
 * Call this from the mousemove handler when dragging an ax glyph
 */
export function updateAxMeldPreview(axElement: HTMLElement): void {
    // Find all prompt glyphs on canvas
    const promptGlyphs = document.querySelectorAll(`[data-symbol="${SO}"]`);

    let closestPrompt: HTMLElement | null = null;
    let closestDistance = Infinity;

    // Find the closest prompt glyph
    promptGlyphs.forEach(promptElement => {
        const distance = getAxToPromptProximity(axElement, promptElement as HTMLElement);

        if (distance !== null && distance < closestDistance) {
            closestDistance = distance;
            closestPrompt = promptElement as HTMLElement;
        }
    });

    // Track previous melded state for pop detection
    const wasMelded = activePreview.isMelded || false;

    // Update melded state with hysteresis
    if (closestDistance !== Infinity) {
        if (!activePreview.isMelded && closestDistance < MELD_THRESHOLD) {
            // Enter melded state
            activePreview.isMelded = true;
        } else if (activePreview.isMelded && closestDistance > UNMELD_THRESHOLD) {
            // Exit melded state (will trigger pop)
            activePreview.isMelded = false;
        }
    }

    // Determine if we should show preview
    const shouldShowPreview = closestPrompt && (
        activePreview.isMelded || // Always show when melded
        closestDistance < PROXIMITY_THRESHOLD // Show when approaching
    );

    if (shouldShowPreview && closestPrompt) {
        // Show morphing preview
        animateMeldPreview(axElement, closestPrompt, closestDistance);
        activePreview.lastDistance = closestDistance;
    } else {
        // No preview needed
        activePreview.isMelded = false;
        clearMeldPreview();
    }
}

/**
 * Check if ax and prompt are close enough to meld
 * Call this on mouseup to decide whether to actually meld
 */
export function shouldMeldAxToPrompt(axElement: HTMLElement): { prompt: HTMLElement; distance: number } | null {
    const promptGlyphs = document.querySelectorAll(`[data-symbol="${SO}"]`);

    for (const promptElement of promptGlyphs) {
        const distance = getAxToPromptProximity(axElement, promptElement as HTMLElement);

        if (distance !== null && distance < MELD_THRESHOLD) {
            return {
                prompt: promptElement as HTMLElement,
                distance
            };
        }
    }

    return null;
}

/**
 * Clean up on drag end
 */
export function cleanupMeldPreview(): void {
    clearMeldPreview();
}