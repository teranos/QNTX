//go:build atsless

package server

// An ATSless node has no Rust slow log to report from.
func buildPerformanceData() map[string]any { return nil }
