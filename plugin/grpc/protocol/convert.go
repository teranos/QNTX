package protocol

import (
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
		Signature:  p.Signature,
		SignerDID:  p.SignerDid,
	}
}

// AttestationFromTypes converts a types.As to a proto Attestation.
func AttestationFromTypes(as *types.As) *Attestation {
	var attrs *structpb.Struct
	if len(as.Attributes) > 0 {
		// structpb.NewStruct handles map[string]interface{} → Struct conversion.
		// Errors only on unsupported value types (e.g. channels), safe to ignore here.
		attrs, _ = structpb.NewStruct(as.Attributes)
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
	}
}
