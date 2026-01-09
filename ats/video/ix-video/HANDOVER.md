# ix-video Development Handover

**Module**: `ats/video/ix-video`
**Status**: Foundation complete, inference stub
**Last Updated**: 2026-01-09

---

## Overview

ix-video provides CGO-based real-time video frame processing for QNTX attestation generation. It follows the established `fuzzy-ax` pattern for Rust/Go integration via CGO.

### Purpose

Enable real-time video stream analysis where each frame can generate attestations about detected objects, scenes, or events. The ultra-low latency CGO approach was chosen over gRPC plugins to minimize frame processing overhead.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Go Application                          │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                  ixvideo package                         │   │
│  │  video_cgo.go (build tag: cgo && rustvideo)             │   │
│  │  video_nocgo.go (fallback stub)                         │   │
│  │  types.go (shared types)                                │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │ CGO
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Rust Library (libqntx_ix_video)             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │   ffi.rs     │  │  engine.rs   │  │     types.rs         │  │
│  │  C-ABI layer │──│  Processing  │──│  Detection, BBox,    │  │
│  │  Memory mgmt │  │  Pipeline    │  │  Config, Stats       │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| CGO over gRPC | Minimize latency for real-time processing |
| Excluded from workspace | CGO libraries have different build lifecycle |
| Pre-allocated buffers | Reduce GC pressure in hot path |
| Build tags | Allow compilation without Rust toolchain |
| Stub inference | Decouple FFI work from ML integration |

---

## Current Implementation

### Completed ✓

| Component | File | Status |
|-----------|------|--------|
| Rust crate structure | `Cargo.toml` | ✓ |
| Core types | `src/types.rs` | ✓ |
| Video engine | `src/engine.rs` | ✓ (stub inference) |
| FFI layer | `src/ffi.rs` | ✓ |
| C header | `include/video_engine.h` | ✓ |
| Go CGO bindings | `ixvideo/video_cgo.go` | ✓ |
| Go fallback | `ixvideo/video_nocgo.go` | ✓ |
| Shared Go types | `ixvideo/types.go` | ✓ |
| Unit tests | 9 tests passing | ✓ |
| Documentation | `README.md` | ✓ |

### Not Implemented ✗

| Component | Priority | Notes |
|-----------|----------|-------|
| ONNX model loading | High | Feature flag `onnx` exists |
| YOLO output parsing | High | Depends on model format |
| Object tracking | Medium | TrackID field exists but unused |
| FFmpeg decoding | Low | Feature flag `ffmpeg` exists |
| GPU inference | Low | `use_gpu` config exists |
| Integration tests | Medium | Requires built library |

---

## File Reference

### Rust (`src/`)

```
lib.rs          - Crate entry, re-exports public types
types.rs        - BoundingBox, Detection, ProcessingStats, Config
engine.rs       - VideoEngine with process_frame pipeline
ffi.rs          - C-compatible FFI functions and types
```

### Go (`ixvideo/`)

```
types.go        - Shared types (no build tags)
video_cgo.go    - CGO implementation (build tag: cgo && rustvideo)
video_nocgo.go  - Stub returning ErrNotAvailable (build tag: !cgo || !rustvideo)
```

### Headers (`include/`)

```
video_engine.h  - C API declarations for CGO
```

---

## Build Instructions

### Rust Library

```bash
cd ats/video/ix-video

# Development
cargo build

# Release (required for Go integration)
cargo build --release

# With ONNX support (when implemented)
cargo build --release --features onnx
```

### Go Application

```bash
# Build with CGO
CGO_ENABLED=1 go build -tags rustvideo ./...

# Runtime library path
export LD_LIBRARY_PATH=/path/to/QNTX/target/release:$LD_LIBRARY_PATH
```

---

## Implementation Guide: ONNX Integration

The inference stub in `engine.rs:247-269` needs to be replaced with actual ONNX model execution.

### Step 1: Add ONNX Session to Engine

```rust
// engine.rs
pub struct VideoEngine {
    config: VideoEngineConfig,
    labels: Vec<String>,
    state: RwLock<EngineState>,
    #[cfg(feature = "onnx")]
    session: ort::Session,  // Add this
}
```

### Step 2: Load Model in Constructor

```rust
#[cfg(feature = "onnx")]
fn load_model(model_path: &str) -> Result<ort::Session, String> {
    ort::Session::builder()
        .map_err(|e| e.to_string())?
        .with_optimization_level(ort::GraphOptimizationLevel::Level3)
        .map_err(|e| e.to_string())?
        .commit_from_file(model_path)
        .map_err(|e| e.to_string())
}
```

### Step 3: Implement Inference

```rust
fn run_inference(state: &mut EngineState, session: &ort::Session) {
    state.raw_detections.clear();

    // Reshape input_buffer to [1, 3, H, W] for YOLO
    let input = ndarray::Array::from_shape_vec(
        (1, 3, 640, 640),
        state.input_buffer.clone()
    ).unwrap();

    let outputs = session.run(ort::inputs![input]).unwrap();

    // Parse YOLO output format
    // Output shape: [1, 84, 8400] for YOLOv8
    // 84 = 4 (bbox) + 80 (classes)
    // 8400 = detection candidates

    parse_yolo_output(&outputs[0], &mut state.raw_detections);
}
```

### Step 4: YOLO Output Parsing

```rust
fn parse_yolo_output(output: &ort::Value, detections: &mut Vec<Detection>) {
    let tensor = output.extract_tensor::<f32>().unwrap();
    let data = tensor.view();

    // YOLOv8 output: [1, 84, 8400]
    // Transpose to [8400, 84] for easier processing
    for i in 0..8400 {
        let cx = data[[0, 0, i]];
        let cy = data[[0, 1, i]];
        let w = data[[0, 2, i]];
        let h = data[[0, 3, i]];

        // Find best class
        let mut best_class = 0;
        let mut best_conf = 0.0f32;
        for c in 0..80 {
            let conf = data[[0, 4 + c, i]];
            if conf > best_conf {
                best_conf = conf;
                best_class = c;
            }
        }

        if best_conf > 0.25 {  // Pre-NMS threshold
            detections.push(Detection {
                class_id: best_class as u32,
                label: String::new(),  // Filled in postprocess
                confidence: best_conf,
                bbox: BoundingBox {
                    x: cx - w / 2.0,
                    y: cy - h / 2.0,
                    width: w,
                    height: h,
                },
                track_id: 0,
            });
        }
    }
}
```

---

## Testing

### Current Tests

```bash
cd ats/video/ix-video
cargo test
```

```
test engine::tests::test_bounding_box_iou ... ok
test engine::tests::test_frame_size_calculation ... ok
test engine::tests::test_engine_creation ... ok
test engine::tests::test_invalid_threshold ... ok
test engine::tests::test_label_parsing ... ok
test ffi::tests::test_engine_lifecycle ... ok
test ffi::tests::test_expected_frame_size ... ok
test ffi::tests::test_null_engine_handling ... ok
test ffi::tests::test_frame_processing ... ok
```

### Suggested Additional Tests

1. **Integration test with Go** - Verify CGO bindings work end-to-end
2. **Memory leak test** - Verify FFI allocations are properly freed
3. **Concurrent processing** - Stress test thread safety
4. **Large frame handling** - Test with 4K frames
5. **Format conversion** - Verify all pixel formats work correctly

---

## Known Issues

1. **ort version**: Using RC version `2.0.0-rc.9` - update when stable
2. **Preprocessing**: Current resize is naive bilinear, consider SIMD optimization
3. **YUV420**: Only Y channel used, full conversion not implemented

---

## Related Files

| File | Purpose |
|------|---------|
| `/home/user/QNTX/Cargo.toml` | Workspace excludes ix-video |
| `/home/user/QNTX/ats/ax/fuzzy-ax/` | Reference CGO implementation |
| `/home/user/QNTX/ats/types/attestation.go` | Attestation structure for output |

---

## Contact

Branch: `claude/rust-video-processing-i7Mrb`

For questions about the CGO pattern, reference the fuzzy-ax implementation which has been production-tested.
