package server

import (
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/errors"
)

// wantsATS reports whether err is ATS being absent: an id this build cannot
// mint, or the attestation store an ATSless node does not keep.
func wantsATS(err error) bool {
	return errors.Is(err, identity.ErrNoWASM) || errors.Is(err, storage.ErrATSless)
}
