//go:build cgo && !quickdev

package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"go.uber.org/zap"
)

// rustWritingServer is writingServer over the Rust store, the one every door
// writes through: what it refuses is what the node refuses.
func rustWritingServer(t *testing.T) (*QNTXServer, ats.AttestationStore) {
	t.Helper()
	s, _ := writingServer(t)
	store, err := storage.NewStore(":memory:", zap.NewNop().Sugar(), "")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if closer, ok := store.(io.Closer); ok {
		t.Cleanup(func() { closer.Close() })
	}
	s.held = servingOne(s.nodeDB, store)
	return s, store
}

func names(n int) string {
	quoted := make([]string, n)
	for i := range quoted {
		quoted[i] = `"c` + strings.Repeat("x", i%7) + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// What the store refuses comes back to the writer as theirs to fix, and
// nothing is written.
func TestWhatTheStoreRefusesIsABadRequest(t *testing.T) {
	for name, body := range map[string]string{
		"a hundred and one contexts": `{"id":"AS-WIDE","subjects":["qntx"],"predicates":["noted"],"contexts":` + names(101) + `}`,
		"a context past 1000 bytes":  `{"id":"AS-LONG","subjects":["qntx"],"predicates":["noted"],"contexts":["` + strings.Repeat("x", 1001) + `"]}`,
		"no subject":                 `{"id":"AS-NONE","subjects":[],"predicates":["noted"]}`,
		"no predicate":               `{"id":"AS-NONE","subjects":["qntx"],"predicates":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, store := rustWritingServer(t)
			rec := postAs(s, tokenAdmission(), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: 1})
			if err != nil || len(found) != 0 {
				t.Fatalf("read back %v, %v: a refused attestation was written", found, err)
			}
		})
	}
}

func TestAHundredContextsAreWritten(t *testing.T) {
	s, _ := rustWritingServer(t)
	rec := postAs(s, tokenAdmission(), `{"id":"AS-FULL","subjects":["qntx"],"predicates":["noted"],"contexts":`+names(100)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

// An id the node would generate is seeded from a subject and a predicate.
func TestNoSubjectToSeedAnIDFromIsABadRequest(t *testing.T) {
	s, _ := rustWritingServer(t)
	rec := postAs(s, tokenAdmission(), `{"subjects":[],"predicates":["noted"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// An id the store already holds is refused as that, not as the store failing,
// so the handler answers exists instead of 500.
func TestAnIDTheStoreHoldsIsRefusedAsAlreadyExisting(t *testing.T) {
	_, store := rustWritingServer(t)
	as := &types.As{ID: "AS-TWICE", Subjects: []string{"qntx"}, Predicates: []string{"noted"}, Source: "test"}
	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("first write: %v", err)
	}
	err := store.CreateAttestation(as)
	if !ats.AlreadyExists(err) {
		t.Fatalf("err = %v, want the store's already-exists refusal", err)
	}
	if ats.Unwritable(err) {
		t.Fatalf("err = %v read as unwritable", err)
	}
}

// A plugin writes through the store with no HTTP in front of it, and is
// refused the same, as a refusal and not as the store failing.
func TestAPluginIsRefusedWhatHTTPIsRefused(t *testing.T) {
	_, store := rustWritingServer(t)
	err := store.CreateAttestation(&types.As{
		ID:         "AS-PLUGIN",
		Subjects:   []string{"qntx"},
		Predicates: []string{"noted"},
		Actors:     strings.Split(strings.Repeat("a,", 101), ",")[:101],
		Source:     "plugin",
	})
	if !ats.Unwritable(err) {
		t.Fatalf("err = %v, want the store's refusal", err)
	}
	if !strings.Contains(err.Error(), "actors names 101") {
		t.Fatalf("err = %v, want it to name the field and the count", err)
	}
}
