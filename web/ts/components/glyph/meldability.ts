/**
 * Meldability - Defines which glyphs can meld and their relationships
 *
 * Melding is QNTX's spatial composition model where glyphs physically fuse
 * through proximity rather than connecting via wires.
 *
 * Each relationship has semantic meaning:
 * - ax → prompt: Query fills template variables
 * - ax → py: Query drives Python script execution
 * - py → prompt: Script output populates template
 */

/**
 * Meldability rules: maps initiator classes to compatible target classes
 *
 * Phase 2 extension: py → py chaining enabled for sequential pipelines
 */
export const MELDABILITY: Record<string, readonly string[]> = {
    'canvas-ax-glyph': ['canvas-prompt-glyph', 'canvas-py-glyph'],
    'canvas-py-glyph': ['canvas-prompt-glyph', 'canvas-py-glyph']
} as const;

/**
 * Get all classes that can initiate melding
 */
export function getInitiatorClasses(): string[] {
    return Object.keys(MELDABILITY);
}

/**
 * Get all classes that can receive meld
 */
export function getTargetClasses(): string[] {
    return [...new Set(Object.values(MELDABILITY).flat())];
}

/**
 * Get compatible target classes for a given initiator class
 */
export function getCompatibleTargets(initiatorClass: string): readonly string[] {
    return MELDABILITY[initiatorClass] || [];
}

/**
 * Check if two glyph classes are compatible for melding
 */
export function areClassesCompatible(initiatorClass: string, targetClass: string): boolean {
    const compatibleTargets = MELDABILITY[initiatorClass];
    return compatibleTargets ? compatibleTargets.includes(targetClass) : false;
}

/**
 * Extract glyph IDs from a composition element
 * Returns array of glyph IDs in left-to-right order
 */
export function getCompositionGlyphIds(composition: HTMLElement): string[] {
    const glyphElements = composition.querySelectorAll('[data-glyph-id]');
    const glyphIds: string[] = [];

    glyphElements.forEach(el => {
        const id = el.getAttribute('data-glyph-id');
        if (id) glyphIds.push(id);
    });

    return glyphIds;
}

/**
 * Get the CSS class of the rightmost glyph in a composition
 * Used to determine if an initiator glyph can extend the composition
 */
export function getCompositionRightmostGlyphClass(composition: HTMLElement): string | null {
    const glyphElements = composition.querySelectorAll('[data-glyph-id]');
    if (glyphElements.length === 0) return null;

    const rightmostGlyph = glyphElements[glyphElements.length - 1] as HTMLElement;
    return rightmostGlyph.className;
}

/**
 * Check if an initiator glyph can meld with a composition
 * A glyph can meld with a composition if it's compatible with the composition's rightmost glyph
 */
export function canMeldWithComposition(initiatorClass: string, composition: HTMLElement): boolean {
    const rightmostClass = getCompositionRightmostGlyphClass(composition);
    if (!rightmostClass) return false;

    // Check if initiator is compatible with the rightmost glyph's class
    return areClassesCompatible(initiatorClass, rightmostClass);
}
