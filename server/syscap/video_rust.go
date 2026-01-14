//go:build cgo && rustvideo

package syscap

import (
	"github.com/teranos/QNTX/ats/vidstream/vidstream"
)

// VidstreamAvailable returns true when built with CGO and rustvideo tag
func VidstreamAvailable() bool {
	return true
}

// vidstreamAvailable is an internal alias for backward compatibility
func vidstreamAvailable() bool {
	return VidstreamAvailable()
}

// vidstreamBackendVersion returns the vidstream library version
func vidstreamBackendVersion() string {
	return vidstream.Version()
}
