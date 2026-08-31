package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

// beaconTokens resolves one raw value to one grant, the way the door does.
type beaconTokens struct {
	hash  string
	grant auth.Grant
}

func (b beaconTokens) Lookup(hash string) (auth.Grant, bool) {
	if hash == b.hash {
		return b.grant, true
	}
	return auth.Grant{}, false
}
func (beaconTokens) Create(auth.NewToken) (string, string, error) { return "", "", nil }
func (beaconTokens) List() ([]auth.TokenInfo, error)              { return nil, nil }
func (beaconTokens) Revoke(string) error                          { return nil }
func (beaconTokens) Enable(string) error                          { return nil }
func (beaconTokens) SetScope(string, []string, []string) error    { return nil }

func beaconServer(t *testing.T, grant auth.Grant) (*QNTXServer, ats.AttestationStore) {
	t.Helper()
	store, db := createTestStore(t)
	return &QNTXServer{
		db:       db,
		atsStore: store,
		logger:   zap.NewNop().Sugar(),
		tokens:   beaconTokens{hash: auth.HashToken("qntx_beacon_raw"), grant: grant},
	}, store
}

func arrive(s *QNTXServer, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.HandleBeacon(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func doorBeacon() auth.Grant {
	return auth.Grant{
		Label:      "door",
		Level:      auth.LevelBeacon,
		Namespaces: []string{auth.NamespaceDefault},
		ScopeWrite: []string{"card:scanned"},
	}
}

func scanned(t *testing.T, store ats.AttestationStore, subject string) []*types.As {
	t.Helper()
	found, err := store.GetAttestations(ats.AttestationFilter{Subjects: []string{subject}, Limit: 10})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	return found
}

// An arrival through the door lands in the store: the beacon's predicate, the
// subject in its vocabulary, the actor forced to the beacon itself.
func TestABeaconArrivalIsRecorded(t *testing.T) {
	s, store := beaconServer(t, doorBeacon())

	rec := arrive(s, "/beacon/qntx_beacon_raw.gif?subject=TIMDEV000001&schema=1")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("the pixel did not come back: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	got := scanned(t, store, "card:TIMDEV000001")
	if len(got) != 1 {
		t.Fatalf("stored %d attestations, want 1", len(got))
	}
	as := got[0]
	if as.Predicates[0] != "card:scanned" || as.Source != "beacon" {
		t.Fatalf("stored %v from %q", as.Predicates, as.Source)
	}
	if len(as.Actors) != 1 || as.Actors[0] != "beacon:door" {
		t.Fatalf("the actor is %v, not the beacon", as.Actors)
	}
	if as.Attributes["schema"] != "1" {
		t.Fatalf("the schema attribute did not survive: %v", as.Attributes)
	}
	if _, leaked := as.Attributes["subject"]; leaked {
		t.Fatal("the subject parameter doubled as an attribute")
	}
}

// Every refusal answers with the same pixel the arrival gets — probing the
// door teaches nothing — and nothing lands in the store.
func TestARefusedCallerGetsTheSamePixelAndNoRecord(t *testing.T) {
	s, store := beaconServer(t, doorBeacon())

	for _, path := range []string{
		"/beacon/unknown_raw.gif?subject=TIMDEV000001",   // no live beacon
		"/beacon/qntx_beacon_raw.gif",                    // no subject
		"/beacon/qntx_beacon_raw.gif?subject=..%2fetc",   // a subject that is not an identifier
		"/beacon/qntx_beacon_raw.gif?subject=",           // an empty one
	} {
		rec := arrive(s, path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
			t.Fatalf("%s: answered %d %s rather than the pixel",
				path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if got := scanned(t, store, "card:TIMDEV000001"); len(got) != 0 {
		t.Fatalf("a refused arrival was recorded: %v", got)
	}
}

// A bearer token pasted into the beacon path never worked: the door serves
// beacons and nothing else, however live the credential is.
func TestABearerTokenIsNotABeacon(t *testing.T) {
	attestor := doorBeacon()
	attestor.Level = auth.LevelAttestor
	s, store := beaconServer(t, attestor)

	arrive(s, "/beacon/qntx_beacon_raw.gif?subject=TIMDEV000001")

	if got := scanned(t, store, "card:TIMDEV000001"); len(got) != 0 {
		t.Fatalf("an ATTESTOR wrote through the beacon door: %v", got)
	}
}
