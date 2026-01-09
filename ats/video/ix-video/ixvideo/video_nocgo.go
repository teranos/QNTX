//go:build !cgo || !rustvideo

package ixvideo

import "errors"

// ErrNotAvailable is returned when the Rust video engine is not available.
var ErrNotAvailable = errors.New("CGO video engine not available: build with CGO_ENABLED=1 and -tags rustvideo")

// VideoEngine is a stub when CGO is not available.
type VideoEngine struct{}

// NewVideoEngine returns an error when CGO is not available.
func NewVideoEngine() (*VideoEngine, error) {
	return nil, ErrNotAvailable
}

// NewVideoEngineWithConfig returns an error when CGO is not available.
func NewVideoEngineWithConfig(_ Config) (*VideoEngine, error) {
	return nil, ErrNotAvailable
}

// Close is a no-op for the stub.
func (v *VideoEngine) Close() error {
	return nil
}

// ProcessFrame returns an error when CGO is not available.
func (v *VideoEngine) ProcessFrame(
	_ []byte,
	_, _ uint32,
	_ FrameFormat,
	_ uint64,
) (*ProcessingResult, error) {
	return nil, ErrNotAvailable
}

// IsReady returns false when CGO is not available.
func (v *VideoEngine) IsReady() bool {
	return false
}

// InputDimensions returns zeros when CGO is not available.
func (v *VideoEngine) InputDimensions() (width, height uint32) {
	return 0, 0
}

// ExpectedFrameSize returns the expected frame data size for the given parameters.
func ExpectedFrameSize(width, height uint32, format FrameFormat) int {
	return ExpectedFrameSizeFor(width, height, format)
}
