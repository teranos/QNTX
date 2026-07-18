package storage

import (
	"context"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// AtsStore is the backend-agnostic wrapper: it takes a RawAttestationStore
// (the CGO layer of any backend — sqlite, parquet, and future) and adds the
// Go-side domain concerns that apply regardless of backend:
//
//   - Signing on create (via the default signer)
//   - Observer notification on write
//   - Warning when an ID looks like a subject (guard against a common bug)
//   - Vanity ASID generation for GenerateAndCreateAttestation
//
// The RawAttestationStore contract is deliberately minimal; backend-specific
// operations (bounded enforcement on sqlite, Parquet flush on parquet,
// backup, WAL checkpoint) stay on the concrete backend types and are called
// directly by the code that needs them.
type AtsStore struct {
	raw RawAttestationStore
	log *zap.SugaredLogger
}

// NewAtsStore wraps a raw backend with the shared ATS domain logic.
func NewAtsStore(raw RawAttestationStore, log *zap.SugaredLogger) *AtsStore {
	return &AtsStore{raw: raw, log: log}
}

// Raw returns the underlying backend for callers that need backend-specific
// operations (e.g., a Flusher or Backuper on the concrete type).
func (s *AtsStore) Raw() RawAttestationStore {
	return s.raw
}

// CreateAttestation signs then delegates.
func (s *AtsStore) CreateAttestation(as *types.As) error {
	warnIDLikeSubjects(s.log, as.ID, as.Subjects)

	if signer := getDefaultSigner(); signer != nil {
		if err := signer.Sign(as); err != nil {
			return errors.Wrapf(err, "failed to sign attestation %s", as.ID)
		}
	}

	if err := s.raw.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "backend create attestation %s", as.ID)
	}

	NotifyObservers(as)
	return nil
}

// CreateAttestationInbound stores a synced attestation without signing.
func (s *AtsStore) CreateAttestationInbound(as *types.As) error {
	warnIDLikeSubjects(s.log, as.ID, as.Subjects)

	if err := s.raw.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "backend create inbound attestation %s", as.ID)
	}

	NotifyObservers(as)
	return nil
}

// AttestationExists checks by ID.
func (s *AtsStore) AttestationExists(asid string) bool {
	return s.raw.AttestationExists(asid)
}

// GetAttestation retrieves a single attestation by ID.
func (s *AtsStore) GetAttestation(id string) (*types.As, error) {
	return s.raw.GetAttestation(id)
}

// GetAttestations retrieves attestations by filter. Delegates when the raw
// backend satisfies QueryableStore; returns an error when it doesn't.
func (s *AtsStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	q, ok := s.raw.(QueryableStore)
	if !ok {
		return nil, errors.New("backend does not implement filter queries yet")
	}
	return q.GetAttestations(filter)
}

// GenerateAndCreateAttestation generates a vanity ASID and stores the resulting
// self-certifying attestation. Backend-agnostic; uses AttestationExists to
// check collisions.
func (s *AtsStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	checkExists := func(asid string) bool { return s.raw.AttestationExists(asid) }

	subject := firstOr(cmd.Subjects, "_")
	predicate := firstOr(cmd.Predicates, "_")
	ctxStr := firstOr(cmd.Contexts, "_")

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

// CountAttestations returns the backend's count.
func (s *AtsStore) CountAttestations() (int, error) {
	return s.raw.CountAttestations()
}

func firstOr(v []string, fallback string) string {
	if len(v) == 0 {
		return fallback
	}
	return v[0]
}
