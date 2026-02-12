//go:build !qntxwasm

package ats

import (
	"github.com/teranos/QNTX/ats/types"
)

// ExpandCartesianClaims requires the Rust WASM engine.
func ExpandCartesianClaims(_ []types.As) []IndividualClaim {
	panic("cartesian expansion requires qntxwasm build tag — rebuild with 'make cli' or 'go build -tags qntxwasm'")
}

// GroupClaimsByKey requires the Rust WASM engine.
func GroupClaimsByKey(_ []IndividualClaim) map[ClaimKey][]IndividualClaim {
	panic("claim grouping requires qntxwasm build tag — rebuild with 'make cli' or 'go build -tags qntxwasm'")
}

// ConvertClaimsToAttestations requires the Rust WASM engine.
func ConvertClaimsToAttestations(_ []IndividualClaim) []types.As {
	panic("claim deduplication requires qntxwasm build tag — rebuild with 'make cli' or 'go build -tags qntxwasm'")
}
