package handlers

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/sym"
)

// HandleExportStatic renders the canvas as a self-contained static HTML page.
// GET /api/canvas/export/static — returns text/html with Content-Disposition attachment.
func (h *CanvasHandler) HandleExportStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	glyphs, err := h.store.ListGlyphs(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to list glyphs for export"), http.StatusInternalServerError)
		return
	}

	compositions, err := h.store.ListCompositions(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to list compositions for export"), http.StatusInternalServerError)
		return
	}

	htmlContent := renderStaticCanvas(glyphs, compositions)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="canvas.html"`)
	fmt.Fprint(w, htmlContent)
}

// renderStaticCanvas produces a self-contained HTML document from canvas state.
func renderStaticCanvas(glyphs []*storage.CanvasGlyph, compositions []*storage.CanvasComposition) string {
	// Build glyph ID → composition membership map
	glyphToComp := make(map[string]*storage.CanvasComposition)
	for _, comp := range compositions {
		for _, edge := range comp.Edges {
			glyphToComp[edge.From] = comp
			glyphToComp[edge.To] = comp
		}
	}

	// Track which compositions we've already rendered
	renderedComps := make(map[string]bool)

	var glyphsHTML strings.Builder
	for _, g := range glyphs {
		// Skip glyphs that belong to a composition — they'll be rendered inside it
		if comp, ok := glyphToComp[g.ID]; ok {
			if renderedComps[comp.ID] {
				continue
			}
			renderedComps[comp.ID] = true
			glyphsHTML.WriteString(renderComposition(comp, glyphs))
			continue
		}
		glyphsHTML.WriteString(renderGlyph(g))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>QNTX Canvas</title>
<style>
%s
</style>
</head>
<body>
<div class="canvas-workspace">
<div class="canvas-content-layer">
%s
</div>
</div>
</body>
</html>`, staticCSS, glyphsHTML.String())
}

// renderComposition renders a melded composition container with its member glyphs.
func renderComposition(comp *storage.CanvasComposition, allGlyphs []*storage.CanvasGlyph) string {
	glyphMap := make(map[string]*storage.CanvasGlyph)
	for _, g := range allGlyphs {
		glyphMap[g.ID] = g
	}

	// Collect unique glyph IDs from edges, preserving order
	var memberIDs []string
	seen := make(map[string]bool)
	for _, edge := range comp.Edges {
		if !seen[edge.From] {
			memberIDs = append(memberIDs, edge.From)
			seen[edge.From] = true
		}
		if !seen[edge.To] {
			memberIDs = append(memberIDs, edge.To)
			seen[edge.To] = true
		}
	}

	var members strings.Builder
	for _, id := range memberIDs {
		if g, ok := glyphMap[id]; ok {
			// Render member glyphs without absolute positioning (grid handles layout)
			members.WriteString(renderGlyphInline(g))
		}
	}

	return fmt.Sprintf(`<div class="melded-composition" style="left:%dpx;top:%dpx;">
%s
</div>`, comp.X, comp.Y, members.String())
}

// renderGlyph renders a single glyph with absolute positioning.
func renderGlyph(g *storage.CanvasGlyph) string {
	className := glyphClassName(g.Symbol)
	title := glyphTitle(g.Symbol)
	style := glyphStyle(g)

	content := renderGlyphContent(g)

	var titleBarHTML string
	if title != "" && !isNoteGlyph(g.Symbol) {
		titleBarHTML = fmt.Sprintf(`<div class="canvas-glyph-title-bar"><span>%s</span></div>`, html.EscapeString(title))
	}

	var extraStyle string
	if isNoteGlyph(g.Symbol) {
		extraStyle = noteInlineStyle()
	}

	return fmt.Sprintf(`<div class="%s canvas-glyph" data-glyph-id="%s" style="%s%s">
%s%s
</div>
`, className, html.EscapeString(g.ID), style, extraStyle, titleBarHTML, content)
}

// renderGlyphInline renders a glyph without absolute positioning (for use inside compositions).
func renderGlyphInline(g *storage.CanvasGlyph) string {
	className := glyphClassName(g.Symbol)
	title := glyphTitle(g.Symbol)
	content := renderGlyphContent(g)

	var titleBarHTML string
	if title != "" && !isNoteGlyph(g.Symbol) {
		titleBarHTML = fmt.Sprintf(`<div class="canvas-glyph-title-bar"><span>%s</span></div>`, html.EscapeString(title))
	}

	sizeStyle := ""
	if g.Width != nil {
		sizeStyle += fmt.Sprintf("width:%dpx;", *g.Width)
	}
	if g.Height != nil {
		sizeStyle += fmt.Sprintf("min-height:%dpx;", *g.Height)
	}

	return fmt.Sprintf(`<div class="%s canvas-glyph" data-glyph-id="%s" style="position:relative;%s">
%s%s
</div>
`, className, html.EscapeString(g.ID), sizeStyle, titleBarHTML, content)
}

// renderGlyphContent returns the inner HTML for a glyph's content based on its symbol type.
func renderGlyphContent(g *storage.CanvasGlyph) string {
	content := ""
	if g.Content != nil {
		content = *g.Content
	}

	switch g.Symbol {
	case "py":
		return renderCodeContent(content, "python")
	case "ts":
		return renderCodeContent(content, "typescript")
	case sym.SO: // Prompt
		return renderCodeContent(content, "yaml")
	case sym.AX: // AX query
		return renderQueryContent(content)
	case sym.SE: // Semantic search
		return renderQueryContent(content)
	case sym.IX: // Ingest
		return renderCodeContent(content, "text")
	case sym.Prose: // Note
		return renderNoteContent(content)
	case sym.Doc: // Document
		return renderDocContent(content)
	case sym.Subcanvas: // Subcanvas
		return `<div class="subcanvas-preview"></div>`
	case sym.AS: // Attestation
		return renderAttestationContent(content)
	default:
		// result, error, or unknown — render as preformatted text
		if content != "" {
			return fmt.Sprintf(`<div class="glyph-content"><pre>%s</pre></div>`, html.EscapeString(content))
		}
		return `<div class="glyph-content"></div>`
	}
}

func renderCodeContent(content, lang string) string {
	if content == "" {
		content = "// empty"
	}
	return fmt.Sprintf(`<div class="glyph-content"><pre><code class="language-%s">%s</code></pre></div>`,
		html.EscapeString(lang), html.EscapeString(content))
}

func renderQueryContent(content string) string {
	return fmt.Sprintf(`<div class="glyph-content glyph-query"><code>%s</code></div>`,
		html.EscapeString(content))
}

func renderNoteContent(content string) string {
	// Markdown is stored as plain text — render as-is with whitespace preserved
	if content == "" {
		return `<div class="glyph-content note-content"></div>`
	}
	return fmt.Sprintf(`<div class="glyph-content note-content">%s</div>`,
		html.EscapeString(content))
}

func renderDocContent(content string) string {
	if content == "" {
		return `<div class="glyph-content doc-placeholder">Document</div>`
	}
	return fmt.Sprintf(`<div class="glyph-content doc-placeholder">%s</div>`,
		html.EscapeString(content))
}

func renderAttestationContent(content string) string {
	if content == "" {
		return `<div class="glyph-content attestation-content">+</div>`
	}
	return fmt.Sprintf(`<div class="glyph-content attestation-content"><pre>%s</pre></div>`,
		html.EscapeString(content))
}

// glyphClassName maps symbol to CSS class name.
func glyphClassName(symbol string) string {
	switch symbol {
	case sym.AX:
		return "canvas-ax-glyph"
	case sym.SE:
		return "canvas-se-glyph"
	case "py":
		return "canvas-py-glyph"
	case sym.IX:
		return "canvas-ix-glyph"
	case sym.SO:
		return "canvas-prompt-glyph"
	case "ts":
		return "canvas-ts-glyph"
	case sym.Prose:
		return "canvas-note-glyph"
	case sym.Doc:
		return "canvas-doc-glyph"
	case sym.Subcanvas:
		return "canvas-subcanvas-glyph"
	case sym.AS:
		return "canvas-attestation-glyph"
	default:
		return "canvas-glyph-unknown"
	}
}

// glyphTitle returns the human-readable title for a glyph symbol.
func glyphTitle(symbol string) string {
	switch symbol {
	case sym.AX:
		return sym.AX + " Query"
	case sym.SE:
		return sym.SE + " Semantic"
	case "py":
		return "Python"
	case sym.IX:
		return sym.IX + " Ingest"
	case sym.SO:
		return sym.SO + " Prompt"
	case "ts":
		return "TypeScript"
	case sym.Prose:
		return ""
	case sym.Doc:
		return "Document"
	case sym.Subcanvas:
		return sym.Subcanvas + " Subcanvas"
	case sym.AS:
		return sym.AS + " Attestation"
	default:
		return symbol
	}
}

func isNoteGlyph(symbol string) bool {
	return symbol == sym.Prose
}

func glyphStyle(g *storage.CanvasGlyph) string {
	s := fmt.Sprintf("left:%dpx;top:%dpx;", g.X, g.Y)
	if g.Width != nil {
		s += fmt.Sprintf("width:%dpx;", *g.Width)
	}
	if g.Height != nil {
		s += fmt.Sprintf("min-height:%dpx;", *g.Height)
	}
	return s
}

func noteInlineStyle() string {
	return "background-color:#f5edb8;border:1px solid #d4c59a;border-radius:2px;box-shadow:2px 2px 8px rgba(0,0,0,0.15);color:#2a2a2a;"
}

// staticCSS contains all styles needed to render the canvas snapshot.
// Inlined to produce a fully self-contained HTML file.
const staticCSS = `
/* Reset */
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

body {
    font-family: system-ui, -apple-system, sans-serif;
    margin: 0;
    padding: 0;
    background: #1a1b1a;
    color: #dfe1e0;
}

/* Canvas workspace */
.canvas-workspace {
    width: 100%;
    min-height: 100vh;
    position: relative;
    overflow: auto;
    background-color: #2d2e36;
    background-image:
        repeating-linear-gradient(0deg, transparent, transparent 23px, #2f353c 23px, #2f353c 24px),
        repeating-linear-gradient(90deg, transparent, transparent 23px, #2f353c 23px, #2f353c 24px);
}

.canvas-content-layer {
    position: relative;
    min-height: 100vh;
}

/* Glyph base */
.canvas-glyph {
    position: absolute;
    display: flex;
    flex-direction: column;
    background-color: #252625;
    border: 1px solid #3f4140;
    border-radius: 4px;
    overflow: hidden;
}

/* Title bar */
.canvas-glyph-title-bar {
    height: 32px;
    background-color: #2e2f2e;
    border-bottom: 1px solid #3f4140;
    display: flex;
    align-items: center;
    padding: 0 8px;
    gap: 8px;
    flex-shrink: 0;
    font-size: 13px;
    color: rgba(255, 255, 255, 0.7);
}

/* Content */
.glyph-content {
    flex: 1;
    padding: 8px;
    overflow: auto;
    font-size: 13px;
}

.glyph-content pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-wrap: break-word;
    font-family: 'JetBrains Mono', 'SF Mono', 'Monaco', 'Fira Code', 'Consolas', monospace;
    font-size: 13px;
    line-height: 1.5;
    color: #d4d4d4;
}

.glyph-content code {
    font-family: 'JetBrains Mono', 'SF Mono', 'Monaco', 'Fira Code', 'Consolas', monospace;
    font-size: 13px;
}

/* Query glyphs (AX, SE) */
.glyph-query {
    display: flex;
    align-items: center;
    padding: 6px 8px;
    font-family: 'JetBrains Mono', 'SF Mono', monospace;
    font-size: 13px;
    color: #d4d4d4;
}

/* Note glyph (post-it) */
.note-content {
    padding: 4px;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    font-size: 14px;
    line-height: 1.2;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-wrap: break-word;
}

/* Attestation glyph */
.canvas-attestation-glyph {
    background: #1a1b1a;
    border: 1px solid #3f4140;
    border-radius: 3px;
    box-shadow: 2px 2px 8px rgba(0, 0, 0, 0.2);
    color: rgba(255, 255, 255, 0.85);
}

.attestation-content {
    font-family: monospace;
    font-size: 12px;
    color: rgba(255, 255, 255, 0.85);
}

/* Document glyph */
.canvas-doc-glyph {
    border: 1px solid #3f4140;
    border-radius: 3px;
    box-shadow: 2px 2px 8px rgba(0, 0, 0, 0.2);
    color: rgba(255, 255, 255, 0.85);
}

.doc-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #999;
    font-size: 14px;
}

/* Subcanvas glyph */
.canvas-subcanvas-glyph .canvas-glyph-title-bar {
    background: #3a2d5a;
    color: #c0a0e8;
}

.subcanvas-preview {
    flex: 1;
    overflow: hidden;
    background-color: #2d2e36;
    background-image:
        repeating-linear-gradient(0deg, transparent, transparent 9px, rgba(160, 120, 200, 0.08) 9px, rgba(160, 120, 200, 0.08) 10px),
        repeating-linear-gradient(90deg, transparent, transparent 9px, rgba(160, 120, 200, 0.08) 9px, rgba(160, 120, 200, 0.08) 10px);
    min-height: 60px;
}

/* Python glyph title bar accent */
.canvas-py-glyph .canvas-glyph-title-bar {
    background: #2a5578;
}

/* TypeScript glyph title bar accent */
.canvas-ts-glyph .canvas-glyph-title-bar {
    background: #2d4f7c;
}

/* Prompt glyph title bar accent */
.canvas-prompt-glyph .canvas-glyph-title-bar {
    background: #3d2e2e;
}

/* Melded composition */
.melded-composition {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: auto;
    gap: 0;
    position: absolute;
    background: transparent;
}

.melded-composition .canvas-glyph {
    position: relative;
}

/* Responsive: scroll on small screens */
@media (max-width: 768px) {
    .canvas-workspace {
        overflow: auto;
        -webkit-overflow-scrolling: touch;
    }
}
`
