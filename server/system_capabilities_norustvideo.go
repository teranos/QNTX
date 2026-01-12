//go:build !cgo || !rustvideo

package server

// vidstreamAvailable returns false when built without CGO or rustvideo tag
func vidstreamAvailable() bool {
	return false
}
