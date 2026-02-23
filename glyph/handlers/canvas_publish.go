package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/glyph/ipfs"
	"github.com/teranos/QNTX/glyph/storage"
)

// PublishResponse is returned by HandlePublish.
type PublishResponse struct {
	CID     string `json:"cid,omitempty"`
	IPFSURL string `json:"url,omitempty"`
	Path    string `json:"path,omitempty"`
}

// HandlePublish renders a subcanvas as static HTML and publishes it.
// Publishes to IPFS (if Pinata configured) and writes docs/demo/index.html.
// POST /api/canvas/publish — requires canvas_id in JSON body, returns PublishResponse.
// Only available in demo mode (QNTX_DEMO=1).
func (h *CanvasHandler) HandlePublish(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("QNTX_DEMO") != "1" {
		h.writeError(w, errors.New("canvas publish only available in demo mode (make demo)"), http.StatusForbidden)
		return
	}

	if r.Method != http.MethodPost {
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		CanvasID string `json:"canvas_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}
	if body.CanvasID == "" {
		h.writeError(w, errors.New("canvas_id is required"), http.StatusBadRequest)
		return
	}

	glyphs, err := h.store.ListGlyphsByCanvas(r.Context(), body.CanvasID)
	if err != nil {
		h.writeError(w, errors.Wrapf(err, "failed to list glyphs for canvas %s", body.CanvasID), http.StatusInternalServerError)
		return
	}

	compositions, err := h.store.ListCompositions(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to list compositions for publish"), http.StatusInternalServerError)
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

	resp := PublishResponse{}

	// Pin to IPFS via Pinata (if configured)
	if h.pinataJWT != "" {
		pinResp, err := ipfs.PinFile(h.pinataJWT, "canvas.html", []byte(htmlContent))
		if err != nil {
			h.writeError(w, errors.Wrap(err, "failed to pin canvas to IPFS"), http.StatusBadGateway)
			return
		}

		gateway := h.pinataGateway
		if gateway == "" {
			gateway = "https://gateway.pinata.cloud"
		}

		resp.CID = pinResp.IpfsHash
		resp.IPFSURL = gateway + "/ipfs/" + pinResp.IpfsHash
	}

	// Write rendered HTML to docs/demo/index.html
	demoPath, err := writeDemoHTML(htmlContent)
	if err != nil {
		if resp.CID == "" {
			h.writeError(w, errors.Wrap(err, "failed to write demo canvas"), http.StatusInternalServerError)
			return
		}
		// IPFS succeeded, file write failed — still return 200
	} else {
		resp.Path = demoPath
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeDemoHTML writes the rendered HTML to docs/demo/index.html relative to
// the working directory. Returns the path written.
func writeDemoHTML(htmlContent string) (string, error) {
	// Use working directory — no git dependency
	wd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "failed to get working directory")
	}

	demoDir := filepath.Join(wd, "docs", "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		return "", errors.Wrapf(err, "failed to create %s", demoDir)
	}

	demoPath := filepath.Join(demoDir, "index.html")
	if err := os.WriteFile(demoPath, []byte(htmlContent), 0644); err != nil {
		return "", errors.Wrapf(err, "failed to write %s", demoPath)
	}

	// Return relative path for display
	rel, err := filepath.Rel(wd, demoPath)
	if err != nil {
		rel = demoPath
	}
	return filepath.ToSlash(strings.TrimPrefix(rel, "./")), nil
}
