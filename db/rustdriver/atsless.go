//go:build atsless

package rustdriver

// An ATSless node has no Rust, so no flight recorder to tag a query for.
func SetCaller(string) {}
