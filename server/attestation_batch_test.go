//go:build cgo && !quickdev

// The batch is answered by the Rust store, which is what refuses an
// attestation with no subject.

package server

import (
	"encoding/json"
	"github.com/teranos/QNTX/ats/types"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
)

// "what i want is to move from 30 per minute to 1 per 30 sec"
// "but batch them"
func TestABatchIsAnsweredOnePerAttestation(t *testing.T) {
	s, store := rustWritingServer(t)

	body := `[` +
		`{"id":"ground:one","subjects":["qntx"],"predicates":["noted"],"actors":["ground"]},` +
		`{"id":"ground:two","subjects":[],"predicates":["noted"]},` +
		`{"id":"ground:three","subjects":["qntx"],"predicates":["noted"],"actors":["ground"]}` +
		`]`
	req := rootRequest(httptest.NewRequest(http.MethodPost, "/api/attestations", jsonBody(body)))
	rec := httptest.NewRecorder()
	s.handleCreateAttestation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for the batch: %s", rec.Code, rec.Body.String())
	}
	var said struct {
		Results []struct {
			Status int             `json:"status"`
			Answer json.RawMessage `json:"answer"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &said); err != nil {
		t.Fatalf("the batch answer did not parse: %v: %s", err, rec.Body.String())
	}

	// Each one answered as a single POST of it would have been, in the order
	// sent, so the sender knows which landed and which were refused.
	want := []int{http.StatusCreated, http.StatusBadRequest, http.StatusCreated}
	if len(said.Results) != len(want) {
		t.Fatalf("results = %d, want %d: %s", len(said.Results), len(want), rec.Body.String())
	}
	for i, status := range want {
		if said.Results[i].Status != status {
			t.Fatalf("result %d status = %d, want %d: %s", i, said.Results[i].Status, status, said.Results[i].Answer)
		}
	}

	found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: 10})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("stored = %d, want the two that were whole", len(found))
	}

	// Sent again, each one that exists says so, as a single POST does.
	again := httptest.NewRecorder()
	s.handleCreateAttestation(again, rootRequest(httptest.NewRequest(http.MethodPost, "/api/attestations", jsonBody(body))))
	if err := json.Unmarshal(again.Body.Bytes(), &said); err != nil {
		t.Fatalf("the second answer did not parse: %v", err)
	}
	if said.Results[0].Status != http.StatusOK || said.Results[2].Status != http.StatusOK {
		t.Fatalf("a resent attestation = %d and %d, want 200 exists", said.Results[0].Status, said.Results[2].Status)
	}
}

// A body that is not a list is the single POST it always was.
func TestASingleAttestationIsUnchanged(t *testing.T) {
	root := auth.Admitted(auth.LevelRoot)
	_, rec := writingAs(t, &root, `{"subjects":["qntx"],"predicates":["noted"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

// countingMany is a store that counts the writes of many it is asked for.
type countingMany struct {
	ats.AttestationStore
	sizes []int
}

func (c *countingMany) CreateAttestations(attestations []*types.As) ([]error, error) {
	c.sizes = append(c.sizes, len(attestations))
	return c.AttestationStore.(storage.ManyCreator).CreateAttestations(attestations)
}

// "if k took the list in memory and wrote it in one transaction, the drain would go far faster"
//
// A list is written in one write of the store, and each is still answered as
// a single POST of it would be: the second of two by one id says it exists.
func TestABatchIsOneWriteOfTheStore(t *testing.T) {
	s, store := rustWritingServer(t)
	counted := &countingMany{AttestationStore: store}
	s.held = servingOne(s.nodeDB, counted)

	body := `[` +
		`{"id":"ground:one","subjects":["qntx"],"predicates":["noted"],"actors":["ground"]},` +
		`{"id":"ground:two","subjects":["qntx"],"predicates":["noted"],"actors":["ground"]},` +
		`{"id":"ground:one","subjects":["qntx"],"predicates":["noted"],"actors":["ground"]},` +
		`{"id":"ground:three","subjects":["qntx"],"predicates":["noted"],"actors":["ground"]}` +
		`]`
	rec := httptest.NewRecorder()
	s.handleCreateAttestation(rec, rootRequest(httptest.NewRequest(http.MethodPost, "/api/attestations", jsonBody(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for the batch: %s", rec.Code, rec.Body.String())
	}
	var said struct {
		Results []struct {
			Status int             `json:"status"`
			Answer json.RawMessage `json:"answer"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &said); err != nil {
		t.Fatalf("the batch answer did not parse: %v: %s", err, rec.Body.String())
	}
	want := []int{http.StatusCreated, http.StatusCreated, http.StatusOK, http.StatusCreated}
	if len(said.Results) != len(want) {
		t.Fatalf("results = %d, want %d: %s", len(said.Results), len(want), rec.Body.String())
	}
	for i, status := range want {
		if said.Results[i].Status != status {
			t.Fatalf("result %d status = %d, want %d: %s", i, said.Results[i].Status, status, said.Results[i].Answer)
		}
	}
	if len(counted.sizes) != 1 || counted.sizes[0] != 4 {
		t.Fatalf("writes of many = %v, want one of all four", counted.sizes)
	}
	found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: 10})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	if len(found) != 3 {
		t.Fatalf("stored = %d, want three", len(found))
	}
}
