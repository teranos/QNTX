package ats

import errors "github.com/teranos/sacred-error"

// UnwritableRegion is the region a store's refusal carries when the
// attestation is one no store takes: crates/ats/src/attestation/writable.rs
// says which, for every door that writes, and nothing on this side says it again.
const UnwritableRegion = "unwritable"

// Unwritable reports whether err is a store refusing the attestation itself,
// which the writer can fix, rather than the store failing.
func Unwritable(err error) bool {
	var said *errors.Error
	return errors.As(err, &said) && said.Context.Region == UnwritableRegion
}
