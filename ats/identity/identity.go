// Package identity provides ASUID generation for all QNTX components.
//
// This is the single entry point for identity generation. Implementation
// dispatches to Rust WASM (qntxwasm build tag) or falls back to vanity-id.
//
// Usage:
//
//	asuid, err := identity.GenerateASUID("AS", subject, predicate, context)
//	jobID, err := identity.GenerateJobID(handlerName, source)
//	execID := identity.GenerateExecutionID()
package identity

import "github.com/teranos/QNTX/errors"

// GenerateASUID generates an Attestation System Unique ID with the given prefix.
// Prefix is typically "AS" for attestations.
func GenerateASUID(prefix, subject, predicate, context string) (string, error) {
	return generateASUID(prefix, subject, predicate, context)
}

// GenerateASUIDWithRetry generates an ASUID, retrying up to 10 times if
// checkExists returns true (collision detected).
func GenerateASUIDWithRetry(prefix, subject, predicate, context string, checkExists func(string) bool) (string, error) {
	const maxRetries = 10
	for i := 0; i < maxRetries; i++ {
		id, err := generateASUID(prefix, subject, predicate, context)
		if err != nil {
			return "", errors.Wrapf(err, "ASUID generation attempt %d/%d for %s/%s/%s", i+1, maxRetries, subject, predicate, context)
		}
		if !checkExists(id) {
			return id, nil
		}
	}
	return "", errors.Newf("ASUID collision after %d retries for %s/%s/%s", maxRetries, subject, predicate, context)
}

// GenerateJobID generates a Job ASUID (JB prefix).
func GenerateJobID(jobType, source string) (string, error) {
	return generateASUID("JB", jobType, "process", source)
}

// GenerateExecutionID generates a Pulse Execution ID (PX prefix).
func GenerateExecutionID() string {
	id, _ := generateASUID("PX", "execution", "id", "pulse")
	return id
}
