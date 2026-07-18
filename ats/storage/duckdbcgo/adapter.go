//go:build cgo && rustduckdb

package duckdbcgo

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// ffiAttestation is the JSON shape shared between Go and Rust at the FFI boundary.
// On the Rust side, qntx_proto::Attestation deserializes this JSON with int64
// timestamps in milliseconds — not RFC3339 strings, which Go's default
// time.Time marshaling would produce.
//
// Identical shape to ats/storage/sqlitecgo/adapter.go. Kept in-crate rather
// than shared because the two backends will diverge in what they carry over
// the boundary (e.g., duckdb may add Parquet-partition metadata later).
type ffiAttestation struct {
	ID         string                 `json:"id,omitempty"`
	Subjects   []string               `json:"subjects,omitempty"`
	Predicates []string               `json:"predicates,omitempty"`
	Contexts   []string               `json:"contexts,omitempty"`
	Actors     []string               `json:"actors,omitempty"`
	Timestamp  int64                  `json:"timestamp,omitempty"`
	Source     string                 `json:"source,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	CreatedAt  int64                  `json:"created_at,omitempty"`
	Signature  []byte                 `json:"signature,omitempty"` // base64 in JSON
	SignerDID  string                 `json:"signer_did,omitempty"`
}

// toRustJSON converts Go types.As to JSON matching the proto shape Rust expects.
func toRustJSON(as *types.As) ([]byte, error) {
	if as == nil {
		return nil, errors.New("attestation is nil")
	}
	ffi := ffiAttestation{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: as.Attributes,
		CreatedAt:  as.CreatedAt.UnixMilli(),
		Signature:  as.Signature,
		SignerDID:  as.SignerDID,
	}
	return json.Marshal(ffi)
}

// fromRustJSON converts Rust JSON back to Go types.As.
func fromRustJSON(jsonBytes []byte) (*types.As, error) {
	var ffi ffiAttestation
	if err := json.Unmarshal(jsonBytes, &ffi); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal FFI JSON")
	}
	return &types.As{
		ID:         ffi.ID,
		Subjects:   ffi.Subjects,
		Predicates: ffi.Predicates,
		Contexts:   ffi.Contexts,
		Actors:     ffi.Actors,
		Timestamp:  time.UnixMilli(ffi.Timestamp),
		Source:     ffi.Source,
		Attributes: ffi.Attributes,
		CreatedAt:  time.UnixMilli(ffi.CreatedAt),
		Signature:  ffi.Signature,
		SignerDID:  ffi.SignerDID,
	}, nil
}
