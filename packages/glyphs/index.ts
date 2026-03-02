/**
 * @qntx/glyphs — Type definitions for QNTX glyph UI authoring.
 *
 * Plugin repos import types from this package for type-safe glyph development.
 * Monorepo-only — re-exports via relative paths that break outside this repo.
 * Runtime is injected by the host — the `ui` parameter in render() provides
 * the real implementations of GlyphUI at render time.
 *
 * Usage:
 *   import type { Glyph, GlyphUI, RenderFn } from '@qntx/glyphs';
 *
 *   export const render: RenderFn = (glyph, ui) => {
 *       const { element } = ui.container({ ... });
 *       return element;
 *   };
 */

// Glyph primitive
export type { Glyph } from '../../web/ts/components/glyph/glyph';

// GlyphUI interface and related types
export type {
    GlyphUI,
    GlyphModule,
    RenderFn,
    ContainerOpts,
    FetchOpts,
} from '../../web/ts/components/glyph/glyph-ui';
