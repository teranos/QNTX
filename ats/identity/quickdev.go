package identity

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/teranos/errors"
)

// QuickDev mints its ids in Go: the prefix keeps what kind of thing an id
// names, and the rest is lowercase hex marked quickdev, so no QuickDev id is
// taken for an ASUID. US-quickdev-3f9a1c0b7e2d.

// quickDevID is a QuickDev id under prefix.
func quickDevID(prefix string) (string, error) {
	suffix, err := quickDevHex(12)
	if err != nil {
		return "", errors.Wrapf(err, "no QuickDev id under %s", prefix)
	}
	return prefix + "-quickdev-" + suffix, nil
}

// quickDevHex is length random lowercase hex characters.
func quickDevHex(length int) (string, error) {
	if length < 1 {
		return "", errors.Newf("a random id of length %d", length)
	}
	raw := make([]byte, (length+1)/2)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Wrap(err, "no randomness for a QuickDev id")
	}
	return hex.EncodeToString(raw)[:length], nil
}
