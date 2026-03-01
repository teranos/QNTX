package sqlitecgo

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// ffiAttestation is the JSON shape shared between Go and Rust at the FFI boundary.
// On the Rust side, the proto type (qntx_proto::Attestation) deserializes this JSON
// using serde with base64_serde for signature and serde(default) for missing fields.
// Go's omitempty omits nil/zero fields; Rust's serde(default) fills in defaults.
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
	Signature  []byte                 `json:"signature,omitempty"`  // base64 in JSON
	SignerDID  string                 `json:"signer_did,omitempty"`
}

// toRustJSON converts Go types.As to JSON for the Rust FFI boundary.
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
