//go:build quickdev

package server

// QuickDev has no Rust slow log to report from.
func buildPerformanceData() map[string]any { return nil }
