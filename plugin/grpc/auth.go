package grpc

import (
	"crypto/subtle"

	"github.com/teranos/QNTX/errors"
)

// ValidateToken performs constant-time comparison of authentication tokens.
// This prevents timing attacks by comparing all bytes regardless of match status.
func ValidateToken(providedToken, storedToken string) error {
	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(storedToken)) != 1 {
		return errors.New("invalid authentication token")
	}
	return nil
}
