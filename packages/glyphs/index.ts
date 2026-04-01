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
export { configureGlyphs, stripHtml, getLogger, getLogSegment, getPersistence } from './config';
export type { GlyphConfig, GlyphLogger, GlyphPersistence } from './config';

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

// Window drag — standalone, no canvas dependency
export { setupWindowDrag, teardownWindowDrag } from './window-drag';

// Manifestations
export { morphToWindow, morphFromWindow } from './manifestations/window';
export { morphToCanvas, morphFromCanvas } from './manifestations/canvas';
export { morphToPanel, morphFromPanel } from './manifestations/panel';

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
    buildFocusGraph,
} from './edge-graph';

// Focus manifestation — DAG-aware thread layout
export {
    focusGlyph,
    unfocusGlyph,
    isFocused,
    getFocusedGlyphId,
    setupCanvasFocus,
    resetFocusState,
} from './manifestations/focus';
export type {
    FocusGraph,
    FocusGraphProvider,
    ColumnProvider,
    FocusDeps,
    FocusPanControl,
    FocusPersistence,
} from './manifestations/focus';

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
