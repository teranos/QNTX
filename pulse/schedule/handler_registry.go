package schedule

// HandlerMapping maps data types to async job handlers.
// This registry determines which Pulse handler to invoke for a given data source type.
//
// Data types come from ix command parsing (e.g., "jd", "vacancies", "luma")
// and map to specific async job handlers in the internal/role package.
var HandlerMapping = map[string]string{
	"jd":        "role.jd-ingestion",
	"role":      "role.jd-ingestion", // Alias for jd
	"vacancies": "role.vacancies-scraper",
	// Future mappings as async handlers are added:
	// "linkedin":  "role.linkedin-import",
	// "vcf":       "role.vcf-import",
	// "luma":      "role.luma-import",
}

// GetHandler returns the async handler name for a data type, or empty string if not supported.
//
// Example:
//
//	GetHandler("jd")        -> "role.jd-ingestion"
//	GetHandler("vacancies") -> "role.vacancies-scraper"
//	GetHandler("unknown")   -> ""
func GetHandler(dataType string) string {
	return HandlerMapping[dataType]
}

// IsSchedulable returns true if the data type can be scheduled as an async job.
//
// A data type is schedulable if it has a registered handler in HandlerMapping.
// This is used to determine whether an ix command can be sent to the Pulse job queue.
//
// Example:
//
//	IsSchedulable("jd")      -> true
//	IsSchedulable("linkedin") -> false (not yet implemented)
func IsSchedulable(dataType string) bool {
	_, ok := HandlerMapping[dataType]
	return ok
}
