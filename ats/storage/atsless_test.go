package storage

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// An ATSless node's store says so wherever it is asked, rather than holding
// nothing quietly.
func TestAbsentRefusesEveryWriteAndRead(t *testing.T) {
	var store Absent
	for name, err := range map[string]error{
		"CreateAttestation":        store.CreateAttestation(&types.As{ID: "AS-1"}),
		"CreateAttestationInbound": store.CreateAttestationInbound(nil),
		"GenerateAndCreateAttestation": func() error {
			_, err := store.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{})
			return err
		}(),
		"GetAttestations": func() error {
			_, err := store.GetAttestations(ats.AttestationFilter{})
			return err
		}(),
	} {
		if !errors.Is(err, ErrATSless) {
			t.Errorf("%s answered %v, not ErrATSless", name, err)
		}
	}
	if store.AttestationExists("AS-1") {
		t.Error("an ATSless node has an attestation")
	}
}
