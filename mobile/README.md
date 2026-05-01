# mobile-scry

iOS show-off app for the Scry inference signals. Standalone — no QNTX core,
no Loom, no ATS attestations. Just chat + the 3D logit-space fan-out.

## Demo loop

1. Open app, model loads from disk (or downloads on first launch).
2. Type prompt, watch tokens stream in colored by per-token confidence.
3. **Tap a token.** Fullscreen transitions into 3D PCA logit space. The
   chosen token sits at center; the runner-up alternatives fan out at
   their PCA positions, sized and alpha'd by probability.
4. Tap an alternative → text rewrites from that point with the alternative
   chosen (RDR / `fork_and_generate`).

## Layout

```
mobile/
├── Package.swift                  # SwiftPM library targets
├── Frameworks/
│   └── llama.xcframework          # built by scripts/build-llama-xcframework.sh
├── Sources/
│   ├── ScryCore/                  # Obj-C++ bridge over InferenceEngine
│   │   ├── include/
│   │   │   ├── ScryEngine.h       # public Obj-C interface
│   │   │   └── module.modulemap
│   │   └── ScryEngine.mm
│   ├── ScryKit/                   # pure-Swift API + types + downloader
│   │   ├── ScryEngine+Async.swift
│   │   └── ModelDownloader.swift
│   └── ScryApp/                   # SwiftUI views + Metal nebula renderer
│       ├── ScryApp.swift
│       ├── ChatView.swift
│       ├── ChatViewModel.swift
│       ├── NebulaView.swift
│       ├── NebulaScreen.swift
│       ├── NebulaRenderer.swift
│       ├── OrbitCamera.swift
│       ├── ModelPickerView.swift
│       └── Resources/NebulaShaders.metal
└── scripts/
    └── build-llama-xcframework.sh
```

The C++ sources are pulled in by reference from `qntx-plugins/scry/src/`
via Package.swift's `sources:` list — no copies, no fork. Mobile defines
`SCRY_NO_GRPC` so plugin.h skips its gRPC service classes.

## Setup (on a Mac)

### 1. Build the llama.xcframework

```bash
cd mobile
# Make sure llama.cpp is fetched. Easiest: run a desktop scry CMake
# configure once, which fetches it into qntx-plugins/scry/vendor/llama.cpp.
( cd ../qntx-plugins/scry && cmake -S . -B build )
./scripts/build-llama-xcframework.sh
```

This produces `mobile/Frameworks/llama.xcframework` containing iphoneos +
iphonesimulator slices. ~5 minutes on an M-series Mac.

### 2. Open the package in Xcode

```bash
open Package.swift
```

Xcode resolves the SwiftPM targets. Build the `ScryApp` library to confirm
it compiles.

### 3. Wrap in an Xcode app target

SwiftPM can't produce an iOS app binary directly — it produces libraries.
Create a thin Xcode app shell:

1. File → New → Project → iOS → App. Name it "Scry", language Swift,
   interface SwiftUI. Save it as `mobile/ScryHost/`.
2. In the project navigator, right-click the project → Add Package
   Dependencies → Add Local → select `mobile/`. Add the `ScryApp` library
   to the app target.
3. Replace the generated `App.swift` with:
   ```swift
   import SwiftUI
   import ScryApp

   @main struct ScryHostApp: App {
       var body: some Scene {
           WindowGroup { ScryRootView() }
       }
   }
   ```
4. Build settings:
   - **Enable Bitcode**: No
   - **Other Linker Flags**: `-ObjC`
   - **Deployment Target**: iOS 17.0+
5. Sign with your Apple Developer account or use automatic signing for
   personal-team device builds.

### 4. Run

Plug in an iPhone, select it as the run destination, hit ⌘R. First launch
opens the model picker — pick Qwen 2.5 0.5B for a fast demo (~400MB
download, fits in any context, runs hot only mildly).

## What's implemented vs not

**Working (in scaffold):**
- Obj-C++ bridge over InferenceEngine: load, chat, fork
- Swift `AsyncThrowingStream<TokenEvent>` API
- SwiftUI chat with confidence-colored token spans
- Tap-to-fan-out fullscreen view with PCA-positioned particles
- Tap-an-alternative → branch via `fork_and_generate`
- Orbit camera, pan/pinch/rotate gestures
- Pick buffer (R32Uint texture readback) for tap selection
- Curated + custom-URL model picker, on-device download with progress

**Not yet:**
- Vision / image attachments (`stream_chat_vision` exists but isn't wired)
- Animation polish on the fullscreen transition (currently a hard cut)
- Particle motion / orbit animation (static positions for now)
- Full vocab nebula (the desktop "all 50K tokens" view) — current renderer
  shows only top-K + chosen for the focused token. Adding the full vocab
  is a bigger pass: the desktop renderer's keyframe interpolation logic
  doesn't carry over and would need a Swift port.
- Sampler-stage observers (the SCO snapshots from desktop scry)
- Token rank navigation, ghost trails, scrub mode

## Versioning

Plugin version bumped to **0.37.10** to reflect the SCRY_NO_GRPC gating
in plugin.h. The plugin behavior on desktop is unchanged.

## Caveats

- This scaffold has **never been compiled** — it was written on Linux. The
  first Xcode build will surface issues. Most likely culprits:
  - Package.swift relative-path `sources:` may need to be a `path:` target
    rooted at the scry source dir with a binary symlink in mobile/.
  - llama.cpp's vendored cmake setup may need flags this script doesn't pass.
  - `vocab_projection.cpp` accesses llama.cpp private headers (PVH) — the
    iOS slice may need the same header search paths the desktop build uses.
- The renderer is intentionally simple. Don't expect feature parity with
  desktop scry's `metal_renderer.cpp` — that one has 280 lines of state we
  don't need for the fan-out demo.
