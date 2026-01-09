# QNTX Video Ingestion (ix-video)

Real-time video frame processing for QNTX attestation generation via CGO.

## Architecture

```
┌─────────────────┐     CGO      ┌──────────────────────┐
│  Go Application │─────────────▶│  Rust VideoEngine    │
│  (ixvideo pkg)  │              │  • Frame processing  │
└─────────────────┘              │  • ONNX inference    │
                                 │  • Detection output  │
                                 └──────────────────────┘
```

## Build Requirements

### Rust Library

```bash
cd ats/video/ix-video
cargo build --release
```

This produces `target/release/libqntx_ix_video.so` (Linux) or equivalent.

### Go with CGO

```bash
# Enable CGO and build with rustvideo tag
CGO_ENABLED=1 go build -tags rustvideo ./...

# Set library path at runtime
export LD_LIBRARY_PATH=/path/to/QNTX/target/release:$LD_LIBRARY_PATH
```

## Usage

### Go API

```go
package main

import (
    "log"
    "github.com/teranos/QNTX/ats/video/ix-video/ixvideo"
)

func main() {
    // Create engine with default config
    engine, err := ixvideo.NewVideoEngine()
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close()

    // Or with custom config
    cfg := ixvideo.Config{
        ModelPath:           "/path/to/yolov8n.onnx",
        ConfidenceThreshold: 0.5,
        NMSThreshold:        0.45,
        InputWidth:          640,
        InputHeight:         640,
    }
    engine, err = ixvideo.NewVideoEngineWithConfig(cfg)

    // Process frames
    result, err := engine.ProcessFrame(
        frameData,           // []byte - raw pixel data
        width, height,       // uint32 - frame dimensions
        ixvideo.FormatRGB8,  // pixel format
        timestampUs,         // uint64 - frame timestamp
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create attestations from detections
    for _, det := range result.Detections {
        log.Printf("Detected %s (%.2f) at [%.0f,%.0f,%.0f,%.0f]",
            det.Label,
            det.Confidence,
            det.BBox.X, det.BBox.Y,
            det.BBox.Width, det.BBox.Height,
        )
    }

    // Check processing stats
    log.Printf("Processing took %d us (preprocess: %d, inference: %d, postprocess: %d)",
        result.Stats.TotalUs,
        result.Stats.PreprocessUs,
        result.Stats.InferenceUs,
        result.Stats.PostprocessUs,
    )
}
```

### Frame Formats

| Format | Description | Bytes/Pixel |
|--------|-------------|-------------|
| `FormatRGB8` | RGB, 8 bits per channel | 3 |
| `FormatRGBA8` | RGBA, 8 bits per channel | 4 |
| `FormatBGR8` | BGR (OpenCV default) | 3 |
| `FormatYUV420` | YUV420 planar | 1.5 |
| `FormatGray8` | Grayscale | 1 |

### Attestation Integration

```go
// Convert detection to QNTX attestation
as := &types.As{
    Subjects:   []string{fmt.Sprintf("VIDEO:%s", streamID), fmt.Sprintf("FRAME:%d", frameNum)},
    Predicates: []string{"detected", "classified"},
    Contexts:   []string{det.Label, fmt.Sprintf("confidence:%.2f", det.Confidence)},
    Actors:     []string{"video-processor@ai"},
    Timestamp:  time.Now(),
    Source:     "ix-video",
    Attributes: map[string]interface{}{
        "bbox":      []float32{det.BBox.X, det.BBox.Y, det.BBox.Width, det.BBox.Height},
        "class_id":  det.ClassID,
        "latency_us": result.Stats.TotalUs,
    },
}
```

## Optional Features

The Rust library supports optional features for extended functionality:

```bash
# Build with ONNX inference support
cargo build --release --features onnx

# Build with FFmpeg video decoding
cargo build --release --features ffmpeg

# Build with all features
cargo build --release --features full
```

**Note:** Features require additional system dependencies:
- `onnx`: ONNX Runtime libraries
- `ffmpeg`: FFmpeg development libraries

## Fallback Mode

When built without CGO or the `rustvideo` tag, the Go package provides stub implementations that return `ErrNotAvailable`. This allows the codebase to compile on systems without Rust.

```go
engine, err := ixvideo.NewVideoEngine()
// err == ixvideo.ErrNotAvailable when CGO is disabled
```

## Performance Considerations

- **Pre-allocated buffers**: The engine reuses buffers to minimize allocations
- **Thread-safe**: Multiple goroutines can call `ProcessFrame` concurrently
- **Zero-copy where possible**: Frame data is passed by reference to Rust
- **Batch processing**: For video files, consider processing frames in parallel

## Testing

```bash
# Rust tests
cd ats/video/ix-video
cargo test

# Go tests (requires built Rust library)
CGO_ENABLED=1 go test -tags rustvideo ./ats/video/ix-video/ixvideo/...
```
