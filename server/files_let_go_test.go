package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/teranos/QNTX/ats/types"
)

// hasPredicate is whether one of the lines says predicate.
func hasPredicate(lines []*types.As, predicate string) bool {
	return slices.ContainsFunc(lines, func(line *types.As) bool { return slices.Contains(line.Predicates, predicate) })
}

// "i want it to be directly accesible"
// 「[Opus 5.5] A file is fetched by the name its bytes have; a name this node holds nothing of is not found.」
func TestAFileIsFetchedByItsName(t *testing.T) {
	srv := createFileTestServer(t)
	kept, err := srv.KeepFileForPlugin("ipluk", "shot.jpeg", screenshot, "")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	w := httptest.NewRecorder()
	srv.HandleFiles(w, httptest.NewRequest(http.MethodGet, "/api/files/"+kept.Asset, nil))
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), screenshot) {
		t.Fatalf("GET by its name answered %d with %d bytes, want the %d it was given", w.Code, w.Body.Len(), len(screenshot))
	}
	w = httptest.NewRecorder()
	srv.HandleFiles(w, httptest.NewRequest(http.MethodGet, "/api/files/sha256:"+"00000000000000000000000000000000000000000000000000000000000000ff", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("a name held nowhere answered %d, want 404", w.Code)
	}
}

// "to me it feels like k should own my Media Assets"
// 「[Opus 5.5] Set to, a node lets go of its copy once the peer holds the same bytes, and says」
// 「[Opus 5.5] so; not set to, every copy stays.」
func TestANodeLetsGoOfWhatThePeerHolds(t *testing.T) {
	srv := createFileTestServer(t)
	if srv.deps == nil || srv.deps.cfg == nil {
		t.Fatal("this server is made without its config")
	}
	kept, err := srv.KeepFileForPlugin("ipluk", "shot.jpeg", screenshot, "k")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	waiting, err := srv.stillToBring()
	if err != nil || len(waiting) != 1 {
		t.Fatalf("%d wait (%v), want 1", len(waiting), err)
	}

	srv.deps.cfg.Files.LetGoOnceBrought = false
	srv.letGo(waiting[0], "the-id-at-k")
	if _, here, _ := srv.storedFile(kept.ID); !here {
		t.Fatal("a node not set to let go let go")
	}

	srv.deps.cfg.Files.LetGoOnceBrought = true
	srv.letGo(waiting[0], "the-id-at-k")
	if _, here, _ := srv.storedFile(kept.ID); here {
		t.Error("the copy is still here")
	}
	if said := saidOf(t, srv, kept.Asset); !hasPredicate(said, assetLetGo) {
		t.Errorf("no %s line was written of %s", assetLetGo, kept.Asset)
	}
}
