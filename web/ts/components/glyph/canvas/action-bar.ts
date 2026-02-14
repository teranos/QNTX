/**
 * Canvas Action Bar
 *
 * Floating toolbar that appears at top center when glyphs are selected.
 * Provides delete and unmeld actions.
 *
 * Container-scoped: each workspace manages its own action bar via DOM queries.
 * No global singleton — supports multiple simultaneous workspaces.
 */

import { getMinimizeDuration } from '../glyph';

import { Prose } from '@generated/sym.js';

// Action bar animation constants
const ACTION_BAR_ANIMATION_SPEED = 0.5;
const ACTION_BAR_TOP_OFFSET = 8;

/**
 * Show the action bar at top middle of canvas with slide-in animation
 */
export function showActionBar(
    selectedGlyphIds: string[],
    container: HTMLElement,
    onDelete: () => void,
    onUnmeld: (composition: HTMLElement) => void,
    onConvertToPrompt?: () => void,
    onConvertToNote?: () => void,
): void {
    if (selectedGlyphIds.length === 0) {
        return;
    }

    // Defensive cleanup: remove any existing action bars in this container
    container.querySelectorAll('.canvas-action-bar').forEach(el => el.remove());

    const bar = document.createElement('div');
    bar.className = 'canvas-action-bar';

    // Check if any selected glyphs are in a meld
    // Uses .closest() to handle glyphs inside sub-containers within compositions
    let meldedComposition: HTMLElement | null = null;
    for (const glyphId of selectedGlyphIds) {
        const glyphEl = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        const comp = glyphEl?.closest('.melded-composition') as HTMLElement | null;
        if (comp) {
            meldedComposition = comp;
            break;
        }
    }

    // Check single-glyph selection type for conversion buttons
    let selectedSymbol: string | undefined;
    if (selectedGlyphIds.length === 1) {
        const glyphEl = container.querySelector(`[data-glyph-id="${selectedGlyphIds[0]}"]`) as HTMLElement | null;
        selectedSymbol = glyphEl?.dataset.glyphSymbol;
    }

    // Add convert-to-prompt button if single note selected
    if (selectedSymbol === Prose && onConvertToPrompt) {
        const convertBtn = document.createElement('button');
        convertBtn.className = 'canvas-action-button canvas-action-convert has-tooltip';
        convertBtn.dataset.tooltip = 'Convert to prompt glyph';
        convertBtn.textContent = '⟶';
        convertBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            onConvertToPrompt();
        });
        bar.appendChild(convertBtn);
    }

    // Add convert-to-note button if single result selected
    if (selectedSymbol === 'result' && onConvertToNote) {
        const convertBtn = document.createElement('button');
        convertBtn.className = 'canvas-action-button canvas-action-convert has-tooltip';
        convertBtn.dataset.tooltip = 'Convert to note';
        convertBtn.textContent = '✎';
        convertBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            onConvertToNote();
        });
        bar.appendChild(convertBtn);
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
 * Hide the action bar within a specific container with slide-up animation
 */
export function hideActionBar(container: HTMLElement): void {
    const bar = container.querySelector('.canvas-action-bar') as HTMLElement | null;
    if (!bar) return;

    // Cancel any running animations (if Web Animations API is available)
    if (typeof bar.getAnimations === 'function') {
        bar.getAnimations().forEach(anim => anim.cancel());
    }

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
