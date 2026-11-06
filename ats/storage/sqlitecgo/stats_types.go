package sqlitecgo

// StorageStats contains storage statistics returned by Rust.
type StorageStats struct {
	TotalAttestations int `json:"total_attestations"`
	UniqueSubjects    int `json:"unique_subjects"`
	UniquePredicates  int `json:"unique_predicates"`
	UniqueContexts    int `json:"unique_contexts"`
	UniqueActors      int `json:"unique_actors"`
}
