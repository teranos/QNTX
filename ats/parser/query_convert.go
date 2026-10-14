package parser

// nilIfEmpty returns nil for empty slices to match Go parser behavior.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
