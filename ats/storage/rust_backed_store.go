//go:build cgo

package storage

import (
	"context"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// RustBackedStore wraps the Rust FFI store with Go domain logic:
// signing (before write), observers (after write), and bounded enforcement (periodic).
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

	return nil
}

// CreateAttestationInbound inserts a synced attestation without signing (preserves provenance).
func (s *RustBackedStore) CreateAttestationInbound(as *types.As) error {
	if err := s.rust.CreateAttestationInbound(as); err != nil {
		return errors.Wrapf(err, "rust create inbound attestation %s", as.ID)
	}

	notifyObservers(as)

	return nil
}

// FlushEnforcement runs enforcement for all pending dimensions.
// Used by tests to verify enforcement behavior synchronously.
func (s *RustBackedStore) FlushEnforcement() {
	// Enforcement is owned by Rust. This is a no-op in production.
	// Tests that need enforcement call it directly through the BoundedStore.
}

// GetStorageStats returns storage statistics via Rust FFI.
func (s *RustBackedStore) GetStorageStats() (*sqlitecgo.StorageStats, error) {
	return s.rust.GetStorageStats()
}

// AttestationExists checks if an attestation with the given ID exists.
func (s *RustBackedStore) AttestationExists(asid string) bool {
	return s.rust.AttestationExists(asid)
}

// GetAttestations retrieves attestations based on filters.
func (s *RustBackedStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return s.rust.GetAttestations(filters)
}

// GetAllPredicates returns all distinct predicates via Rust FFI.
func (s *RustBackedStore) GetAllPredicates() ([]string, error) {
	return s.rust.GetAllPredicates()
}

// GetAllContexts returns all distinct contexts via Rust FFI.
func (s *RustBackedStore) GetAllContexts() ([]string, error) {
	return s.rust.GetAllContexts()
}

// CountAttestations returns the total count of attestations via Rust FFI.
func (s *RustBackedStore) CountAttestations() (int, error) {
	return s.rust.CountAttestations()
}

// GetAttestation retrieves a single attestation by ID via Rust FFI.
func (s *RustBackedStore) GetAttestation(id string) (*types.As, error) {
	return s.rust.GetAttestation(id)
}

// QueryAttestationsRaw executes a raw SQL query through Rust's connection.
func (s *RustBackedStore) QueryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error) {
	return s.rust.QueryAttestationsRaw(sql, params)
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

// BatchGenerateAndCreateAttestations generates vanity ASIDs, signs, and stores
// multiple attestations in a single write queue slot.
func (s *RustBackedStore) BatchGenerateAndCreateAttestations(ctx context.Context, cmds []*types.AsCommand) (int, error) {
	if len(cmds) == 0 {
		return 0, nil
	}

	checkExists := func(asid string) bool {
		return s.rust.AttestationExists(asid)
	}

	signer := getDefaultSigner()
	attestations := make([]*types.As, 0, len(cmds))

	for _, cmd := range cmds {
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
			return 0, errors.Wrapf(err, "failed to generate vanity ASID for batch item %d", len(attestations))
		}

		as := cmd.ToAs(asid, "")
		as.Actors = []string{asid}

		if signer != nil {
			if err := signer.Sign(as); err != nil {
				return 0, errors.Wrapf(err, "failed to sign attestation %s", as.ID)
			}
		}

		attestations = append(attestations, as)
	}

	created, err := s.rust.BatchCreateAttestations(attestations)
	if err != nil {
		return created, err
	}

	for _, as := range attestations {
		notifyObservers(as)
	}

	return created, nil
}

// CreateAttestationHighPriority signs and stores with high priority.
// POST handler uses this to jump ahead of queued plugin writes.
func (s *RustBackedStore) CreateAttestationHighPriority(as *types.As) error {
	if signer := getDefaultSigner(); signer != nil {
		if err := signer.Sign(as); err != nil {
			return errors.Wrapf(err, "failed to sign attestation %s", as.ID)
		}
	}

	if err := s.rust.CreateAttestationHighPriority(as); err != nil {
		return errors.Wrapf(err, "rust create attestation %s (high priority)", as.ID)
	}

	notifyObservers(as)
	return nil
}

// Backup creates a hot backup of the database to destPath.
// Implements schedule.BackupProvider.
func (s *RustBackedStore) Backup(destPath string) error {
	return s.rust.Backup(destPath)
}

// CrashTest triggers a deliberate crash for flight recorder verification.
func (s *RustBackedStore) CrashTest() {
	s.rust.CrashTest()
}

