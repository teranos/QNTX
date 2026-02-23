package handlers

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/sym"
)

// HandleExportStatic renders a subcanvas as a self-contained static HTML page.
// GET /api/canvas/export/static?canvas_id=<id> — returns text/html with Content-Disposition attachment.
// Only available in demo mode (QNTX_DEMO=1).
func (h *CanvasHandler) HandleExportStatic(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("QNTX_DEMO") != "1" {
		h.writeError(w, errors.New("canvas export only available in demo mode (make demo)"), http.StatusForbidden)
		return
	}

	if r.Method != http.MethodGet {
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	canvasID := r.URL.Query().Get("canvas_id")
	if canvasID == "" {
		h.writeError(w, errors.New("canvas_id query parameter is required"), http.StatusBadRequest)
		return
	}

	glyphs, err := h.store.ListGlyphsByCanvas(r.Context(), canvasID)
	if err != nil {
		h.writeError(w, errors.Wrapf(err, "failed to list glyphs for canvas %s", canvasID), http.StatusInternalServerError)
		return
	}

	compositions, err := h.store.ListCompositions(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to list compositions for export"), http.StatusInternalServerError)
		return
	}

	// Filter compositions to only those whose members all belong to this canvas
	glyphIDs := make(map[string]bool, len(glyphs))
	for _, g := range glyphs {
		glyphIDs[g.ID] = true
	}
	var canvasComps []*storage.CanvasComposition
	for _, comp := range compositions {
		allLocal := true
		for _, edge := range comp.Edges {
			if !glyphIDs[edge.From] || !glyphIDs[edge.To] {
				allLocal = false
				break
			}
		}
		if allLocal {
			canvasComps = append(canvasComps, comp)
		}
	}

	htmlContent, err := renderStaticCanvas(glyphs, canvasComps)
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to render static canvas"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="canvas.html"`)
	fmt.Fprint(w, htmlContent)
}

// renderStaticCanvas produces a self-contained HTML document from canvas state.
func renderStaticCanvas(glyphs []*storage.CanvasGlyph, compositions []*storage.CanvasComposition) (string, error) {
	// Read CSS files from web directory
	coreCSS, err := os.ReadFile(filepath.Join("web", "css", "core.css"))
	if err != nil {
		return "", errors.Wrapf(err, "failed to read core.css")
	}
	canvasCSS, err := os.ReadFile(filepath.Join("web", "css", "canvas.css"))
	if err != nil {
		return "", errors.Wrapf(err, "failed to read canvas.css")
	}
	combinedCSS := string(coreCSS) + "\n" + string(canvasCSS)

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
</html>`, combinedCSS, glyphsHTML.String()), nil
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
