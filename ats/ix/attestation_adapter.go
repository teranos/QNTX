package ix

// ItemAttestationAdapter adapts ix.IxItem to the AttestationItem interface
type ItemAttestationAdapter struct {
	IxItem
}

// GetSubject returns the subject of the item
func (adapter ItemAttestationAdapter) GetSubject() string {
	return adapter.Subject
}

// GetPredicate returns the predicate of the item
func (adapter ItemAttestationAdapter) GetPredicate() string {
	return adapter.Pred
}

// GetContext returns the context of the item
func (adapter ItemAttestationAdapter) GetContext() string {
	return adapter.Obj // TODO: rename Obj field to Ctx (breaking JSON change: "obj" → "ctx")
}

// GetMeta returns the metadata of the item
func (adapter ItemAttestationAdapter) GetMeta() map[string]string {
	return adapter.Meta
}

// AdaptItemsForAttestation converts ix.IxItems to AttestationItems
func AdaptItemsForAttestation(items []IxItem) []ItemAttestationAdapter {
	adapted := make([]ItemAttestationAdapter, len(items))
	for i, item := range items {
		adapted[i] = ItemAttestationAdapter{item}
	}
	return adapted
}
