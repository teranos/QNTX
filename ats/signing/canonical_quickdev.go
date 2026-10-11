//go:build !qntxwasm && quickdev

package signing

// canonical is QuickDev's own: Go's encoding/json, which crates/ats-sign was
// written to match. testdata/canonical.json holds both to it.
func canonical(wire []byte) ([]byte, error) {
	return wire, nil
}
