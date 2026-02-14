//go:build qntxwasm

package sync

import (
	"encoding/json"

	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/QNTX/errors"
)

// wasmTree implements SyncTree by delegating to qntx-core via wazero.
// Each method serializes its arguments as JSON, calls the corresponding Rust
// function, and deserializes the JSON result. All crypto (SHA-256, Merkle
// tree hashing) happens inside the WASM module — Go never touches raw bytes.
type wasmTree struct {
	engine *wasm.Engine
}

// NewSyncTree creates a SyncTree backed by the WASM engine.
// Panics if the WASM engine is unavailable — run `make wasm` to build.
func NewSyncTree() SyncTree {
	engine, err := wasm.GetEngine()
	if err != nil {
		panic("WASM sync tree unavailable: " + err.Error() + " — run `make wasm`")
	}
	return &wasmTree{engine: engine}
}

func (t *wasmTree) Root() (string, error) {
	raw, err := t.engine.Call("sync_merkle_root", "")
	if err != nil {
		return "", errors.Wrap(err, "sync_merkle_root")
	}

	var result struct {
		Root  string `json:"root"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", errors.Wrapf(err, "unmarshal sync_merkle_root: %s", raw)
	}
	if result.Error != "" {
		return "", errors.Newf("sync_merkle_root: %s", result.Error)
	}
	return result.Root, nil
}

func (t *wasmTree) GroupHashes() (map[string]string, error) {
	raw, err := t.engine.Call("sync_merkle_group_hashes", "")
	if err != nil {
		return nil, errors.Wrap(err, "sync_merkle_group_hashes")
	}

	var result struct {
		Groups map[string]string `json:"groups"`
		Error  string            `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, errors.Wrapf(err, "unmarshal sync_merkle_group_hashes: %s", raw)
	}
	if result.Error != "" {
		return nil, errors.Newf("sync_merkle_group_hashes: %s", result.Error)
	}
	return result.Groups, nil
}

func (t *wasmTree) Diff(remoteGroups map[string]string) (localOnly, remoteOnly, divergent []string, err error) {
	input, merr := json.Marshal(struct {
		Remote map[string]string `json:"remote"`
	}{Remote: remoteGroups})
	if merr != nil {
		return nil, nil, nil, errors.Wrap(merr, "marshal sync_merkle_diff input")
	}

	raw, cerr := t.engine.Call("sync_merkle_diff", string(input))
	if cerr != nil {
		return nil, nil, nil, errors.Wrap(cerr, "sync_merkle_diff")
	}

	var result struct {
		LocalOnly  []string `json:"local_only"`
		RemoteOnly []string `json:"remote_only"`
		Divergent  []string `json:"divergent"`
		Error      string   `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, nil, nil, errors.Wrapf(err, "unmarshal sync_merkle_diff: %s", raw)
	}
	if result.Error != "" {
		return nil, nil, nil, errors.Newf("sync_merkle_diff: %s", result.Error)
	}
	return result.LocalOnly, result.RemoteOnly, result.Divergent, nil
}

func (t *wasmTree) Contains(contentHashHex string) (bool, error) {
	input, err := json.Marshal(struct {
		ContentHash string `json:"content_hash"`
	}{ContentHash: contentHashHex})
	if err != nil {
		return false, errors.Wrap(err, "marshal sync_merkle_contains input")
	}

	raw, cerr := t.engine.Call("sync_merkle_contains", string(input))
	if cerr != nil {
		return false, errors.Wrap(cerr, "sync_merkle_contains")
	}

	var result struct {
		Exists bool   `json:"exists"`
		Error  string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return false, errors.Wrapf(err, "unmarshal sync_merkle_contains: %s", raw)
	}
	if result.Error != "" {
		return false, errors.Newf("sync_merkle_contains: %s", result.Error)
	}
	return result.Exists, nil
}

func (t *wasmTree) FindGroupKey(gkhHex string) (actor, context string, err error) {
	input, merr := json.Marshal(struct {
		GroupKeyHash string `json:"group_key_hash"`
	}{GroupKeyHash: gkhHex})
	if merr != nil {
		return "", "", errors.Wrap(merr, "marshal sync_merkle_find_group_key input")
	}

	raw, cerr := t.engine.Call("sync_merkle_find_group_key", string(input))
	if cerr != nil {
		return "", "", errors.Wrap(cerr, "sync_merkle_find_group_key")
	}

	var result struct {
		Actor   string `json:"actor"`
		Context string `json:"context"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", "", errors.Wrapf(err, "unmarshal sync_merkle_find_group_key: %s", raw)
	}
	if result.Error != "" {
		return "", "", errors.Newf("sync_merkle_find_group_key: %s", result.Error)
	}
	return result.Actor, result.Context, nil
}

func (t *wasmTree) Insert(actor, context, contentHashHex string) error {
	input, err := json.Marshal(struct {
		Actor       string `json:"actor"`
		Context     string `json:"context"`
		ContentHash string `json:"content_hash"`
	}{Actor: actor, Context: context, ContentHash: contentHashHex})
	if err != nil {
		return errors.Wrap(err, "marshal sync_merkle_insert input")
	}

	raw, cerr := t.engine.Call("sync_merkle_insert", string(input))
	if cerr != nil {
		return errors.Wrap(cerr, "sync_merkle_insert")
	}

	var result struct {
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return errors.Wrapf(err, "unmarshal sync_merkle_insert: %s", raw)
	}
	if result.Error != "" {
		return errors.Newf("sync_merkle_insert: %s", result.Error)
	}
	return nil
}

func (t *wasmTree) ContentHash(attestationJSON string) (string, error) {
	raw, err := t.engine.Call("sync_content_hash", attestationJSON)
	if err != nil {
		return "", errors.Wrap(err, "sync_content_hash")
	}

	var result struct {
		Hash  string `json:"hash"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", errors.Wrapf(err, "unmarshal sync_content_hash: %s", raw)
	}
	if result.Error != "" {
		return "", errors.Newf("sync_content_hash: %s", result.Error)
	}
	return result.Hash, nil
}
