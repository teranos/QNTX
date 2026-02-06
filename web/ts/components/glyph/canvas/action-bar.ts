/**
 * Canvas Action Bar
 *
 * Floating toolbar that appears at top center when glyphs are selected.
 * Provides delete and unmeld actions.
 */

import { getMinimizeDuration } from '../glyph';
import { isMeldedComposition } from '../meld-system';

// Action bar animation constants
const ACTION_BAR_ANIMATION_SPEED = 0.5;
const ACTION_BAR_TOP_OFFSET = 8;

// Module state
let actionBar: HTMLElement | null = null;

/**
 * Show the action bar at top middle of canvas with slide-in animation
 */
export function showActionBar(
    selectedGlyphIds: string[],
    container: HTMLElement,
    onDelete: () => void,
    onUnmeld: (composition: HTMLElement) => void
): void {
    if (selectedGlyphIds.length === 0) {
        return;
    }

    hideActionBar();

    // Defensive cleanup: remove any orphaned action bars
    container.querySelectorAll('.canvas-action-bar').forEach(el => el.remove());

    const bar = document.createElement('div');
    bar.className = 'canvas-action-bar';

    // Check if any selected glyphs are in a meld
    let meldedComposition: HTMLElement | null = null;
    for (const glyphId of selectedGlyphIds) {
        const glyphEl = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        if (glyphEl?.parentElement && isMeldedComposition(glyphEl.parentElement)) {
            meldedComposition = glyphEl.parentElement;
            break;
        }
    }

    // Add unmeld button if glyphs are in a meld
    if (meldedComposition) {
        const unmeldBtn = document.createElement('button');
        unmeldBtn.className = 'canvas-action-button canvas-action-unmeld has-tooltip';
        unmeldBtn.dataset.tooltip = 'Break meld';
        unmeldBtn.textContent = '⋈'; // Bowtie/join symbol
        unmeldBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            onUnmeld(meldedComposition!);
        });
        bar.appendChild(unmeldBtn);
    }

    // Add delete button
    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'canvas-action-button canvas-action-delete has-tooltip';
    deleteBtn.dataset.tooltip = `Delete ${selectedGlyphIds.length} glyph${selectedGlyphIds.length > 1 ? 's' : ''}`;
    deleteBtn.textContent = '✕'; // Heavy multiplication X
    deleteBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        onDelete();
    });

    bar.appendChild(deleteBtn);
    container.appendChild(bar);

    positionActionBar(bar);
    actionBar = bar;

    // Slide in from top
    const duration = getMinimizeDuration() * ACTION_BAR_ANIMATION_SPEED;
    if (duration > 0) {
        bar.animate([
            { transform: 'translate(-50%, -100%)', opacity: 0 },
            { transform: 'translateX(-50%)', opacity: 1 }
        ], {
            duration,
            easing: 'ease',
            fill: 'both'
        });
    }
}

/**
 * Position action bar at top middle of the canvas
 */
function positionActionBar(bar: HTMLElement): void {
    bar.style.position = 'absolute';
    bar.style.left = '50%';
    bar.style.top = `${ACTION_BAR_TOP_OFFSET}px`;
    bar.style.zIndex = '9999';
}

/**
 * Hide the action bar with slide-up animation
 */
export function hideActionBar(): void {
    if (!actionBar) return;

    const bar = actionBar;
    actionBar = null;

    // Cancel any running animations
    bar.getAnimations().forEach(anim => anim.cancel());

    const duration = getMinimizeDuration() * 0.5;
    if (duration === 0) {
        bar.remove();
        return;
    }

    // Slide up and fade out
    const animation = bar.animate([
        { transform: 'translateX(-50%)', opacity: 1 },
        { transform: 'translate(-50%, -100%)', opacity: 0 }
    ], {
        duration,
        easing: 'ease',
        fill: 'forwards'
    });

    animation.onfinish = () => {
        bar.remove();
    };
}
