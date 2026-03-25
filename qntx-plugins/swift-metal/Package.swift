// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "qntx-swift-metal",
    platforms: [
        .macOS(.v14)
    ],
    dependencies: [
        .package(url: "https://github.com/grpc/grpc-swift.git", from: "1.23.0"),
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.28.0"),
    ],
    targets: [
        .executableTarget(
            name: "qntx-swift-metal-plugin",
            dependencies: [
                .product(name: "GRPC", package: "grpc-swift"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "Sources/SwiftMetalPlugin",
            linkerSettings: [
                .linkedFramework("Metal"),
                .linkedFramework("MetalKit"),
                .linkedFramework("CoreGraphics"),
                .linkedFramework("ImageIO"),
            ]
        ),
    ]
)
