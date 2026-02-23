package qntxatproto

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
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

		// Use unified AttestType with "atproto" context
		if err := types.AttestType(store, def.Name, source, "atproto", attrs.From(def)); err != nil {
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
