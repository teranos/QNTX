package storage

import (
	"context"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// RustBackedStore wraps the Rust FFI store with Go domain logic:
// signing (before write), observers (after write), and bounded enforcement (after write).
// Enforcement runs through Rust's single connection to avoid dual-driver SQLITE_CORRUPT.
type RustBackedStore struct {
	rust           *sqlitecgo.RustStore         // Attestation CRUD + enforcement via Rust FFI
	enforcementCfg *sqlitecgo.EnforcementConfig // Bounded storage limits (16/64/64 default)
	log            *zap.SugaredLogger
}

// CreateAttestation signs the attestation then delegates to Rust for INSERT.
func (s *RustBackedStore) CreateAttestation(as *types.As) error {
	// Sign if we have a signer and the attestation isn't already signed
	if signer := getDefaultSigner(); signer != nil {
		if err := signer.Sign(as); err != nil {
			return errors.Wrapf(err, "failed to sign attestation %s", as.ID)
		}
	}

	if err := s.rust.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "rust create attestation %s", as.ID)
	}

	notifyObservers(as)
	s.enforceLimitsViaRust(as)

	return nil
}

// CreateAttestationInbound inserts a synced attestation without signing (preserves provenance).
func (s *RustBackedStore) CreateAttestationInbound(as *types.As) error {
	if err := s.rust.CreateAttestationInbound(as); err != nil {
		return errors.Wrapf(err, "rust create inbound attestation %s", as.ID)
	}

	notifyObservers(as)
	s.enforceLimitsViaRust(as)

	return nil
}

// enforceLimitsViaRust delegates bounded enforcement to Rust's single connection.
func (s *RustBackedStore) enforceLimitsViaRust(as *types.As) {
	if as == nil {
		return
	}

	events, err := s.rust.EnforceLimits(as.Actors, as.Contexts, as.Subjects, s.enforcementCfg)
	if err != nil {
		if s.log != nil {
			s.log.Warnw("Rust enforcement failed", "error", err, "attestation", as.ID)
		}
		return
	}

	if s.log != nil && len(events) > 0 {
		for _, ev := range events {
			s.log.Debugw("Bounded storage limit enforced",
				"event_type", ev.EventType,
				"actor", ev.Actor,
				"context", ev.Context,
				"entity", ev.Entity,
				"deletions", ev.DeletedCount,
				"limit", ev.LimitValue,
			)
		}
	}
}

// AttestationExists checks if an attestation with the given ID exists.
func (s *RustBackedStore) AttestationExists(asid string) bool {
	return s.rust.AttestationExists(asid)
}

// GetAttestations retrieves attestations based on filters.
func (s *RustBackedStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return s.rust.GetAttestations(filters)
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation.
// Reimplemented here (rather than delegating to RustStore) so that CreateAttestation
// goes through this wrapper's signing/observers/bounded enforcement path.
func (s *RustBackedStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	checkExists := func(asid string) bool {
		return s.rust.AttestationExists(asid)
	}

	subject := "_"
	if len(cmd.Subjects) > 0 {
		subject = cmd.Subjects[0]
	}
	predicate := "_"
	if len(cmd.Predicates) > 0 {
		predicate = cmd.Predicates[0]
	}
	ctxStr := "_"
	if len(cmd.Contexts) > 0 {
		ctxStr = cmd.Contexts[0]
	}

	asid, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, ctxStr, checkExists)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate vanity ASID")
	}

	as := cmd.ToAs(asid, "")
	as.Actors = []string{asid}

	if err := s.CreateAttestation(as); err != nil {
		return nil, errors.Wrap(err, "failed to create attestation")
	}

	return as, nil
}
