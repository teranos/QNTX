//go:build !qntxwasm

package sync

// NewSyncTree panics without qntxwasm — WASM is required for sync.
func NewSyncTree() SyncTree {
	panic("WASM sync tree required: built without qntxwasm tag — run `make wasm`")
}
