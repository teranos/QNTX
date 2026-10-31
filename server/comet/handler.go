package comet

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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
		// "A comet held for the harness is checked there."
		if n.ABI == "" {
			h.Refuse(w, http.StatusNotFound, "/comet/"+key+" names no engine ABI: a sand is read as /comet/"+key+"/{abi}, the ABI of the ground that loads it")
			return
		}
		path, err := Path(n)
		if err != nil {
			h.Refuse(w, http.StatusInternalServerError, err.Error())
			return
		}
		digest, err := Digest(path)
		if errors.Is(err, os.ErrNotExist) {
			said := "no comet of " + key + " is kept: POST /comet/" + n.Base() + " builds it"
			if abis := keptABIs(n); len(abis) > 0 {
				said = "no comet of " + key + " is kept: " + n.Base() + " is kept for engine ABI " + strings.Join(abis, ", ") + ", built from ground's main"
			}
			if last, building := builds.Load(n.Base()); building {
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
		if n.ABI != "" {
			h.Refuse(w, http.StatusUnprocessableEntity, "POST /comet/"+key+" names an ABI: a build makes the sand of ground's main, whatever ABI it is, at POST /comet/"+n.Base())
			return
		}
		// "A person configures controls; the model is on the enforced side of them"
		machine, err := MachineOf(r.URL.Query().Get("machine"))
		if err != nil {
			h.Refuse(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		n.Machine = machine
		if err := h.Start(n); err != nil {
			h.Refuse(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.Respond(w, http.StatusAccepted, map[string]string{"building": key})
		return
	}
	h.Refuse(w, http.StatusMethodNotAllowed, r.Method+" /comet/"+key+": a comet is read with GET, its digest with HEAD or ?digest, and built with POST")
}

// keptABIs is each engine ABI a sand of n's repository and platform is kept for.
func keptABIs(n Name) []string {
	dir, err := Dir()
	if err != nil {
		return nil
	}
	found, err := filepath.Glob(filepath.Join(dir, n.Owner, n.Repo, n.Platform, "*", SandFile(n.Platform)))
	if err != nil {
		return nil
	}
	abis := []string{}
	for _, path := range found {
		abis = append(abis, filepath.Base(filepath.Dir(path)))
	}
	return abis
}

// Start builds n apart from whoever asked, and keeps what the build came to for GET to say.
// A sand imports no symbol, so any node builds it for every platform ground's flake names.
func (h Handler) Start(n Name) error {
	if _, err := Native(); err != nil {
		return err
	}
	key := n.Base()
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
