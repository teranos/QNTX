package comet

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// Handler serves a project's comet (GET), its digest (GET ?digest, or HEAD), or builds it (POST).
// Refuse and Respond answer in the node's own JSON shape; Ctx is the node's lifetime, which a build runs in.
type Handler struct {
	Builder Builder
	Ctx     context.Context
	Refuse  func(w http.ResponseWriter, status int, message string)
	Respond func(w http.ResponseWriter, status int, data any)
	Log     func(message string, keysAndValues ...any)
}

// builds is what each comet's last build came to, by its key, until one is kept: building
// since when, or why it failed. A comet kept has no entry.
var builds sync.Map

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n, err := Of(r.URL.Path)
	if err != nil {
		h.Refuse(w, http.StatusNotFound, err.Error())
		return
	}
	key := n.Key()
	// HEAD is ground asking whether its comet moved: the digest, and none of the bytes.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		path, err := Path(n)
		if err != nil {
			h.Refuse(w, http.StatusInternalServerError, err.Error())
			return
		}
		digest, err := Digest(path)
		if errors.Is(err, os.ErrNotExist) {
			said := "no comet of " + key + " is kept: POST /comet/" + key + " builds it"
			if last, building := builds.Load(key); building {
				said = "no comet of " + key + " is kept: " + last.(string)
			}
			h.Refuse(w, http.StatusNotFound, said)
			return
		}
		if err != nil {
			h.Refuse(w, http.StatusInternalServerError, err.Error())
			return
		}
		if r.URL.Query().Has("digest") {
			h.Respond(w, http.StatusOK, map[string]string{"digest": digest})
			return
		}
		w.Header().Set("X-Comet-Digest", digest)
		http.ServeFile(w, r, path)
		return
	}
	if r.Method == http.MethodPost {
		if err := h.Start(n); err != nil {
			h.Refuse(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.Respond(w, http.StatusAccepted, map[string]string{"building": key})
		return
	}
	h.Refuse(w, http.StatusMethodNotAllowed, r.Method+" /comet/"+key+": a comet is read with GET, its digest with HEAD or ?digest, and built with POST")
}

// Start builds n apart from whoever asked, and keeps what the build came to for GET to say.
// A platform this node neither builds natively nor holds the stubs to link is refused, and why.
func (h Handler) Start(n Name) error {
	native, err := Native()
	if err != nil {
		return err
	}
	// A Darwin comet built on the box links Apple's stubs (ADR-050, The stubs); another Linux
	// links what the binary cache holds for it, on a Linux node.
	_, linux := linuxTargets[n.Platform]
	if linux && n.Platform != native && !strings.HasSuffix(native, "-linux") {
		return errors.Newf("this node builds %s natively, and links another Linux only from a Linux", native)
	}
	if _, darwin := darwinTargets[n.Platform]; darwin && n.Platform != native {
		stubs, err := Stubs(n.Platform)
		if err != nil {
			return errors.Wrapf(err, "this node builds %s natively, and a comet for %s links Apple's stubs", native, n.Platform)
		}
		h.Log("A Darwin comet links the stubs the node holds", "comet", n.Key(), "sdk", stubs.SDK, "libgit2", stubs.Git2)
	}
	key := n.Key()
	builds.Store(key, "building since "+time.Now().UTC().Format(time.RFC3339))
	sacred.Go("comet.build."+key, func() {
		if err := h.Builder.Build(h.Ctx, n); err != nil {
			builds.Store(key, "its last build failed: "+err.Error())
			return
		}
		builds.Delete(key)
	})
	return nil
}
