package server

import (
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// A node that cannot write an attestation is not a node.

// QNTX exists to hold attestations. A process that comes up, answers /health
// with ok, and refuses every write is worse than one that does not come up:
// callers keep calling and the record they are owed is never made.

// Proven by writing the record of the node starting, which is a fact worth
// keeping on its own rather than a probe pretending to be one.
const PredicateStarted = "node:started"

type storeProofSubsystem struct{}

func (storeProofSubsystem) Name() string { return "store-proof" }

func (storeProofSubsystem) Init(s *QNTXServer) error {
	store := s.systemAttestor()
	if store == nil {
		return errors.New("no attestation store, so nothing can be written down")
	}

	// The node talking about itself, in the namespace that is nobody's.
	subject := "did:key:unknown"
	if s.nodeDID != nil {
		subject = s.nodeDID.DID
	}

	id, err := identity.GenerateASUID("AS", subject, PredicateStarted, auth.NamespaceSystem)
	if err != nil {
		return errors.Wrapf(err, "failed to mint an id for the %s attestation", PredicateStarted)
	}

	build := version.Get()
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{PredicateStarted},
		Contexts:   []string{auth.NamespaceSystem},
		Actors:     []string{subject},
		Timestamp:  now,
		Source:     subject,
		Attributes: map[string]any{
			"commit":     build.CommitHash,
			"version":    build.Version,
			"build_time": build.BuildTime,
		},
		CreatedAt: now,
	}); err != nil {
		return errors.Wrapf(err,
			"the attestation store refused the %s record, so this node cannot keep what it is for",
			PredicateStarted)
	}

	s.logger.Infow("Attestation store proven", "attestation", id, "subject", subject)
	return nil
}
