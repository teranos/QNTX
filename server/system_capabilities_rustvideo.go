//go:build cgo && rustvideo

package server

import (
	"github.com/teranos/QNTX/ats/vidstream/vidstream"
)

// vidstreamAvailable returns true when built with CGO and rustvideo tag
func vidstreamAvailable() bool {
	return true
}

// vidstreamBackendVersion returns the vidstream library version
func vidstreamBackendVersion() string {
	return vidstream.Version()
}
