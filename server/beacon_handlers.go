package server

// The beacon door (ADR-034): a public receive endpoint. A beacon is a token
// whose transport is the URL — GET /beacon/{raw}.gif records the arrival as
// an attestation in the beacon's namespace, with its one predicate, and
// answers with a 1×1 pixel.
//
// Every caller gets the same pixel and the same status: valid arrival,
// unknown beacon, revoked beacon, unusable subject. A refused caller is told
// nothing, and the difference lives in the store and the log.

import (
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

// A 1×1 transparent GIF. The whole point of answering with an image is that
// an <img> tag carries the beacon with no CORS, no preflight and no script.
var beaconPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// HandleBeacon answers GET /beacon/{raw}.gif.
//
// Query: `subject` carries the local part of what arrived — the beacon
// prepends its predicate's vocabulary, so `?subject=TIMDEV000001` under a
// `card:scanned` beacon records `card:TIMDEV000001`. Every other parameter
// becomes an attribute, capped by auth.BeaconAttributes.
func (s *QNTXServer) HandleBeacon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The pixel goes out whatever happened: probing the door teaches nothing.
	defer func() {
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(beaconPixel)
		}
	}()

	raw, ok := strings.CutPrefix(r.URL.Path, auth.BeaconPathPrefix)
	if !ok {
		return
	}
	raw, ok = strings.CutSuffix(raw, auth.BeaconPathSuffix)
	if !ok || raw == "" || strings.Contains(raw, "/") {
		return
	}

	if s.tokens == nil {
		s.logger.Debugw("Beacon arrival with no token store to resolve it")
		return
	}
	grant, live := s.tokens.Lookup(auth.HashToken(raw))
	if !live || grant.Level != auth.LevelBeacon {
		// A revoked or unknown beacon, or a bearer token pasted into a URL —
		// which never worked, so a leaked URL never doubles as a credential.
		s.logger.Infow("Beacon arrival refused",
			"reason", "no live beacon answers on this path", "client", r.RemoteAddr)
		return
	}
	if len(grant.ScopeWrite) != 1 {
		// Minting refuses this shape, so a record like it did not come from
		// the mint. Say which beacon rather than write under a guessed predicate.
		s.logger.Errorw("Beacon record is malformed",
			"label", grant.Label, "scope_write", grant.ScopeWrite)
		return
	}
	predicate := grant.ScopeWrite[0]

	subject := auth.BeaconSubject(predicate, r.URL.Query().Get("subject"))
	if subject == "" {
		s.logger.Infow("Beacon arrival refused",
			"label", grant.Label, "reason", "no usable subject", "client", r.RemoteAddr)
		return
	}

	namespace := auth.NamespaceDefault
	if len(grant.Namespaces) == 1 {
		namespace = grant.Namespaces[0]
	}
	store, err := s.storeIn(namespace)
	if err != nil {
		s.logger.Errorw("Beacon arrival lost: its namespace is not served",
			"label", grant.Label, "namespace", namespace, "error", err)
		return
	}

	// The actor is forced: everything through this door is a claim by the
	// beacon, never by whoever loaded the page.
	actor := "beacon:" + grant.Label
	if grant.Label == "" {
		actor = "beacon:" + grant.DID
	}

	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, "_", store.AttestationExists)
	if err != nil {
		s.logger.Errorw("Beacon arrival lost: no ASID for it",
			"label", grant.Label, "subject", subject, "error", err)
		return
	}
	as := &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Actors:     []string{actor},
		Timestamp:  time.Now(),
		Source:     "beacon",
		Attributes: auth.BeaconAttributes(r.URL.Query()),
		CreatedAt:  time.Now(),
	}
	if err := store.CreateAttestation(as); err != nil {
		s.logger.Errorw("Beacon arrival lost: the store did not take it",
			"label", grant.Label, "subject", subject, "namespace", namespace, "error", err)
		return
	}
	s.logger.Infow("Beacon arrival recorded",
		"label", grant.Label, "subject", subject, "predicate", predicate,
		"namespace", namespace, "id", id)
}
