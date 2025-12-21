package ix

// StructuredOptions represents execution options for structured data source adapters.
//
// This is a generic pattern that can be used by any adapter implementation
// to configure execution behavior. The options are domain-agnostic and can
// be applied to any data source processor.
//
// Example usage:
//
//	opts := ix.StructuredOptions{
//	    Actor:        "system@data-importer",
//	    IncludeTrace: true,
//	}
//	result, err := myAdapter.Execute(ctx, path, dryRun, opts)
type StructuredOptions struct {
	// Actor is the identifier of the entity performing the data import.
	// This will be used to attribute generated attestations to the appropriate actor.
	Actor string

	// IncludeTrace indicates whether to include detailed execution traces
	// in the result output. Useful for debugging and detailed logging.
	IncludeTrace bool
}
