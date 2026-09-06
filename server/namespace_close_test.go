package server

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// fakeOpener hands out a store per name and records which names were closed,
// so eviction can be told from a handle left open.
type fakeOpener struct {
	// stores is what each name was opened as, so a test can read back what a
	// drain wrote rather than only that it said it wrote something.
	stores map[string]*countingStore
	opened []string
	closed []string
	err    error
}

func (f *fakeOpener) OpenNamespace(name string) (ats.AttestationStore, error) {
	f.opened = append(f.opened, name)
	if f.stores == nil {
		f.stores = map[string]*countingStore{}
	}
	if held, open := f.stores[name]; open {
		return held, f.err
	}
	held := &countingStore{}
	f.stores[name] = held
	return held, f.err
}

func (f *fakeOpener) CloseNamespace(name string) error {
	f.closed = append(f.closed, name)
	return f.err
}

// countingStore is an attestation store that holds what it was given, so a
// drain can be read back and a count is what was actually written.
type countingStore struct {
	held []*types.As
}

func (c *countingStore) CreateAttestation(as *types.As) error {
	c.held = append(c.held, as)
	return nil
}

func (c *countingStore) CreateAttestationInbound(as *types.As) error {
	return c.CreateAttestation(as)
}

func (c *countingStore) AttestationExists(asid string) bool {
	for _, as := range c.held {
		if as.ID == asid {
			return true
		}
	}
	return false
}

func (c *countingStore) GenerateAndCreateAttestation(context.Context, *types.AsCommand) (*types.As, error) {
	return nil, errors.New("this store is written into by hand, so nothing generates through it")
}

func (c *countingStore) GetAttestations(ats.AttestationFilter) ([]*types.As, error) {
	return c.held, nil
}

func closingServer(t *testing.T, namespaces storage.Namespaces, opener NamespaceOpener) *QNTXServer {
	t.Helper()
	return &QNTXServer{
		namespaces:      namespaces,
		namespaceOpener: opener,
		logger:          zap.NewNop().Sugar(),
	}
}

// storeIn answers from the cache before it asks whether the namespace is still
// there, so a handle nothing evicts keeps serving one that is gone.
func TestClosingANamespaceTakesTheHandleOutOfTheCache(t *testing.T) {
	opener := &fakeOpener{}
	s := closingServer(t, heldNamespaces{names: []string{"pond"}}, opener)

	if _, err := s.storeIn("pond"); err != nil {
		t.Fatalf("pond would not open: %v", err)
	}
	if len(opener.opened) != 1 {
		t.Fatalf("opened %v, want pond once", opener.opened)
	}

	// A second ask comes from the cache; the opener is not asked again.
	if _, err := s.storeIn("pond"); err != nil {
		t.Fatalf("pond would not open a second time: %v", err)
	}
	if len(opener.opened) != 1 {
		t.Fatalf("opened %v, want the cache to have answered", opener.opened)
	}

	if err := s.closeIn("pond"); err != nil {
		t.Fatalf("closing pond failed: %v", err)
	}
	if _, held := s.stores.open["pond"]; held {
		t.Error("the handle is still in the cache after closing")
	}
}

// Evicting alone is not enough: the flush loop holds its own reference and
// writes the prefix back on the next tick, so the opener has to be told.
func TestClosingANamespaceStopsTheFlushBehindIt(t *testing.T) {
	opener := &fakeOpener{}
	s := closingServer(t, heldNamespaces{names: []string{"pond"}}, opener)

	if _, err := s.storeIn("pond"); err != nil {
		t.Fatalf("pond would not open: %v", err)
	}
	if err := s.closeIn("pond"); err != nil {
		t.Fatalf("closing pond failed: %v", err)
	}

	if len(opener.closed) != 1 || opener.closed[0] != "pond" {
		t.Errorf("closed %v, want pond — the flush loop was left running", opener.closed)
	}
}

// A door is keyed by slug and a namespace keeps the name it was created with,
// so the handle comes off under the slug it was filed under.
func TestClosingReachesTheHandleBySlug(t *testing.T) {
	opener := &fakeOpener{}
	s := closingServer(t, heldNamespaces{names: []string{"Clean"}}, opener)

	if _, err := s.storeIn("clean"); err != nil {
		t.Fatalf("clean would not open: %v", err)
	}
	if err := s.closeIn("Clean"); err != nil {
		t.Fatalf("closing Clean failed: %v", err)
	}
	if len(s.stores.open) != 0 {
		t.Errorf("the cache still holds %v", s.stores.open)
	}
}

// Nothing is holding a namespace nobody opened, and saying so is not an error.
func TestClosingANamespaceNobodyOpenedSaysNothing(t *testing.T) {
	opener := &fakeOpener{}
	s := closingServer(t, heldNamespaces{}, opener)

	if err := s.closeIn("pond"); err != nil {
		t.Fatalf("closing a namespace nobody opened failed: %v", err)
	}
	if len(opener.closed) != 0 {
		t.Errorf("the opener was told to close %v", opener.closed)
	}
}
