package services

import (
	"crypto/subtle"

	errors "github.com/teranos/sacred-error"
)

// ValidateToken performs constant-time comparison of authentication tokens.
// This prevents timing attacks by comparing all bytes regardless of match status.
//
// A refusal here travels back inside the response message (Success: false,
// Error: ...) with a nil transport error — a gRPC status error would discard
// the structured response plugins are written to read. The nilerr
// suppressions point here; AttestationExists is the exception (no error field).
func ValidateToken(providedToken, storedToken string) error {
	if !sameToken(providedToken, storedToken) {
		return errors.New("invalid authentication token")
	}
	return nil
}

// sameToken is whether the token provided is the one stored, compared in
// constant time.
func sameToken(providedToken, storedToken string) bool {
	return subtle.ConstantTimeCompare([]byte(providedToken), []byte(storedToken)) == 1
}
