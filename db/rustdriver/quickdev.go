//go:build quickdev

package rustdriver

// QuickDev has no Rust, so no flight recorder to tag a query for.
func SetCaller(string) {}
