# VidStream: Real-Time Video Inference

**Module**: `ats/vidstream`
**Status**: Production-ready with ONNX inference
**Last Updated**: 2026-01-10

---

## Overview

VidStream provides WebSocket-based real-time video object detection for QNTX. Browser camera frames are sent via WebSocket to the Go server, which processes them through [ONNX Runtime](https://onnxruntime.ai/) models via Rust CGO bindings, returning bounding box detections.

### Purpose

Enable real-time browser-based video analysis where each frame generates object detections (YOLO models). Designed for low-latency frame processing with browser camera access - no desktop permissions required.

---

## Architecture

### High-Level Flow

```
┌──────────────┐  WebSocket   ┌─────────────┐    CGO     ┌────────────────┐
│   Browser    │─────────────▶│ Go Server   │───────────▶│ Rust ONNX      │
│  Camera API  │  JSON frames │ handlers.go │  vidstream │ VideoEngine    │
│  640x480     │              │             │            │ yolo11n.onnx   │
└──────────────┘              └─────────────┘            └────────────────┘
       │                             │                           │
       │                             ▼                           ▼
       │                      ┌──────────────┐           ┌──────────────┐
       └─────────────────────│ Bounding     │◀──────────│ Detections   │
         JSON detections      │ boxes drawn  │           │ [person, 89%]│
                              └──────────────┘           └──────────────┘
```

### Detailed Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                         Go Application                          │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                 vidstream package                        │   │
│  │  video_cgo.go (build tag: cgo && rustvideo)             │   │
│  │  video_nocgo.go (fallback stub)                         │   │
│  │  types.go (shared types)                                │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │ CGO
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Rust Library (libqntx_vidstream)            │
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
| [CGO](https://go.dev/wiki/cgo) over gRPC | Minimize latency for real-time processing |
| Excluded from workspace | CGO libraries have different build lifecycle |
| Pre-allocated buffers | Reduce GC pressure in hot path |
| [Build tags](https://pkg.go.dev/go/build#hdr-Build_Constraints) | Allow compilation without Rust toolchain |
| Stub inference | Decouple FFI work from ML integration |

---

## Current Implementation

### Working Features ✓

| Component | Status | Details |
|-----------|--------|---------|
| ONNX inference | ✓ | YOLOv11n model (10MB), 80ms latency |
| WebSocket transport | ✓ | 10MB message limit for 4.4MB JSON frames |
| Browser camera access | ✓ | MediaDevices API, 640x480 @ 62 FPS |
| Frame throttling | ✓ | 5 FPS inference to prevent overload |
| Object detection | ✓ | Person, tie, cup, etc. (COCO classes) |
| Bounding box rendering | ✓ | Green boxes with confidence labels |
| Async engine init | ✓ | Non-blocking 2-5s model load |
| Detection stats | ✓ | FPS, latency, detection count |

### Known Issues

| Issue | Impact | Workaround |
|-------|--------|------------|
| JSON frame encoding | 4.4MB per frame (inefficient) | TODO: Switch to binary WebSocket frames (would reduce to 1.2MB) |
| WebSocket message limit | Required increasing from 1MB → 10MB | See server/client.go:36 TODO comment |
| Test timing issues | Test flakes due to async init | Skip slow tests in `make test` |

---

## File Reference

### Rust (`src/`)

```
lib.rs          - Crate entry, re-exports public types
types.rs        - BoundingBox, Detection, ProcessingStats, Config
engine.rs       - VideoEngine with process_frame pipeline
ffi.rs          - C-compatible FFI functions and types
```

### Go (`vidstream/`)

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

### Rust Library with ONNX

```bash
cd ats/vidstream

# Build with ONNX support (REQUIRED for inference)
cargo build --release --features onnx

# Library output: target/release/libqntx_vidstream.{so,dylib,dll}
```

### Go Application

```bash
# Build with rustvideo tag (enables CGO)
CGO_ENABLED=1 go build -tags rustvideo ./cmd/qntx

# Runtime library path (macOS)
export DYLD_LIBRARY_PATH=$PWD/ats/vidstream/target/release:$DYLD_LIBRARY_PATH

# Runtime library path (Linux)
export LD_LIBRARY_PATH=$PWD/ats/vidstream/target/release:$LD_LIBRARY_PATH
```

### Quick Start

```bash
# 1. Build Rust library
cd ats/vidstream && cargo build --release --features onnx && cd ../..

# 2. Start QNTX server
DYLD_LIBRARY_PATH=$PWD/ats/vidstream/target/release ./qntx

# 3. Open browser to http://localhost:3030
# 4. Click VidStream button (⮀ icon)
# 5. Click "Initialize ONNX" → "Start Camera"
# 6. See detections rendered as green bounding boxes
```

---

## Architecture Details

### WebSocket Message Flow

**Client → Server (vidstream_init)**:
```json
{
  "type": "vidstream_init",
  "model_path": "ats/vidstream/models/yolo11n.onnx",
  "confidence_threshold": 0.5,
  "nms_threshold": 0.45
}
```

**Server → Client (vidstream_init_success)**:
```json
{
  "type": "vidstream_init_success",
  "width": 640,
  "height": 480,
  "ready": true
}
```

**Client → Server (vidstream_frame)** - 5 FPS:
```json
{
  "type": "vidstream_frame",
  "frame_data": [255, 128, 64, ...],  // RGBA bytes as JSON array (4.4MB)
  "width": 640,
  "height": 480,
  "format": "rgba8"
}
```

**Server → Client (vidstream_detections)**:
```json
{
  "type": "vidstream_detections",
  "detections": [
    {
      "ClassID": 0,
      "Label": "person",
      "Confidence": 0.89,
      "BBox": { "X": 109, "Y": 46, "Width": 525, "Height": 433 }
    }
  ],
  "stats": {
    "inference_us": 79915,
    "total_us": 80605,
    "detections_final": 1
  }
}
```

## Performance Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| Model size | 10MB | YOLOv11n (nano variant) |
| Engine init time | 2-5s | One-time cost on first load |
| Inference latency | 60-80ms | Per frame (CPU-only) |
| Frame rate | 5 FPS | Throttled to prevent WebSocket overload |
| Payload size | 4.4MB | JSON encoding (inefficient, see TODO) |
| WebSocket limit | 10MB | Allows ~2 frames buffered |

## Future Optimizations

**Binary WebSocket Frames** (TODO):
- Switch from JSON array to binary frames
- Header: 12 bytes (width:u32, height:u32, format:u32)
- Payload: raw RGBA bytes
- **Result**: 4.4MB → 1.2MB (3.6x reduction)
- Allows reducing maxMessageSize back to 2MB

**GPU Inference** (when needed):
- Enable ONNX GPU execution provider
- Requires CUDA/ROCm/Metal dependencies
- Expected: 80ms → 10-20ms latency

**Object Tracking** (future):
- TrackID field exists but currently unused
- Would enable persistent object identity across frames
- Useful for counting, dwell time, trajectory analysis

---

## Related Files

| File | Purpose |
|------|---------|
| server/vidstream.go | WebSocket message handlers (init, frame) |
| server/client.go | WebSocket limits and logging |
| web/ts/vidstream-window.ts | Frontend camera, rendering, throttling |
| ats/vidstream/src/engine.rs | Rust ONNX inference pipeline |
| ats/vidstream/src/ffi.rs | CGO interface layer |

---

## API Reference

- [WebSocket Protocol](api/websocket.md) - Complete WebSocket message type reference (including `vidstream_init` and `vidstream_frame`)
