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
 */
export const MELDABILITY: Record<string, readonly string[]> = {
    'canvas-ax-glyph': ['canvas-prompt-glyph', 'canvas-py-glyph'],
    'canvas-py-glyph': ['canvas-prompt-glyph']
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
