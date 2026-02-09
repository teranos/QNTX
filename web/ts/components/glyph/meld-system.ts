/**
 * Meld System — barrel re-export
 *
 * Split into focused modules:
 *   meld-detect.ts      — proximity detection and target finding
 *   meld-feedback.ts    — visual proximity cues (box shadows)
 *   meld-composition.ts — create, extend, reconstruct, unmeld compositions
 */

export { canInitiateMeld, canReceiveMeld, findMeldTarget, PROXIMITY_THRESHOLD, MELD_THRESHOLD } from './meld-detect';
export { applyMeldFeedback, clearMeldFeedback } from './meld-feedback';
export { performMeld, extendComposition, reconstructMeld, isMeldedComposition, unmeldComposition, mergeCompositions } from './meld-composition';
