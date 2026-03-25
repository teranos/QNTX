import Foundation
import Metal
import CoreGraphics
import ImageIO

/// GPU-accelerated renderer using Metal compute and render pipelines.
///
/// This is the scaffold — the actual shader compilation, pipeline creation,
/// and render passes will be implemented per-visualization type.
final class MetalRenderer {
    private var device: MTLDevice?
    private var commandQueue: MTLCommandQueue?

    var isReady: Bool { device != nil && commandQueue != nil }

    var deviceName: String {
        device?.name ?? "none"
    }

    /// Initialize Metal device and command queue.
    func setup() throws {
        guard let device = MTLCreateSystemDefaultDevice() else {
            // Metal not available — headless Linux, CI, or no GPU
            return
        }
        self.device = device
        self.commandQueue = device.makeCommandQueue()
    }

    /// Release Metal resources.
    func teardown() {
        commandQueue = nil
        device = nil
    }

    /// Render visualization data to a PNG image.
    ///
    /// `data` is a JSON payload describing what to render.
    /// Returns PNG bytes, or nil on failure.
    func renderToImage(data: Data, width: Int, height: Int) -> Data? {
        guard let device = device, let commandQueue = commandQueue else {
            return nil
        }

        // Create a texture to render into
        let descriptor = MTLTextureDescriptor.texture2DDescriptor(
            pixelFormat: .rgba8Unorm,
            width: width,
            height: height,
            mipmapped: false
        )
        descriptor.usage = [.shaderWrite, .shaderRead]

        guard let texture = device.makeTexture(descriptor: descriptor) else {
            return nil
        }

        guard let commandBuffer = commandQueue.makeCommandBuffer() else {
            return nil
        }

        // TODO: Dispatch compute shader or render pass here based on visualization type.
        // For now, this is a clear-to-color proof that the Metal pipeline works end-to-end.

        commandBuffer.commit()
        commandBuffer.waitUntilCompleted()

        return textureToPNG(texture: texture, width: width, height: height)
    }

    /// Extract RGBA pixels from a Metal texture and encode as PNG.
    private func textureToPNG(texture: MTLTexture, width: Int, height: Int) -> Data? {
        let bytesPerRow = width * 4
        var pixels = [UInt8](repeating: 0, count: height * bytesPerRow)

        texture.getBytes(
            &pixels,
            bytesPerRow: bytesPerRow,
            from: MTLRegionMake2D(0, 0, width, height),
            mipmapLevel: 0
        )

        let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
        guard let context = CGContext(
            data: &pixels,
            width: width,
            height: height,
            bitsPerComponent: 8,
            bytesPerRow: bytesPerRow,
            space: colorSpace,
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        ) else {
            return nil
        }

        guard let cgImage = context.makeImage() else {
            return nil
        }

        let data = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(data, "public.png" as CFString, 1, nil) else {
            return nil
        }
        CGImageDestinationAddImage(destination, cgImage, nil)
        guard CGImageDestinationFinalize(destination) else {
            return nil
        }

        return data as Data
    }
}
