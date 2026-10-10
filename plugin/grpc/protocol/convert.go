package protocol

import (
	"encoding/json"
	"maps"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// NewJobLogEntry creates a JobLogEntry with the current timestamp.
// This is the standard way to build log entries in ExecuteJob implementations.
func NewJobLogEntry(level, stage, message string) *JobLogEntry {
	return &JobLogEntry{
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     level,
		Stage:     stage,
		Message:   message,
	}
}

// ErrUnknownHandler returns a formatted error for unrecognized handler names in ExecuteJob.
func ErrUnknownHandler(handlerName string) error {
	return errors.Newf("unknown handler: %s", handlerName)
}

// ToTypes converts a proto Attestation to types.As.
func (p *Attestation) ToTypes() *types.As {
	return &types.As{
		ID:         p.Id,
		Subjects:   p.Subjects,
		Predicates: p.Predicates,
		Contexts:   p.Contexts,
		Actors:     p.Actors,
		Timestamp:  time.UnixMilli(p.Timestamp),
		Source:     p.Source,
		Attributes: p.GetAttributes().AsMap(),
		CreatedAt:  time.UnixMilli(p.CreatedAt),
		Signature:  p.Signature,
		SignerDID:  p.SignerDid,
	}
}

// AttestationFromTypes converts a types.As to a proto Attestation.
func AttestationFromTypes(as *types.As) (*Attestation, error) {
	attrs, err := AttributesStruct(as.Attributes)
	if err != nil {
		return nil, errors.Wrapf(err, "attestation %s", as.ID)
	}

	return &Attestation{
		Id:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: attrs,
		CreatedAt:  as.CreatedAt.UnixMilli(),
		Signature:  as.Signature,
		SignerDid:  as.SignerDID,
	}, nil
}

// AttributesStruct is attributes as a protobuf Struct, read through their JSON:
// what JSON holds the Struct holds, and a value JSON cannot hold is refused.
// Attributes holding none are an empty Struct.
func AttributesStruct(attributes map[string]any) (*structpb.Struct, error) {
	held := make(map[string]any, len(attributes))
	maps.Copy(held, attributes)
	raw, err := json.Marshal(held)
	if err != nil {
		return nil, errors.Wrapf(err, "attributes %v have no JSON form", attributes)
	}
	var attrs structpb.Struct
	if err := protojson.Unmarshal(raw, &attrs); err != nil {
		return nil, errors.Wrapf(err, "attributes %s have no protobuf Struct form", raw)
	}
	return &attrs, nil
}
