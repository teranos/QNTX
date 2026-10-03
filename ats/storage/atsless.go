package storage

import (
	"context"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// ErrATSless is what an ATSless node answers wherever an attestation would be
// written or read: it was built without ATS.
var ErrATSless = errors.New("this node runs ATSless: it was built with -tags atsless and keeps no attestations")

// Absent is the attestation store of an ATSless node. It keeps nothing and
// refuses every write and read, so what needs attestations says so instead of
// finding none.
type Absent struct{}

var _ ats.AttestationStore = Absent{}

func (Absent) CreateAttestation(*types.As) error {
	return errors.Wrap(ErrATSless, "attestation not created")
}

func (Absent) CreateAttestationInbound(*types.As) error {
	return errors.Wrap(ErrATSless, "inbound attestation not created")
}

// AttestationExists is false: an ATSless node has none.
func (Absent) AttestationExists(string) bool { return false }

func (Absent) GenerateAndCreateAttestation(context.Context, *types.AsCommand) (*types.As, error) {
	return nil, errors.Wrap(ErrATSless, "attestation not generated")
}

func (Absent) GetAttestations(ats.AttestationFilter) ([]*types.As, error) {
	return nil, errors.Wrap(ErrATSless, "attestations not read")
}
