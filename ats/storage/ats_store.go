package storage

import (
	"context"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
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
// operations (Parquet flush on parquet, backup, WAL checkpoint) stay on the
// concrete backend types and are called directly by the code that needs them.
type AtsStore struct {
	raw RawAttestationStore
	log *zap.SugaredLogger
	// namespace is the universe this store is. It is what observers are routed
	// by, so a store that names none reaches none (ADR-026).
	namespace string
}

// NewAtsStore wraps a raw backend with the shared ATS domain logic.
//
// namespace is which universe this store holds. A backend that keeps one
// passes the name of the one it keeps; a backend that opens a store per
// namespace passes the name it opened.
func NewAtsStore(raw RawAttestationStore, log *zap.SugaredLogger, namespace string) *AtsStore {
	return &AtsStore{raw: raw, log: log, namespace: namespace}
}

// Raw returns the underlying backend for callers that need backend-specific
// operations (e.g., a Flusher or Backuper on the concrete type).
func (s *AtsStore) Raw() RawAttestationStore {
	return s.raw
}

// CreateAttestation signs then delegates.
func (s *AtsStore) CreateAttestation(as *types.As) error {
	if err := signBeforeStore(s.log, as); err != nil {
		return err
	}

	if err := s.raw.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "backend create attestation %s", as.ID)
	}

	NotifyObservers(s.namespace, as)
	return nil
}

// "if k took the list in memory and wrote it in one transaction, the drain would go far faster"
//
// ManyCreator writes many attestations at once, answering each in the order
// given: nil for one written, what CreateAttestation would have said for one
// that was not. The second error is the write as a whole failing.
type ManyCreator interface {
	CreateAttestations(attestations []*types.As) ([]error, error)
}

// CreateAttestations signs each, writes them in one go where the backend can
// (ManyCreator) and one at a time where it cannot, and tells the observers of
// each one written.
func (s *AtsStore) CreateAttestations(attestations []*types.As) ([]error, error) {
	answers, err := createMany(s.log, attestations, s.raw.CreateAttestation, s.raw)
	if err != nil {
		return nil, err
	}
	for i, as := range attestations {
		if answers[i] == nil {
			NotifyObservers(s.namespace, as)
		}
	}
	return answers, nil
}

// createMany signs each attestation and writes those it could sign: through
// many when it is a ManyCreator, and with one for each otherwise.
func createMany(log *zap.SugaredLogger, attestations []*types.As, one func(*types.As) error, many any) ([]error, error) {
	answers := make([]error, len(attestations))
	signed := make([]*types.As, 0, len(attestations))
	at := make([]int, 0, len(attestations))
	for i, as := range attestations {
		if err := signBeforeStore(log, as); err != nil {
			answers[i] = err
			continue
		}
		signed = append(signed, as)
		at = append(at, i)
	}
	if creator, ok := many.(ManyCreator); ok {
		written, err := creator.CreateAttestations(signed)
		if err != nil {
			return nil, err
		}
		for j, i := range at {
			if written[j] != nil {
				answers[i] = errors.Wrapf(written[j], "backend create attestation %s", signed[j].ID)
			}
		}
		return answers, nil
	}
	for j, i := range at {
		if err := one(signed[j]); err != nil {
			answers[i] = errors.Wrapf(err, "backend create attestation %s", signed[j].ID)
		}
	}
	return answers, nil
}

// CreateAttestationInbound stores a synced attestation without signing.
func (s *AtsStore) CreateAttestationInbound(as *types.As) error {
	warnIDLikeSubjects(s.log, as.ID, as.Subjects)

	if err := s.raw.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "backend create inbound attestation %s", as.ID)
	}

	NotifyObservers(s.namespace, as)
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

// GetAttestationsByIDs resolves many ids at once. Every method here is
// forwarded by hand, so a batch method added to a raw store is invisible above
// this wrapper until it is named — which is what happened to this one.
func (s *AtsStore) GetAttestationsByIDs(ids []string) ([]*types.As, error) {
	if b, ok := s.raw.(BatchGetStore); ok {
		return b.GetAttestationsByIDs(ids)
	}
	out := make([]*types.As, 0, len(ids))
	for _, id := range ids {
		as, err := s.raw.GetAttestation(id)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read attestation %s of %d", id, len(ids))
		}
		if as != nil {
			out = append(out, as)
		}
	}
	return out, nil
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

	as, err := cmd.ToAs(asid, "")
	if err != nil {
		return nil, err
	}
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

// signBeforeStore warns of subjects that look like ids and signs with the
// default signer, when the node has one. Every write that is the node's own
// goes through it before the backend sees the attestation.
func signBeforeStore(log *zap.SugaredLogger, as *types.As) error {
	warnIDLikeSubjects(log, as.ID, as.Subjects)

	if signer := getDefaultSigner(); signer != nil {
		if err := signer.Sign(as); err != nil {
			return errors.Wrapf(err, "failed to sign attestation %s", as.ID)
		}
	}
	return nil
}
