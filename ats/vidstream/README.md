# QNTX Video Streaming (vidstream)

Real-time video frame processing for QNTX attestation generation via CGO.

## Architecture

```
┌─────────────────┐     CGO      ┌──────────────────────┐
│  Go Application │─────────────▶│  Rust VideoEngine    │
│ (vidstream pkg) │              │  • Frame processing  │
└─────────────────┘              │  • ONNX inference    │
                                 │  • Detection output  │
                                 └──────────────────────┘
```

## Build Requirements

### Rust Library (Basic)

```bash
cd ats/vidstream
cargo build --release
```

This produces `target/release/libqntx_vidstream.so` (Linux), `.dylib` (macOS), or `.dll` (Windows).

**Note:** The basic build runs in stub mode (no real inference). For actual ML inference, see [ONNX Feature](#onnx-runtime-inference) below.

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
    "github.com/teranos/QNTX/ats/vidstream/vidstream"
)

func main() {
    // Create engine with default config
    engine, err := vidstream.NewVideoEngine()
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close()

    // Or with custom config
    cfg := vidstream.Config{
        ModelPath:           "/path/to/yolov8n.onnx",
        ConfidenceThreshold: 0.5,
        NMSThreshold:        0.45,
        InputWidth:          640,
        InputHeight:         640,
    }
    engine, err = vidstream.NewVideoEngineWithConfig(cfg)

    // Process frames
    result, err := engine.ProcessFrame(
        frameData,           // []byte - raw pixel data
        width, height,       // uint32 - frame dimensions
        vidstream.FormatRGB8,  // pixel format
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
    Source:     "vidstream",
    Attributes: map[string]interface{}{
        "bbox":      []float32{det.BBox.X, det.BBox.Y, det.BBox.Width, det.BBox.Height},
        "class_id":  det.ClassID,
        "latency_us": result.Stats.TotalUs,
    },
}
```

## Optional Features

The Rust library supports optional features for extended functionality.

### ONNX Runtime Inference

**Automatic Setup (Recommended)**

The `onnx` feature automatically downloads ONNX Runtime binaries for your platform:

```bash
cd ats/vidstream
cargo build --release --features onnx
```

This uses the `download-binaries` feature from the `ort` crate, which:
- Downloads pre-built ONNX Runtime for your OS/arch
- Caches binaries in `target/` directory
- Requires network access on first build
- Works on: Linux (x86_64, aarch64), macOS (x86_64, arm64), Windows (x86_64)

**Manual ONNX Runtime (Advanced)**

If you prefer to use a system-installed ONNX Runtime:

```bash
# Install ONNX Runtime via package manager
# Ubuntu/Debian:
apt-get install libonnxruntime-dev

# macOS (Homebrew):
brew install onnxruntime

# Or via Nix (recommended for QNTX developers):
nix develop  # ONNX Runtime available in dev shell

# Then build without download-binaries
export ORT_LIB_LOCATION=/path/to/onnxruntime/lib
cargo build --release --features onnx
```

**ONNX Runtime Version**

Currently using `ort` version 2.0.0-rc.11, which supports:
- ONNX Runtime 1.22.x
- CPU inference (default)
- GPU inference (requires additional configuration)

**Downloading Models for Development**

To test with a real YOLO model:

```bash
cd ats/vidstream
mkdir -p models

# Download YOLO11n (nano) - 10MB, ~40ms latency at 640x480
gh release download v8.3.0 --repo ultralytics/assets --pattern "yolo11n.onnx" --dir models

# Or download YOLOv8n for comparison
gh release download v8.3.0 --repo ultralytics/assets --pattern "yolov8n.onnx" --dir models

# Run benchmark
cargo run --release --features onnx --example benchmark
```

Performance with YOLO11n on CPU:
- Average latency: 40.83 ms per frame
- Throughput: 24.5 FPS (640x480 input)
- Inference: 39ms (97%), Preprocessing: 1ms (2.3%)

**Note:** The `models/` directory is gitignored. Models must be downloaded separately for development/testing.

### FFmpeg Video Decoding (Future)

```bash
# Build with FFmpeg video decoding (NOT YET IMPLEMENTED)
cargo build --release --features ffmpeg

# Build with all features
cargo build --release --features full
```

**Note:** FFmpeg feature requires FFmpeg development libraries installed on your system.

## Fallback Mode

When built without CGO or the `rustvideo` tag, the Go package provides stub implementations that return `ErrNotAvailable`. This allows the codebase to compile on systems without Rust.

```go
engine, err := vidstream.NewVideoEngine()
// err == vidstream.ErrNotAvailable when CGO is disabled
```

## Performance Considerations

- **Pre-allocated buffers**: The engine reuses buffers to minimize allocations
- **Thread-safe**: Multiple goroutines can call `ProcessFrame` concurrently
- **Zero-copy where possible**: Frame data is passed by reference to Rust
- **Batch processing**: For video files, consider processing frames in parallel

## Testing

### Rust Tests

```bash
cd ats/vidstream

# Test without ONNX (stub mode - fast)
cargo test --lib

# Test with ONNX (inference mode - downloads ONNX Runtime on first run)
cargo test --lib --features onnx
```

### Go CGO Tests

```bash
# Build Rust library first
cd ats/vidstream
cargo build --release

# Run Go tests
cd vidstream
CGO_ENABLED=1 go test -tags rustvideo -v
```

### Performance Benchmarks

The `examples/benchmark.rs` measures real-world inference latency:

```bash
cd ats/vidstream

# Download a model first (see "Downloading Models for Development" above)
gh release download v8.3.0 --repo ultralytics/assets --pattern "yolo11n.onnx" --dir models

# Run benchmark
cargo run --release --features onnx --example benchmark
```

**Output includes:**
- Per-frame latency breakdown (preprocess, inference, postprocess)
- Average latency and throughput (FPS)
- Detection counts (raw and after NMS filtering)
- Sample detections

**Example Results (YOLO11n on Apple Silicon M-series):**
```
Average latency: 40.83 ms per frame
Throughput:      24.5 FPS
Breakdown:
  - Preprocess:    973 μs (2.3%)
  - Inference:   41430 μs (97%)
  - Postprocess:     0 μs (negligible)
```

The benchmark uses synthetic gradient frames (640x480 RGB). For realistic testing, modify the example to load actual images or video frames.

### CI Pipeline

The GitHub Actions workflow (`.github/workflows/vidstream.yml`) runs:
- Rust format checking (`cargo fmt`)
- Clippy linting (with and without ONNX)
- Unit tests (with and without ONNX)
- CGO integration tests
- Release builds
