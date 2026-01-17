//go:build !cgo || !rustvideo

package syscap

// VidstreamAvailable returns false when built without CGO or rustvideo tag
func VidstreamAvailable() bool {
	return false
}

// vidstreamAvailable is an internal alias for backward compatibility
func vidstreamAvailable() bool {
	return VidstreamAvailable()
}

// vidstreamBackendVersion returns "n/a" when vidstream is not available
func vidstreamBackendVersion() string {
	return "n/a"
}
