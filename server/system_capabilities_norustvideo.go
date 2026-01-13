//go:build !cgo || !rustvideo

package server

// vidstreamAvailable returns false when built without CGO or rustvideo tag
func vidstreamAvailable() bool {
	return false
}

// vidstreamBackendVersion returns "n/a" when vidstream is not available
func vidstreamBackendVersion() string {
	return "n/a"
}
