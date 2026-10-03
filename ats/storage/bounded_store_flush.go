//go:build cgo && !quickdev

package storage

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/errors"
)

// FlushEnforcement runs enforcement directly through Rust for all recent attestations.
// Used by tests to verify enforcement behavior synchronously. A swallowed
// failure here is a test asserting on a store the flush never touched.
func (bs *BoundedStore) FlushEnforcement() error {
	rbs, ok := bs.store.(*RustBackedStore)
	if !ok {
		return errors.Newf("enforcement flush requires *RustBackedStore, store is %T", bs.store)
	}
	// Run a broad enforcement pass
	allActors := []string{}
	allContexts := []string{}
	allSubjects := []string{}

	// Query all attestations to collect dimensions
	all, err := rbs.rust.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		return errors.Wrap(err, "failed to list attestations for enforcement flush")
	}
	actorSet := map[string]struct{}{}
	contextSet := map[string]struct{}{}
	subjectSet := map[string]struct{}{}
	for _, a := range all {
		for _, v := range a.Actors {
			actorSet[v] = struct{}{}
		}
		for _, v := range a.Contexts {
			contextSet[v] = struct{}{}
		}
		for _, v := range a.Subjects {
			subjectSet[v] = struct{}{}
		}
	}
	for k := range actorSet {
		allActors = append(allActors, k)
	}
	for k := range contextSet {
		allContexts = append(allContexts, k)
	}
	for k := range subjectSet {
		allSubjects = append(allSubjects, k)
	}

	cfg := rbs.enforcementCfg
	if _, err := rbs.rust.EnforceLimits(allActors, allContexts, allSubjects, cfg); err != nil {
		return errors.Wrapf(err, "failed to enforce limits over %d actors, %d contexts, %d subjects",
			len(allActors), len(allContexts), len(allSubjects))
	}
	return nil
}
