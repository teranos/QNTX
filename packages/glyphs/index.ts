/**
 * @qntx/glyphs — Glyph runtime and type definitions.
 *
 * The glyph is the universal UI primitive. This package provides the core
 * runtime (tray, proximity engine, morph transactions, manifestations) and
 * type definitions for glyph development.
 *
 * Host apps call configureGlyphs() at startup to wire in their logger,
 * persistence, and HTML stripping. Without configuration, safe defaults apply.
 *
 * Usage:
 *   import { configureGlyphs, GlyphProximity } from '@qntx/glyphs';
 *   import type { Glyph, GlyphUI, RenderFn } from '@qntx/glyphs';
 */

// Configuration / dependency injection
export { configureGlyphs, stripHtml, getLogger, getLogSegment, getPersistence, getCanvasHost, getCanvasBridge, removeCanvasGlyph } from './config';
export type { GlyphConfig, GlyphLogger, GlyphPersistence, CanvasGlyphData, CanvasHost, CanvasCoordinateBridge } from './config';

// Glyph primitive — interface + constants
export {
    MAXIMIZE_DURATION_MS,
    MINIMIZE_DURATION_MS,
    getMaximizeDuration,
    getMinimizeDuration,
    DEFAULT_WINDOW_WIDTH,
    DEFAULT_WINDOW_HEIGHT,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    TITLE_BAR_HEIGHT,
    WINDOW_BUTTON_SIZE,
    CONTENT_PADDING,
    PANEL_BORDER_RADIUS,
    PANEL_BORDER_RADIUS_BOTTOM,
    PANEL_OVERLAY_BG,
    PANEL_Z_INDEX,
    CANVAS_GLYPH_TITLE_BAR_HEIGHT,
    CANVAS_GLYPH_CONTENT_PADDING,
    GLYPH_CONTENT_INNER_PADDING,
    MAX_VIEWPORT_HEIGHT_RATIO,
    MAX_VIEWPORT_WIDTH_RATIO,
    MIN_WINDOW_HEIGHT,
    MIN_WINDOW_WIDTH,
    DEFAULT_GLYPH_COLOR,
    DEFAULT_GLYPH_TEXT_COLOR,
} from './glyph';
export type { Glyph } from './glyph';

// Dataset attribute helpers
export {
    isInWindowState,
    setWindowState,
    getLastPosition,
    setLastPosition,
    hasProximityText,
    setProximityText,
    getGlyphId,
    setGlyphId,
    setCanvasOrigin,
    getCanvasOrigin,
    clearCanvasOrigin,
    getGlyphSymbol,
    setGlyphSymbol,
} from './dataset';

// Proximity engine
export { GlyphProximity } from './proximity';

// Morph transactions — Web Animations API with commit/rollback
export {
    beginMinimizeMorph,
    beginMaximizeMorph,
    beginRestoreMorph,
    cancelMorph,
} from './morph-transaction';

// Manifestation helpers
export {
    verifyGlyphAxiom,
    prepareMorphTo,
    calculateTrayTarget,
    resetGlyphElement,
} from './manifestations/morphology';

export { addWindowControls, removeWindowControls } from './manifestations/title-bar-controls';
export type { WindowControlsConfig } from './manifestations/title-bar-controls';

export { stashContent, restoreContent, hasStash } from './manifestations/stash';

export { renderGlyphContent } from './manifestations/render-content';
export type { RenderContentResult } from './manifestations/render-content';

// Canvas-window manifestation — canvas ↔ window morphing
export {
    morphCanvasPlacedToWindow,
    morphWindowToCanvasPlaced,
    placeWindowOnCanvas,
} from './manifestations/canvas-window';
export type { CanvasWindowConfig } from './manifestations/canvas-window';

// Expand-to-window — unified lifecycle wiring
export { wireExpandToWindow } from './expand-to-window';
export type { ExpandToWindowConfig } from './expand-to-window';

// Window drag — standalone, no canvas dependency
export { setupWindowDrag, teardownWindowDrag } from './window-drag';

// Manifestations
export { morphToWindow, morphFromWindow } from './manifestations/window';
export { morphToCanvas, morphFromCanvas } from './manifestations/canvas';
export { morphToPanel, morphFromPanel } from './manifestations/panel';

// Cursor manifestation — transient placement preview
export { createCursorElement, attachCursorToMouse, prepareCursorForPlacement, commitCursorPlacement } from './manifestations/cursor';

// Canvas-placed factory (CPLCD)
export { canvasPlaced } from './manifestations/canvas-placed';
export type { CanvasPlacedConfig, CanvasPlacedResult } from './manifestations/canvas-placed';

// GlyphRun tray singleton
export { glyphRun } from './run';

// Composition types — canonical, package-owned (CTYPE)
export type { CompositionEdge, CompositionState, EdgeDirection } from './composition';
export { buildEdgesFromChain, extractGlyphIds } from './composition';

// Edge graph — pure DAG traversal and layout (EWALK + GRDLP)
export {
    getRootGlyphIds,
    getLeafGlyphIds,
    isPortFree,
    isConnectedGraph,
    computeGridPositions,
} from './edge-graph';

// Touch browse
export { setupTouchBrowse, findPeakedGlyph } from './touch-browse';
export type { TouchBrowseHost } from './touch-browse';

// Meld system
export {
    canInitiateMeld,
    canReceiveMeld,
    findMeldTarget,
    checkDirectionalProximity,
    PROXIMITY_THRESHOLD,
    MELD_THRESHOLD,
} from './meld/meld-detect';
export { applyMeldFeedback, clearMeldFeedback } from './meld/meld-feedback';
export {
    performMeld,
    extendComposition,
    reconstructMeld,
    isMeldedComposition,
    unmeldComposition,
    detachGlyph,
} from './meld/meld-composition';
export {
    MELDABILITY,
    getInitiatorClasses,
    getTargetClasses,
    getCompatibleTargets,
    getCompatibleDirections,
    areClassesCompatible,
    getCompositionGlyphIds,
    getGlyphClass,
    getMeldOptions,
    selectPreferredMeldOption,
} from './meld/meldability';
export type { PortRule, MeldOption } from './meld/meldability';

// Canvas drag interaction (DRAGR)
export {
    makeDraggable,
    applyCanvasGlyphLayout,
    preventDrag,
} from './canvas-drag';
export type { CanvasGlyphLayoutOptions } from './canvas-drag';

// Canvas resize interaction
export { makeResizable } from './canvas-resize';
export type { MakeResizableOptions } from './canvas-resize';

// Glyph element lifecycle cleanup
export {
    storeCleanup,
    runCleanup,
    cleanupResizeObserver,
    setupGlyphResizeObserver,
} from './canvas-cleanup';

// GlyphUI interface and related types
export type {
    GlyphUI,
    GlyphModule,
    GlyphDef,
    RenderFn,
    GlyphOpts,
    FetchOpts,
    MeldEvent,
    SpawnResultDetail,
    MakeDraggableOptions,
} from './glyph-ui';
