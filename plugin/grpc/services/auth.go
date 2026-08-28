package services

import (
	"crypto/subtle"

	"github.com/teranos/errors"
)

// ValidateToken performs constant-time comparison of authentication tokens.
// This prevents timing attacks by comparing all bytes regardless of match status.
//
// A refusal here travels back inside the response message (Success: false,
// Error: ...) with a nil transport error — the plugin protocol's convention,
// because a gRPC status error would discard the structured response plugins
// are written to read. The nilerr suppressions at those returns point here.
// The one exception is a response shape with no error field, where the
// transport error is the only honest channel (see AttestationExists).
func ValidateToken(providedToken, storedToken string) error {
	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(storedToken)) != 1 {
		return errors.New("invalid authentication token")
	}
	return nil
}
