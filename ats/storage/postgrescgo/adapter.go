//go:build cgo && rustpostgres

package postgrescgo

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// ffiAttestation is the JSON shape shared between Go and Rust at the FFI
// boundary: qntx_proto::Attestation, with int64 timestamps in milliseconds.
// Identical shape to ats/storage/duckdbcgo/adapter.go, kept per backend as
// sqlitecgo and duckdbcgo each keep theirs.
type ffiAttestation struct {
	ID         string         `json:"id,omitempty"`
	Subjects   []string       `json:"subjects,omitempty"`
	Predicates []string       `json:"predicates,omitempty"`
	Contexts   []string       `json:"contexts,omitempty"`
	Actors     []string       `json:"actors,omitempty"`
	Timestamp  int64          `json:"timestamp,omitempty"`
	Source     string         `json:"source,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	CreatedAt  int64          `json:"created_at,omitempty"`
	Signature  []byte         `json:"signature,omitempty"` // base64 in JSON
	SignerDID  string         `json:"signer_did,omitempty"`
}

func toRustJSON(as *types.As) ([]byte, error) {
	return json.Marshal(ffiAttestation{
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
	})
}

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

// fromRustArray reads a JSON array of attestations.
func fromRustArray(body string) ([]*types.As, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal([]byte(body), &raws); err != nil {
		return nil, errors.Wrap(err, "failed to parse attestation array")
	}
	out := make([]*types.As, 0, len(raws))
	for _, r := range raws {
		as, err := fromRustJSON(r)
		if err != nil {
			return nil, err
		}
		out = append(out, as)
	}
	return out, nil
}
