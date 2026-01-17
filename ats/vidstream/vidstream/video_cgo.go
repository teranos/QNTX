//go:build cgo && rustvideo

// Package vidstream provides a CGO wrapper for the Rust video processing engine.
//
// This package links directly with the Rust library via CGO, providing
// real-time video frame processing for attestation generation.
//
// Build Requirements:
//
//	Rust toolchain (cargo build --release in ats/vidstream)
//	CGO enabled (CGO_ENABLED=1)
//	Build tag: -tags rustvideo
//	Library path set correctly for your platform
//
// Usage:
//
//	engine, err := vidstream.NewVideoEngine()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer engine.Close()
//
//	result, err := engine.ProcessFrame(frameData, width, height, vidstream.FormatRGB8, timestamp)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for _, detection := range result.Detections {
//	    // Create attestation from detection
//	}
package vidstream

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_vidstream -lpthread -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_vidstream -lpthread -ldl -lm
#cgo windows LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_vidstream -lws2_32 -luserenv

#include "video_engine.h"
#include <stdlib.h>
*/
import "C"

import (
	"runtime"
	"unsafe"

	"github.com/teranos/QNTX/errors"
)

// VideoEngine wraps the Rust VideoEngine via CGO
type VideoEngine struct {
	engine *C.VideoEngine
}

// NewVideoEngine creates a new Rust-backed video processing engine with default config.
// The caller must call Close() when done to free resources.
func NewVideoEngine() (*VideoEngine, error) {
	engine := C.video_engine_new()
	if engine == nil {
		return nil, errors.New("video_engine_new returned nil (check Rust library)")
	}

	ve := &VideoEngine{engine: engine}

	// Set finalizer as safety net (but caller should still call Close)
	runtime.SetFinalizer(ve, func(v *VideoEngine) {
		v.Close()
	})

	return ve, nil
}

// NewVideoEngineWithConfig creates a new video engine with custom configuration.
func NewVideoEngineWithConfig(cfg Config) (*VideoEngine, error) {
	var cConfig C.VideoEngineConfigC

	// Convert strings to C
	var cModelPath *C.char
	var cLabels *C.char

	if cfg.ModelPath != "" {
		cModelPath = C.CString(cfg.ModelPath)
		defer C.free(unsafe.Pointer(cModelPath))
	}
	if cfg.Labels != "" {
		cLabels = C.CString(cfg.Labels)
		defer C.free(unsafe.Pointer(cLabels))
	}

	cConfig.model_path = cModelPath
	cConfig.confidence_threshold = C.float(cfg.ConfidenceThreshold)
	cConfig.nms_threshold = C.float(cfg.NMSThreshold)
	cConfig.input_width = C.uint32_t(cfg.InputWidth)
	cConfig.input_height = C.uint32_t(cfg.InputHeight)
	cConfig.num_threads = C.uint32_t(cfg.NumThreads)
	cConfig.use_gpu = C.bool(cfg.UseGPU)
	cConfig.labels = cLabels

	engine := C.video_engine_new_with_config(&cConfig)
	if engine == nil {
		return nil, errors.Newf("video_engine_new_with_config returned nil (model: %s)", cfg.ModelPath)
	}

	ve := &VideoEngine{engine: engine}
	runtime.SetFinalizer(ve, func(v *VideoEngine) {
		v.Close()
	})

	return ve, nil
}

// Close frees the underlying Rust engine.
// Safe to call multiple times.
func (v *VideoEngine) Close() error {
	if v.engine != nil {
		C.video_engine_free(v.engine)
		v.engine = nil
	}
	return nil
}

// ProcessFrame processes a single video frame and returns detections.
//
// Parameters:
//   - frameData: Raw pixel data
//   - width: Frame width in pixels
//   - height: Frame height in pixels
//   - format: Pixel format of input data
//   - timestampUs: Frame timestamp in microseconds (for tracking)
//
// Returns processing result with detections and timing statistics.
func (v *VideoEngine) ProcessFrame(
	frameData []byte,
	width, height uint32,
	format FrameFormat,
	timestampUs uint64,
) (*ProcessingResult, error) {
	if v.engine == nil {
		return nil, errors.New("engine is closed")
	}

	if len(frameData) == 0 {
		return nil, errors.New("empty frame data")
	}

	result := C.video_engine_process_frame(
		v.engine,
		(*C.uint8_t)(unsafe.Pointer(&frameData[0])),
		C.size_t(len(frameData)),
		C.uint32_t(width),
		C.uint32_t(height),
		C.int(format),
		C.uint64_t(timestampUs),
	)
	defer C.video_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.Newf("frame processing failed: %s", errMsg)
	}

	// Convert detections
	var detections []Detection
	if result.detections_len > 0 {
		cDetections := unsafe.Slice(result.detections, result.detections_len)
		detections = make([]Detection, len(cDetections))

		for i, cd := range cDetections {
			detections[i] = Detection{
				ClassID:    uint32(cd.class_id),
				Label:      C.GoString(cd.label),
				Confidence: float32(cd.confidence),
				BBox: BoundingBox{
					X:      float32(cd.bbox.x),
					Y:      float32(cd.bbox.y),
					Width:  float32(cd.bbox.width),
					Height: float32(cd.bbox.height),
				},
				TrackID: uint64(cd.track_id),
			}
		}
	}

	return &ProcessingResult{
		Detections: detections,
		Stats: ProcessingStats{
			PreprocessUs:    uint64(result.stats.preprocess_us),
			InferenceUs:     uint64(result.stats.inference_us),
			PostprocessUs:   uint64(result.stats.postprocess_us),
			TotalUs:         uint64(result.stats.total_us),
			FrameWidth:      uint32(result.stats.frame_width),
			FrameHeight:     uint32(result.stats.frame_height),
			DetectionsRaw:   uint32(result.stats.detections_raw),
			DetectionsFinal: uint32(result.stats.detections_final),
		},
	}, nil
}

// IsReady returns true if the engine is ready for inference.
func (v *VideoEngine) IsReady() bool {
	if v.engine == nil {
		return false
	}
	return bool(C.video_engine_is_ready(v.engine))
}

// InputDimensions returns the model's expected input dimensions.
func (v *VideoEngine) InputDimensions() (width, height uint32) {
	if v.engine == nil {
		return 0, 0
	}

	var w, h C.uint32_t
	if C.video_engine_get_input_dimensions(v.engine, &w, &h) {
		return uint32(w), uint32(h)
	}
	return 0, 0
}

// ExpectedFrameSize returns the expected frame data size for the given parameters.
func ExpectedFrameSize(width, height uint32, format FrameFormat) int {
	return int(C.video_expected_frame_size(
		C.uint32_t(width),
		C.uint32_t(height),
		C.int(format),
	))
}

// Version returns the vidstream library version string.
func Version() string {
	return C.GoString(C.video_engine_version())
}
