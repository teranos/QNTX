package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/glyph/ipfs"
)

// PublishResponse is returned by HandlePublish.
type PublishResponse struct {
	CID        string `json:"cid,omitempty"`
	IPFSURL    string `json:"url,omitempty"`
	GitPath    string `json:"git_path,omitempty"`
	GitCommit  string `json:"git_commit,omitempty"`
}

// HandlePublish renders the canvas as static HTML and publishes it.
// Publishes to IPFS (if Pinata configured) and to git (docs/demo/index.html).
// POST /api/canvas/publish — returns PublishResponse.
func (h *CanvasHandler) HandlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	glyphs, err := h.store.ListGlyphs(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to list glyphs for publish"), http.StatusInternalServerError)
		return
	}

	compositions, err := h.store.ListCompositions(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to list compositions for publish"), http.StatusInternalServerError)
		return
	}

	htmlContent := renderStaticCanvas(glyphs, compositions)

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

	// Commit to git — write docs/demo/index.html and commit
	gitPath, gitCommit, err := commitDemoToGit(htmlContent)
	if err != nil {
		// Git publish is best-effort — log but don't fail if IPFS succeeded
		if resp.CID == "" {
			h.writeError(w, errors.Wrap(err, "failed to publish demo canvas to git"), http.StatusInternalServerError)
			return
		}
		// IPFS succeeded, git failed — include error info but still 200
	} else {
		resp.GitPath = gitPath
		resp.GitCommit = gitCommit
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// commitDemoToGit writes the rendered HTML to docs/demo/index.html,
// commits, and returns the relative path and commit hash.
func commitDemoToGit(htmlContent string) (string, string, error) {
	// Find git root
	gitRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", "", errors.Wrap(err, "not in a git repository")
	}
	root := filepath.Clean(string(gitRoot[:len(gitRoot)-1])) // trim newline

	demoDir := filepath.Join(root, "docs", "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		return "", "", errors.Wrapf(err, "failed to create %s", demoDir)
	}

	demoPath := filepath.Join(demoDir, "index.html")
	if err := os.WriteFile(demoPath, []byte(htmlContent), 0644); err != nil {
		return "", "", errors.Wrapf(err, "failed to write %s", demoPath)
	}

	relPath := "docs/demo/index.html"

	// Stage the file
	addCmd := exec.Command("git", "add", relPath)
	addCmd.Dir = root
	if out, err := addCmd.CombinedOutput(); err != nil {
		return "", "", errors.Wrapf(err, "git add failed: %s", string(out))
	}

	// Check if there are staged changes
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet", relPath)
	diffCmd.Dir = root
	if err := diffCmd.Run(); err == nil {
		// No changes — file is identical to HEAD
		// Return current HEAD hash
		headCmd := exec.Command("git", "rev-parse", "HEAD")
		headCmd.Dir = root
		headOut, _ := headCmd.Output()
		return relPath, string(headOut[:len(headOut)-1]), nil
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", "Publish demo canvas")
	commitCmd.Dir = root
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return "", "", errors.Wrapf(err, "git commit failed: %s", string(out))
	}

	// Get commit hash
	hashCmd := exec.Command("git", "rev-parse", "HEAD")
	hashCmd.Dir = root
	hashOut, err := hashCmd.Output()
	if err != nil {
		return relPath, "", errors.Wrap(err, "failed to get commit hash")
	}

	return relPath, string(hashOut[:len(hashOut)-1]), nil
}
