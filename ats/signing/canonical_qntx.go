//go:build qntxwasm

package signing

import (
	"github.com/teranos/QNTX/ats/wasm"
	errors "github.com/teranos/sacred-error"
)

// canonical has crates/ats-sign write the bytes, through the WASM engine.
func canonical(wire []byte) ([]byte, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "WASM engine unavailable for canonical attestation bytes")
	}
	return engine.CanonicalAttestation(wire)
}
