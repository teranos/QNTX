package protocol

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// ToTypes converts a proto Attestation to types.As.
func (p *Attestation) ToTypes() *types.As {
	var attributes map[string]interface{}
	if p.Attributes != "" {
		_ = json.Unmarshal([]byte(p.Attributes), &attributes)
	}
	if attributes == nil {
		attributes = make(map[string]interface{})
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
func AttestationFromTypes(as *types.As) *Attestation {
	attributesJSON := ""
	if len(as.Attributes) > 0 {
		b, err := json.Marshal(as.Attributes)
		if err == nil {
			attributesJSON = string(b)
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
		Attributes: attributesJSON,
		CreatedAt:  as.CreatedAt.UnixMilli(),
	}
}
