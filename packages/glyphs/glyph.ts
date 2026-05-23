/**
 * Glyph - The universal primitive
 *
 * A glyph is exactly ONE DOM element for its entire lifetime.
 * It can morph between different visual states (dot, proximity, window, canvas, panel, etc.)
 * through smooth animations, but the element identity never changes.
 *
 * All glyphs are container glyphs - they can hold child glyphs in various layout strategies.
 *
 * This file contains just the Glyph interface and shared constants.
 * Manifestation logic lives in ./manifestations/*
 */

export interface Glyph {
    id: string;
    title: string;
    renderContent: () => HTMLElement;    // Function to render content
    renderTitleBar?: () => HTMLElement;   // Glyph-specific title bar, enhanced by manifestations with window controls

    // Manifestation configuration
    manifestationType?: 'window' | 'canvas' | 'panel' | 'ax' | 'cursor';  // Default: 'window'
    // TODO: 'ax' is currently its own manifestation type but may not need to be —
    // it renders inline on canvas for query editing, which could be a behavior of
    // 'canvas' or a generic 'inline' type rather than an AX-specific concept
    // TODO: Add 'programmature' manifestation type for full code editor that can minimize to tray
    initialWidth?: string;               // Initial dimensions (e.g., "800px")
    initialHeight?: string;
    defaultX?: number;                   // Default position
    defaultY?: number;

    // Lifecycle hooks
    onClose?: () => void;

    // Fractal container support - all glyphs can contain children
    children?: Glyph[];                  // Child glyphs this glyph contains
    layoutStrategy?: 'flow' | 'grid' | 'custom';  // How to layout children (default: flow)
    onSpawnMenu?: () => string[];        // Symbols that can be spawned inside (right-click)

    // Position metadata (pixel coordinates)
    x?: number;                          // X position in pixels
    y?: number;                          // Y position in pixels
    symbol?: string;                     // Symbol to display

    // Size metadata (for resizable glyphs)
    width?: number;                      // Custom width in pixels
    height?: number;                     // Custom height in pixels

    // Glyph content: source code, markdown, template, or JSON result
    content?: string;

    // Visual identity — every manifestation (dot, window, panel) reads these
    color?: string;      // Background color (default: DEFAULT_GLYPH_COLOR)
    textColor?: string;  // Text color (default: 'rgb(255,255,255)')

    // Pre-existing DOM element from cursor manifestation (placement mode).
    // When set, canvasPlaced reuses this element instead of creating a new one.
    cursorElement?: HTMLElement;
}

export const DEFAULT_GLYPH_COLOR = 'rgba(35, 35, 38, 0.92)';
export const DEFAULT_GLYPH_TEXT_COLOR = 'rgb(255,255,255)';

// Function to check if user prefers reduced motion
function getPrefersReducedMotion(): boolean {
    if (typeof window !== 'undefined' && window.matchMedia) {
        return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    }
    return false; // Default to animations enabled in test environment
}

// Animation durations in milliseconds
export const MAXIMIZE_DURATION_MS = 450;  // Base duration for dot → manifestation
export const MINIMIZE_DURATION_MS = 350;  // Base duration for manifestation → dot

// Get actual durations considering reduced motion preference
export function getMaximizeDuration(): number {
    return getPrefersReducedMotion() ? 0 : MAXIMIZE_DURATION_MS;
}

export function getMinimizeDuration(): number {
    return getPrefersReducedMotion() ? 0 : MINIMIZE_DURATION_MS;
}

// Window manifestation constants (used by manifestations/window.ts)
export const DEFAULT_WINDOW_WIDTH = '800px';
export const DEFAULT_WINDOW_HEIGHT = '600px';
export const WINDOW_BORDER_RADIUS = '8px';
export const WINDOW_BOX_SHADOW = '0 4px 12px rgba(0, 0, 0, 0.15)';

// Window chrome dimensions
export const TITLE_BAR_HEIGHT = '32px';
export const WINDOW_BUTTON_SIZE = '24px';
export const CONTENT_PADDING = '16px';

// Panel manifestation constants (used by manifestations/panel.ts)
export const PANEL_BORDER_RADIUS = '0 0 12px 12px';  // Rounded bottom when sliding from top
export const PANEL_BORDER_RADIUS_BOTTOM = '12px 12px 0 0';  // Rounded top when sliding from bottom
export const PANEL_OVERLAY_BG = 'rgba(0, 0, 0, 0.4)';
export const PANEL_Z_INDEX = '10003';  // Above system drawer (10002)

// Canvas glyph dimensions
export const CANVAS_GLYPH_TITLE_BAR_HEIGHT = 32; // Height in pixels for AX glyphs
export const CANVAS_GLYPH_CONTENT_PADDING = 8; // Content element padding (reduced from CONTENT_PADDING)
export const GLYPH_CONTENT_INNER_PADDING = 4; // .glyph-content CSS padding

// ResizeObserver constraints
export const MAX_VIEWPORT_HEIGHT_RATIO = 0.8; // Don't exceed 80% of viewport height
export const MAX_VIEWPORT_WIDTH_RATIO = 0.8; // Don't exceed 80% of viewport width
export const MIN_WINDOW_HEIGHT = 100; // Minimum window height in pixels
export const MIN_WINDOW_WIDTH = 200; // Minimum window width in pixels
