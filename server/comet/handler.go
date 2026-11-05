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
				said = "no comet of " + key + " is kept: " + n.Base() + " is kept for engine ABI " + strings.Join(abis, ", ") + ", built from ground's " + Ground.Branch
			}
			if last, building := builds.Load(n.Base()); building {
				said = "no comet of " + key + " is kept: " + last.(string)
			}
			// "1. the 2nd"
			if rev := r.URL.Query().Get("ground"); rev != "" {
				said = h.forEngine(n, rev, said)
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
			h.Refuse(w, http.StatusUnprocessableEntity, "POST /comet/"+key+" names an ABI: a build makes the sand of ground's "+Ground.Branch+", whatever ABI it is, at POST /comet/"+n.Base())
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
		if _, err := h.Builder.Build(h.Ctx, n); err != nil {
			builds.Store(key, "its last build failed: "+err.Error())
			return
		}
		builds.Delete(key)
	})
	return nil
}

// engineBuilds is what each build for an engine came to, by its comet and the ground it names:
// building since when, why it failed, or which ABI that ground builds. One build is started for
// each, whatever sky asks after it; a kept comet has no entry and is served.
var engineBuilds sync.Map

// forEngine is what an engine that names the ground it was built from is told when no comet of
// its ABI is kept, and the build of that comet from that ground, started once. A comet is built
// only for a repository and platform a person named a machine for: a comet kept beside it says
// which. Without one, said stands.
func (h Handler) forEngine(n Name, rev, said string) string {
	if !revision(rev) {
		return "no comet of " + n.Key() + " is kept, and ground " + rev + " is not built: it names " + rev + ", which is no revision"
	}
	key := n.Key() + "@" + rev
	if last, asked := engineBuilds.Load(key); asked {
		return "no comet of " + n.Key() + " is kept: ground " + rev + " " + last.(string)
	}
	machine, found, err := keptMachine(n)
	if err != nil {
		return "no comet of " + n.Key() + " is kept, and none is built from ground " + rev + ": " + err.Error()
	}
	if !found {
		return said
	}
	if _, err := Native(); err != nil {
		return "no comet of " + n.Key() + " is kept, and none is built from ground " + rev + ": " + err.Error()
	}
	n.Machine, n.GroundRev = machine, rev
	if _, raced := engineBuilds.LoadOrStore(key, "is building since "+time.Now().UTC().Format(time.RFC3339)); raced {
		last, _ := engineBuilds.Load(key)
		return "no comet of " + n.Key() + " is kept: ground " + rev + " " + last.(string)
	}
	h.Log("A comet is built from the ground an engine names", "comet", n.Key(), "ground", rev, "machine", machine)
	sacred.Go("comet.build."+key, func() {
		abi, err := h.Builder.Build(h.Ctx, n)
		if err != nil {
			engineBuilds.Store(key, "did not build: "+err.Error())
			return
		}
		if abi != n.ABI {
			engineBuilds.Store(key, "builds the sand of engine ABI "+abi+", not "+n.ABI+": the engine was not built from ground "+rev+" alone")
			return
		}
		engineBuilds.Delete(key)
	})
	return "no comet of " + n.Key() + " is kept: building it from ground " + rev
}

// revision is whether rev is a branch, tag or commit as git describe names one, and nothing that
// climbs, spaces or starts as a flag.
func revision(rev string) bool {
	if len(rev) == 0 || len(rev) > 64 || rev[0] == '-' || rev[0] == '.' || strings.Contains(rev, "..") {
		return false
	}
	for _, c := range rev {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// keptMachine is the machine a person named for n's repository and platform, as each comet kept
// for it says beside itself; found is false when none is kept.
func keptMachine(n Name) (string, bool, error) {
	dir, err := Dir()
	if err != nil {
		return "", false, err
	}
	found, err := filepath.Glob(filepath.Join(dir, n.Owner, n.Repo, n.Platform, "*", SandFile(n.Platform)+".machine"))
	if err != nil {
		return "", false, errors.Wrapf(err, "the machines of %s were not listed", n.Base())
	}
	machine := ""
	for _, path := range found {
		named, err := os.ReadFile(path)
		if err != nil {
			return "", false, errors.Wrapf(err, "the machine kept beside %s was not read", path)
		}
		this := strings.TrimSpace(string(named))
		if machine != "" && this != machine {
			return "", false, errors.Newf("%s is kept for machines %s and %s, so which one an engine's comet carries is not known", n.Base(), machine, this)
		}
		machine = this
	}
	return machine, machine != "", nil
}
