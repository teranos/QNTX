# QNTX Command Palette Fonts

## Bytesized Font

The command palette uses the **Bytesized** pixel font for its retro terminal aesthetic.

### Setup

To use the Bytesized font:

1. Download `Bytesized.ttf` from [Google Fonts - Bytesized](https://fonts.google.com/specimen/Bytesized) or your preferred source
2. Place the `.ttf` file in this directory: `internal/graph/web/fonts/`

### Fallback Behavior

If the Bytesized font is not available:
- The palette will automatically fall back to `Courier New` and then `monospace`
- The functionality remains intact, just without the pixel art aesthetic
- This ensures the command palette is always usable

### Font Formats

The CSS is configured to support multiple font formats:
- `.ttf` - TrueType (primary)
- `.woff2` - Web Open Font Format 2 (optimal for web)
- `.woff` - Web Open Font Format (legacy support)

If you want to optimize for web delivery, convert the TTF to WOFF2 format using tools like:
- [Google Fonts Squirrel](https://www.fontsquirrel.com/tools/webfont-generator)
- [cloudconvert.com](https://cloudconvert.com/ttf-to-woff2)

### Current Status

The command palette is fully functional with or without the Bytesized font installed. The aesthetic will adapt gracefully based on available fonts.
