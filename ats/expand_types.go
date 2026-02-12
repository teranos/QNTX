package ats

import (
	"time"

	"github.com/teranos/QNTX/ats/types"
)

const (
	// ClaimKeySeparator is used to join subject, predicate, and context into a unique key
	ClaimKeySeparator = "|"
)

// IndividualClaim represents a single claim extracted from a multi-dimensional attestation
type IndividualClaim struct {
	Subject   string
	Predicate string
	Context   string
	Actor     string
	Timestamp time.Time
	SourceAs  types.As // Reference to original attestation
}

// ClaimKey represents the unique identifier for a claim (Subject, Predicate, Context)
type ClaimKey struct {
	Subject   string
	Predicate string
	Context   string
}

// String returns a string representation of the claim key
func (ck ClaimKey) String() string {
	return ck.Subject + ClaimKeySeparator + ck.Predicate + ClaimKeySeparator + ck.Context
}
