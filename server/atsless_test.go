package server

import (
	"testing"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/errors"
)

// Only ATS being absent is passed on an ATSless node; any other failure is
// what it always was.
func TestWantsATSIsATSAbsentOnly(t *testing.T) {
	for err, want := range map[error]bool{
		errors.Wrap(identity.ErrNoWASM, "failed to mint a canvas id"): true,
		errors.Wrap(storage.ErrATSless, "attestations not read"):      true,
		errors.New("default's canvas could not be read"):              false,
	} {
		if wantsATS(err) != want {
			t.Errorf("wantsATS(%v) is %v, want %v", err, !want, want)
		}
	}
}
