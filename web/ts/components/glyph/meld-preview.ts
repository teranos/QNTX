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
    // Calculate target intensity
    const targetIntensity = Math.max(0, 1 - (distance / PROXIMITY_THRESHOLD));

    // Smooth the intensity change to reduce jitter
    const currentIntensity = activePreview.lastIntensity ?? 0;
    const intensity = smoothIntensity(currentIntensity, targetIntensity);
    activePreview.lastIntensity = intensity;

    // Skip animation update if change is too small (reduces jitter)
    if (Math.abs(intensity - currentIntensity) < 0.02 && activePreview.axAnimation) {
        return;
    }

    // How much the border radius should deform (max 32px at touching)
    const morphRadius = Math.round(32 * intensity);

    // How much to "pull" toward each other visually (stronger effect)
    const pullAmount = Math.round(12 * intensity);

    // Add stronger visual effect when "melded" (very close)
    const isMeldReady = distance < MELD_THRESHOLD;
    const glowColor = isMeldReady ? 'rgba(100, 255, 100, 0.6)' : `rgba(100, 200, 255, ${intensity * 0.4})`;
    const glowSize = isMeldReady ? 30 : 20;

    // Ax right edge morphing (reaching toward prompt)
    const axKeyframes = [
        {
            borderRadius: '8px 8px 8px 8px',
            transform: 'translateX(0)',
            boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
            filter: 'brightness(1)'
        },
        {
            borderRadius: `8px ${8 + morphRadius}px ${8 + morphRadius}px 8px`,
            transform: `translateX(${pullAmount}px)`,
            boxShadow: `${pullAmount}px 0 ${glowSize * intensity}px ${glowColor}`,
            filter: isMeldReady ? 'brightness(1.1)' : 'brightness(1)'
        }
    ];

    // Prompt left edge morphing (reaching toward ax)
    const promptKeyframes = [
        {
            borderRadius: '8px 8px 8px 8px',
            transform: 'translateX(0)',
            boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)',
            filter: 'brightness(1)'
        },
        {
            borderRadius: `${8 + morphRadius}px 8px 8px ${8 + morphRadius}px`,
            transform: `translateX(-${pullAmount}px)`,
            boxShadow: `${-pullAmount}px 0 ${glowSize * intensity}px ${glowColor}`,
            filter: isMeldReady ? 'brightness(1.1)' : 'brightness(1)'
        }
    ];

    // Cancel any existing animations
    activePreview.axAnimation?.cancel();
    activePreview.promptAnimation?.cancel();

    // Create new animations with smoother timing
    activePreview.axAnimation = axElement.animate(axKeyframes, {
        duration: 300, // Longer duration for smoother movement
        easing: 'cubic-bezier(0.4, 0.0, 0.2, 1)', // Material Design easing
        fill: 'forwards'
    });

    activePreview.promptAnimation = promptElement.animate(promptKeyframes, {
        duration: 300,
        easing: 'cubic-bezier(0.4, 0.0, 0.2, 1)',
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
                borderRadius: activePreview.axElement.style.borderRadius || '8px 8px 8px 8px',
                transform: activePreview.axElement.style.transform || 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)'
            },
            {
                borderRadius: '8px 8px 8px 8px',
                transform: 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)'
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
                borderRadius: activePreview.promptElement.style.borderRadius || '8px 8px 8px 8px',
                transform: activePreview.promptElement.style.transform || 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)'
            },
            {
                borderRadius: '8px 8px 8px 8px',
                transform: 'translateX(0)',
                boxShadow: '0 2px 12px rgba(0, 0, 0, 0.1)'
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

    // Apply hysteresis logic
    const threshold = activePreview.isMelded ? UNMELD_THRESHOLD : PROXIMITY_THRESHOLD;

    if (closestPrompt && closestDistance < threshold) {
        // Update melded state when very close
        if (closestDistance < MELD_THRESHOLD) {
            activePreview.isMelded = true;
        } else if (closestDistance > UNMELD_THRESHOLD) {
            activePreview.isMelded = false;
        }

        // Show morphing preview
        animateMeldPreview(axElement, closestPrompt, closestDistance);
        activePreview.lastDistance = closestDistance;
    } else {
        // No prompt nearby or beyond threshold, clear preview
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