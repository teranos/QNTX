package protocol

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"google.golang.org/protobuf/types/known/structpb"
)

// ToTypes converts a proto Attestation to types.As.
func (p *Attestation) ToTypes() *types.As {
	attributes := make(map[string]interface{})
	if p.Attributes != nil {
		attributes = p.Attributes.AsMap()
	}

	return &types.As{
		ID:         p.Id,
		Subjects:   p.Subjects,
		Predicates: p.Predicates,
		Contexts:   p.Contexts,
		Actors:     p.Actors,
		Timestamp:  time.UnixMilli(p.Timestamp),
		Source:     p.Source,
		Attributes: attributes,
		CreatedAt:  time.UnixMilli(p.CreatedAt),
	}
}

// AttestationFromTypes converts a types.As to a proto Attestation.
func AttestationFromTypes(as *types.As) (*Attestation, error) {
	var attrs *structpb.Struct
	if len(as.Attributes) > 0 {
		// Convert Go types to protobuf-compatible types ([]string → []interface{})
		converted := convertToProtoCompatible(as.Attributes)

		var err error
		attrs, err = structpb.NewStruct(converted)
		if err != nil {
			return nil, fmt.Errorf("failed to convert attributes to protobuf Struct: %w", err)
		}
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
	}, nil
}

// convertToProtoCompatible recursively converts Go types to protobuf-compatible types.
// Specifically converts []string to []interface{} which structpb.NewStruct requires.
func convertToProtoCompatible(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = convertValue(v)
	}
	return result
}

func convertValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []string:
		// Convert []string to []interface{}
		result := make([]interface{}, len(val))
		for i, s := range val {
			result[i] = s
		}
		return result
	case map[string]interface{}:
		// Recursively convert nested maps
		return convertToProtoCompatible(val)
	case []interface{}:
		// Recursively convert slice elements
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = convertValue(item)
		}
		return result
	default:
		// Other types (string, int, float64, bool) are already compatible
		return v
	}
}
