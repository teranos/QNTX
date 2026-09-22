//go:build !cgo || !rustduckdb

package server

// recordSpendOf has no store reaching a location in this build. A backend that
// keeps its record on the node spends nothing against it, so there is nothing
// to report and false says so.
func recordSpendOf(_ any) (RecordReporter, bool) {
	return nil, false
}
