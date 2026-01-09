// Package ixvideo provides video processing for QNTX attestation generation.
//
// This package supports two modes:
//   - CGO mode: Links with Rust library for high-performance inference
//   - Stub mode: Returns errors (for builds without Rust library)
//
// To enable CGO mode, build with:
//
//	CGO_ENABLED=1 go build -tags rustvideo
//	export LD_LIBRARY_PATH=/path/to/QNTX/target/release
package ixvideo

// FrameFormat specifies the pixel format of input frames
type FrameFormat int

const (
	// FormatRGB8 is RGB with 8 bits per channel (24 bits per pixel)
	FormatRGB8 FrameFormat = 0
	// FormatRGBA8 is RGBA with 8 bits per channel (32 bits per pixel)
	FormatRGBA8 FrameFormat = 1
	// FormatBGR8 is BGR with 8 bits per channel (OpenCV default)
	FormatBGR8 FrameFormat = 2
	// FormatYUV420 is YUV420 planar (common video format)
	FormatYUV420 FrameFormat = 3
	// FormatGray8 is grayscale 8-bit
	FormatGray8 FrameFormat = 4
)

// BoundingBox represents a detected object's location
type BoundingBox struct {
	X      float32 // X coordinate of top-left corner
	Y      float32 // Y coordinate of top-left corner
	Width  float32 // Width in pixels
	Height float32 // Height in pixels
}

// Detection represents a single detected object
type Detection struct {
	ClassID    uint32      // Class/label ID
	Label      string      // Human-readable label
	Confidence float32     // Confidence score 0.0-1.0
	BBox       BoundingBox // Bounding box
	TrackID    uint64      // Track ID (0 if not tracked)
}

// ProcessingStats contains timing information for performance monitoring
type ProcessingStats struct {
	PreprocessUs    uint64 // Frame preprocessing time (microseconds)
	InferenceUs     uint64 // Model inference time (microseconds)
	PostprocessUs   uint64 // Post-processing/NMS time (microseconds)
	TotalUs         uint64 // Total processing time (microseconds)
	FrameWidth      uint32 // Frame width processed
	FrameHeight     uint32 // Frame height processed
	DetectionsRaw   uint32 // Detections before NMS
	DetectionsFinal uint32 // Detections after NMS
}

// ProcessingResult contains the results of frame processing
type ProcessingResult struct {
	Detections []Detection
	Stats      ProcessingStats
}

// Config contains configuration for the video engine
type Config struct {
	// ModelPath is the path to the ONNX model file
	ModelPath string
	// ConfidenceThreshold is the minimum confidence for detections (0.0-1.0)
	ConfidenceThreshold float32
	// NMSThreshold is the IoU threshold for non-maximum suppression (0.0-1.0)
	NMSThreshold float32
	// InputWidth is the model input width (0 for auto-detect)
	InputWidth uint32
	// InputHeight is the model input height (0 for auto-detect)
	InputHeight uint32
	// NumThreads is the number of inference threads (0 for auto)
	NumThreads uint32
	// UseGPU enables GPU inference if available
	UseGPU bool
	// Labels contains class labels (newline-separated or JSON array)
	Labels string
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() Config {
	return Config{
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.45,
		InputWidth:          640,
		InputHeight:         640,
	}
}

// ExpectedFrameSizeFor returns the expected frame data size for the given parameters.
// This is a helper that doesn't require an engine instance.
func ExpectedFrameSizeFor(width, height uint32, format FrameFormat) int {
	pixels := int(width * height)
	switch format {
	case FormatRGB8, FormatBGR8:
		return pixels * 3
	case FormatRGBA8:
		return pixels * 4
	case FormatYUV420:
		return pixels + (pixels / 2)
	case FormatGray8:
		return pixels
	default:
		return 0
	}
}
