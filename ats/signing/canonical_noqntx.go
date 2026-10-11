//go:build !qntxwasm && !quickdev

package signing

import errors "github.com/teranos/sacred-error"

func canonical(wire []byte) ([]byte, error) {
	return nil, errors.New("canonical attestation bytes require the qntxwasm build tag")
}
