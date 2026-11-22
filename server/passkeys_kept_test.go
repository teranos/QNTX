//go:build cgo && !quickdev

package server

import (
	"io"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"go.uber.org/zap"
)

// "QNTX can be the thing that holds stuff durably"
//
// Each record laye keeps is held by the node once, and a record laye writes
// again is held again: the node keeps its history.
func TestTheNodeKeepsWhatLayeHoldsOfPasskeys(t *testing.T) {
	store, err := storage.NewStore(":memory:", zap.NewNop().Sugar(), "")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if closer, ok := store.(io.Closer); ok {
		t.Cleanup(func() { closer.Close() })
	}
	node := "did:key:z6MkqntxNodeStandsHere"
	held := map[string][]byte{
		"cred-a": []byte(`{"person":"github:1739143","level":"ROOT","sign_count":0}`),
		"cred-b": []byte(`{"person":"did:key:z6Mkb","level":"ROOT","sign_count":0}`),
	}

	kept, err := keepPasskeys(held, store, node, time.Now())
	if err != nil || kept != 2 {
		t.Fatalf("kept %d, %v; want both records kept", kept, err)
	}
	kept, err = keepPasskeys(held, store, node, time.Now())
	if err != nil || kept != 0 {
		t.Fatalf("kept %d again, %v; a record the node holds is not kept twice", kept, err)
	}

	held["cred-a"] = []byte(`{"person":"github:1739143","level":"ROOT","sign_count":1}`)
	kept, err = keepPasskeys(held, store, node, time.Now())
	if err != nil || kept != 1 {
		t.Fatalf("kept %d, %v; a record laye wrote again is kept again", kept, err)
	}

	history, err := store.GetAttestations(ats.AttestationFilter{
		TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{passkeySubject("cred-a")}, Predicates: []string{passkeyKept}, Limit: ats.EveryRow,
	})
	if err != nil {
		t.Fatalf("reading what the node keeps of cred-a: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("the node holds %d records of cred-a, want 2", len(history))
	}
	for _, as := range history {
		if as.Attributes["person"] != "github:1739143" || as.Attributes["etcd_key"] != "/passkeys/cred-a" {
			t.Fatalf("a kept record says %v", as.Attributes)
		}
		if len(as.Actors) != 1 || as.Actors[0] != node {
			t.Fatalf("a kept record is by %v, want the node", as.Actors)
		}
	}

	if _, err := keepPasskeys(map[string][]byte{"cred-c": []byte("not json")}, store, node, time.Now()); err == nil {
		t.Fatal("a record that is not JSON was kept without a word")
	}
}
