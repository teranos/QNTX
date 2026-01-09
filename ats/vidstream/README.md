# QNTX Video Streaming (vidstream)

Real-time video frame processing for QNTX attestation generation via CGO.

**Development Documentation**: See [docs/vidstream-inference.md](../../docs/vidstream-inference.md) for details and design decisions.

## Build Requirements

### Rust Library (Basic)

```bash
cd ats/vidstream
cargo build --release
```

This produces `target/release/libqntx_vidstream.so` (Linux), `.dylib` (macOS), or `.dll` (Windows).

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

## Performance Benchmarks

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

## CI

Automated checks run via [.github/workflows/vidstream.yml](../../.github/workflows/vidstream.yml).
