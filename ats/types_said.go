package ats

import "github.com/teranos/QNTX/ats/types"

// TypesSaid reads what a store's types say now, newest first: a definition that
// changed was attested again, and the newer claim is what the type says.
//
// It lives here rather than beside EnsureTypes because reading takes an
// AttestationFilter, which is this package's, and this package is what imports
// that one.
//
// A read that fails answers nothing, which attests — the same thing EnsureTypes
// did before it could ask, and the safer of the two ways to be wrong.
func TypesSaid(store AttestationStore, names ...string) types.Says {
	if store == nil || len(names) == 0 {
		return types.SaysNothing
	}

	found, err := store.GetAttestations(AttestationFilter{
		Subjects:   names,
		Predicates: []string{"type"},
	})
	if err != nil {
		return types.SaysNothing
	}

	said := make(map[string]*types.As, len(names))
	for _, as := range found {
		if len(as.Subjects) == 0 {
			continue
		}
		name := as.Subjects[0]
		if newest, ok := said[name]; ok && !as.Timestamp.After(newest.Timestamp) {
			continue
		}
		said[name] = as
	}

	return func(name string) (map[string]any, bool) {
		as, ok := said[name]
		if !ok {
			return nil, false
		}
		return as.Attributes, true
	}
}
