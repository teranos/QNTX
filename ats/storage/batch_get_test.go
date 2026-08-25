package storage

import (
	"errors"
	"testing"

	"github.com/teranos/QNTX/ats/types"
)

var errRead = errors.New("backend could not be read")

// The server asserts the batch interface on the store it holds, which is
// *AtsStore and not the raw backend. Adding the method to the backend alone
// left the assertion permanently false and the batch path unreachable in
// production. This line is what names that relationship.
var _ BatchGetStore = (*AtsStore)(nil)

type stubRaw struct {
	held  map[string]*types.As
	gets  int
	fails error
}

func (s *stubRaw) CreateAttestation(as *types.As) error { return nil }
func (s *stubRaw) AttestationExists(id string) bool     { _, ok := s.held[id]; return ok }
func (s *stubRaw) CountAttestations() (int, error)      { return len(s.held), nil }

func (s *stubRaw) GetAttestation(id string) (*types.As, error) {
	s.gets++
	if s.fails != nil {
		return nil, s.fails
	}
	return s.held[id], nil
}

type stubBatchRaw struct {
	stubRaw
	batches int
}

func (s *stubBatchRaw) GetAttestationsByIDs(ids []string) ([]*types.As, error) {
	s.batches++
	out := make([]*types.As, 0, len(ids))
	for _, id := range ids {
		if as := s.held[id]; as != nil {
			out = append(out, as)
		}
	}
	return out, nil
}

func held() map[string]*types.As {
	return map[string]*types.As{
		"AS-1": {ID: "AS-1"},
		"AS-2": {ID: "AS-2"},
	}
}

func TestGetAttestationsByIDsDelegatesToBatchBackend(t *testing.T) {
	raw := &stubBatchRaw{stubRaw: stubRaw{held: held()}}
	store := NewAtsStore(raw, nil)

	got, err := store.GetAttestationsByIDs([]string{"AS-1", "AS-2", "AS-missing"})
	if err != nil {
		t.Fatalf("GetAttestationsByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d attestations, want 2", len(got))
	}
	if raw.batches != 1 {
		t.Errorf("batch backend called %d times, want 1", raw.batches)
	}
	if raw.gets != 0 {
		t.Errorf("fell through to %d single gets, want 0", raw.gets)
	}
}

func TestGetAttestationsByIDsReadsOneAtATimeWithoutBatchBackend(t *testing.T) {
	raw := &stubRaw{held: held()}
	store := NewAtsStore(raw, nil)

	got, err := store.GetAttestationsByIDs([]string{"AS-1", "AS-2", "AS-missing"})
	if err != nil {
		t.Fatalf("GetAttestationsByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d attestations, want 2", len(got))
	}
	if raw.gets != 3 {
		t.Errorf("read %d ids one at a time, want 3", raw.gets)
	}
}

// A backend that cannot be read is not a backend holding nothing. The loop
// swallowed this, which is how an unreachable batch path stayed silent.
func TestGetAttestationsByIDsReportsAReadFailure(t *testing.T) {
	raw := &stubRaw{held: held(), fails: errRead}
	store := NewAtsStore(raw, nil)

	if _, err := store.GetAttestationsByIDs([]string{"AS-1"}); err == nil {
		t.Fatal("a failed read came back as an empty result")
	}
}
