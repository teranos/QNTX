package qntxatproto

import (
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	id "github.com/teranos/vanity-id"
)

// TimelinePost type definition for posts appearing in timeline
var TimelinePost = types.TypeDef{
	Name:             "timeline-post",
	Label:            "Timeline Post",
	Color:            "#1da1f2", // Bluesky blue
	RichStringFields: []string{"text"},
}

// EnsureTypes attests type definitions in the atproto context instead of graph
func EnsureTypes(store ats.AttestationStore, source string, typeDefs ...types.TypeDef) error {
	var errs []error

	for _, def := range typeDefs {
		// Default opacity to 1.0 if not explicitly set
		if def.Opacity == nil {
			defaultOpacity := 1.0
			def.Opacity = &defaultOpacity
		}

		if err := attestTypeInContext(store, def.Name, source, "atproto", attrs.From(def)); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to attest type %s", def.Name))
		}
	}

	if len(errs) > 0 {
		errMsg := "failed to create some type definitions:"
		for _, err := range errs {
			errMsg += "\n  - " + err.Error()
		}
		return errors.New(errMsg)
	}

	return nil
}

// attestTypeInContext creates a type attestation in a specific context
func attestTypeInContext(store ats.AttestationStore, typeName, source, ctxName string, attributes map[string]interface{}) error {
	if typeName == "" {
		return errors.New("typeName cannot be empty")
	}
	if source == "" {
		return errors.New("source cannot be empty")
	}

	// Generate ASID for the type definition
	asid, err := id.GenerateASID(typeName, "type", ctxName, "")
	if err != nil {
		return errors.Wrapf(err, "failed to generate ASID for type %s", typeName)
	}

	// Create attestation directly with CreateAttestation to avoid gRPC protobuf serialization issues
	attestation := &types.As{
		ID:         asid,
		Subjects:   []string{typeName},
		Predicates: []string{"type"},
		Contexts:   []string{ctxName}, // Use domain context instead of "graph"
		Actors:     []string{typeName}, // Self-certifying
		Timestamp:  time.Now(),
		Source:     source,
		Attributes: attributes,
	}

	if err := store.CreateAttestation(attestation); err != nil {
		return errors.Wrapf(err, "failed to create type attestation for %s", typeName)
	}

	return nil
}
