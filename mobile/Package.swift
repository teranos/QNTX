// swift-tools-version: 5.9
// Mobile-scry: standalone iOS show-off app for the Scry inference signals.
//
// This is a SwiftPM library. Wrap it in an Xcode app target for device builds.
// See README.md for the Xcode shell setup.
//
// The C++ sources live in `qntx-plugins/scry/src/` (parent of this directory).
// SwiftPM compiles them via the ScryCore target's `sources` list and the
// `cxxSettings` header search paths. llama.cpp is built as an XCFramework
// the user supplies — see README.md for the build command.

import PackageDescription

let package = Package(
    name: "ScryMobile",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [
        .library(name: "ScryKit", targets: ["ScryKit"]),
        .library(name: "ScryApp", targets: ["ScryApp"]),
    ],
    dependencies: [],
    targets: [
        // Pre-built llama.cpp + ggml as an XCFramework. The user produces this
        // on their Mac via `scripts/build-llama-xcframework.sh`. The path here
        // is relative to this Package.swift.
        .binaryTarget(
            name: "llama",
            path: "Frameworks/llama.xcframework"
        ),

        // Objective-C++ bridge over InferenceEngine. Pulls scry's C++ sources
        // directly from the plugin tree so any change there flows through.
        .target(
            name: "ScryCore",
            dependencies: ["llama"],
            path: "Sources/ScryCore",
            sources: [
                "ScryEngine.mm",
                // Scry C++ sources, referenced by relative path.
                "../../../qntx-plugins/scry/src/inference.cpp",
                "../../../qntx-plugins/scry/src/inference_fork.cpp",
                "../../../qntx-plugins/scry/src/vocab_projection.cpp",
                "../../../qntx-plugins/scry/src/vision.cpp",
            ],
            publicHeadersPath: "include",
            cxxSettings: [
                .define("SCRY_NO_GRPC"),
                .define("GGML_USE_METAL"),
                .headerSearchPath("../../../qntx-plugins/scry/src"),
                .headerSearchPath("../../../qntx-plugins/scry/vendor/glm"),
            ],
            linkerSettings: [
                .linkedFramework("Metal"),
                .linkedFramework("MetalKit"),
                .linkedFramework("Foundation"),
            ]
        ),

        // Pure-Swift API: async wrapper, model download, types.
        .target(
            name: "ScryKit",
            dependencies: ["ScryCore"],
            path: "Sources/ScryKit"
        ),

        // SwiftUI views + the Metal nebula renderer.
        .target(
            name: "ScryApp",
            dependencies: ["ScryKit"],
            path: "Sources/ScryApp",
            resources: [
                .process("Resources"),
            ]
        ),
    ],
    cxxLanguageStandard: .cxx17
)
