package ats

import errors "github.com/teranos/sacred-error"

// UnwritableRegion is the region a store's refusal carries when the
// attestation is one no store takes: crates/ats/src/attestation/writable.rs
// says which, for every door that writes, and nothing on this side says it again.
const UnwritableRegion = "unwritable"

// Unwritable reports whether err is a store refusing the attestation itself,
// which the writer can fix, rather than the store failing.
func Unwritable(err error) bool {
	return refusedIn(err, UnwritableRegion)
}

// AlreadyExistsRegion is the region a store's refusal carries when it already
// holds an attestation by that id: crates/qntx-ffi-common/src/store.rs names it.
const AlreadyExistsRegion = "already-exists"

// AlreadyExists reports whether err is a store refusing an id it already holds.
// The writer asked for what is already true, so nothing failed.
func AlreadyExists(err error) bool {
	return refusedIn(err, AlreadyExistsRegion)
}

func refusedIn(err error, region string) bool {
	var said *errors.Error
	return errors.As(err, &said) && said.Context.Region == region
}
