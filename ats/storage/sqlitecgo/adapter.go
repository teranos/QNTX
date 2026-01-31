package sqlitecgo

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// rustAttestation is the JSON-compatible format expected by Rust qntx-sqlite.
// Timestamps are Unix milliseconds (i64) instead of Go's time.Time.
type rustAttestation struct {
	ID         string                 `json:"id"`
	Subjects   []string               `json:"subjects"`
	Predicates []string               `json:"predicates"`
	Contexts   []string               `json:"contexts"`
	Actors     []string               `json:"actors"`
	Timestamp  int64                  `json:"timestamp"` // Unix milliseconds
	Source     string                 `json:"source"`
	Attributes map[string]interface{} `json:"attributes"`
	CreatedAt  int64                  `json:"created_at"` // Unix milliseconds
}

// toRustJSON converts Go types.As to Rust-compatible JSON.
func toRustJSON(as *types.As) ([]byte, error) {
	if as == nil {
		return nil, errors.New("attestation is nil")
	}

	// Ensure attributes is never nil for Rust
	attrs := as.Attributes
	if attrs == nil {
		attrs = make(map[string]interface{})
	}

	rust := rustAttestation{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: attrs,
		CreatedAt:  as.CreatedAt.UnixMilli(),
	}

	return json.Marshal(rust)
}

// fromRustJSON converts Rust JSON back to Go types.As.
func fromRustJSON(jsonBytes []byte) (*types.As, error) {
	var rust rustAttestation
	if err := json.Unmarshal(jsonBytes, &rust); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal Rust JSON")
	}

	// Convert Unix milliseconds back to time.Time
	timestamp := time.UnixMilli(rust.Timestamp)
	createdAt := time.UnixMilli(rust.CreatedAt)

	as := &types.As{
		ID:         rust.ID,
		Subjects:   rust.Subjects,
		Predicates: rust.Predicates,
		Contexts:   rust.Contexts,
		Actors:     rust.Actors,
		Timestamp:  timestamp,
		Source:     rust.Source,
		Attributes: rust.Attributes,
		CreatedAt:  createdAt,
	}

	return as, nil
}
