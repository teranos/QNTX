//go:build cgo && rustvideo

package server

// vidstreamAvailable returns true when built with CGO and rustvideo tag
func vidstreamAvailable() bool {
	return true
}
