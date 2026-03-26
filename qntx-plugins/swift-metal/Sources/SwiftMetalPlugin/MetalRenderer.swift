import Foundation
import Metal
import CoreGraphics
import ImageIO
import simd

/// GPU-accelerated particle nebula renderer.
/// Takes probability distributions + 3D token positions and renders
/// point sprite particles with additive blending.
final class MetalRenderer: @unchecked Sendable {
    private var device: MTLDevice?
    private var commandQueue: MTLCommandQueue?
    private var computePipeline: MTLComputePipelineState?
    private var renderPipeline: MTLRenderPipelineState?

    // Cached buffers for vocab positions (fixed per model)
    private var positionsBuffer: MTLBuffer?
    private var vocabSize: Int = 0
    private var positionCenter: SIMD2<Float> = .zero
    private var positionExtent: Float = 1.0

    var isReady: Bool { device != nil && commandQueue != nil }

    var deviceName: String {
        device?.name ?? "none"
    }

    func setup() throws {
        guard let device = MTLCreateSystemDefaultDevice() else {
            return
        }
        self.device = device
        self.commandQueue = device.makeCommandQueue()

        try compilePipelines(device: device)
    }

    func teardown() {
        positionsBuffer = nil
        computePipeline = nil
        renderPipeline = nil
        commandQueue = nil
        device = nil
    }

    private func compilePipelines(device: MTLDevice) throws {
        // SPM doesn't compile .metal files into a default metallib.
        // Compile from source at runtime instead.
        let library: MTLLibrary
        do {
            library = try device.makeLibrary(source: metalShaderSource, options: nil)
        } catch {
            print("[swift-metal] Shader compilation failed: \(error)")
            return
        }

        // Compute pipeline
        guard let computeFunc = library.makeFunction(name: "particleCompute") else {
            print("[swift-metal] particleCompute function not found in shader library")
            return
        }
        computePipeline = try device.makeComputePipelineState(function: computeFunc)

        // Render pipeline
        guard let vertexFunc = library.makeFunction(name: "particleVertex"),
              let fragmentFunc = library.makeFunction(name: "particleFragment") else {
            print("[swift-metal] vertex/fragment functions not found in shader library")
            return
        }

        let rpd = MTLRenderPipelineDescriptor()
        rpd.vertexFunction = vertexFunc
        rpd.fragmentFunction = fragmentFunc
        rpd.colorAttachments[0].pixelFormat = .rgba16Float
        // Additive blending
        rpd.colorAttachments[0].isBlendingEnabled = true
        rpd.colorAttachments[0].sourceRGBBlendFactor = .one
        rpd.colorAttachments[0].destinationRGBBlendFactor = .one
        rpd.colorAttachments[0].sourceAlphaBlendFactor = .one
        rpd.colorAttachments[0].destinationAlphaBlendFactor = .one

        renderPipeline = try device.makeRenderPipelineState(descriptor: rpd)
    }

    /// Set the fixed 3D positions for all vocab tokens.
    /// positions: flat array of n_vocab × 3 floats.
    func setVocabPositions(_ positions: [Float]) {
        guard let device = device else { return }
        vocabSize = positions.count / 3
        positionsBuffer = device.makeBuffer(
            bytes: positions,
            length: positions.count * MemoryLayout<Float>.size,
            options: .storageModeShared
        )

        // Compute bounding box for auto-fitting MVP
        var minX: Float = .infinity, maxX: Float = -.infinity
        var minY: Float = .infinity, maxY: Float = -.infinity
        for i in 0..<vocabSize {
            let x = positions[i * 3]
            let y = positions[i * 3 + 1]
            minX = min(minX, x); maxX = max(maxX, x)
            minY = min(minY, y); maxY = max(maxY, y)
        }
        positionCenter = SIMD2<Float>((minX + maxX) / 2, (minY + maxY) / 2)
        positionExtent = max(maxX - minX, maxY - minY) / 2
        if positionExtent < 1e-6 { positionExtent = 1.0 }
    }

    /// Render a probability distribution as a particle nebula.
    /// probabilities: vocabSize floats (softmax output).
    /// Returns PNG data.
    func renderNebula(probabilities: [Float], width: Int, height: Int) -> Data? {
        guard let device = device,
              let commandQueue = commandQueue,
              let computePipeline = computePipeline,
              let renderPipeline = renderPipeline,
              let positionsBuffer = positionsBuffer else {
            return nil
        }

        let n = vocabSize
        guard probabilities.count == n else {
            print("[swift-metal] probability count \(probabilities.count) != vocab size \(n)")
            return nil
        }

        // Create buffers
        guard let probBuffer = device.makeBuffer(
            bytes: probabilities,
            length: n * MemoryLayout<Float>.size,
            options: .storageModeShared
        ) else { return nil }

        // Particle buffer: position(3) + color(4) + size(1) = 32 bytes per particle
        let particleStride = 32
        guard let particleBuffer = device.makeBuffer(
            length: n * particleStride,
            options: .storageModeShared
        ) else { return nil }

        var vocabSizeU: UInt32 = UInt32(n)

        guard let commandBuffer = commandQueue.makeCommandBuffer() else { return nil }

        // --- Compute pass: probabilities + positions → particles ---
        guard let computeEncoder = commandBuffer.makeComputeCommandEncoder() else { return nil }
        computeEncoder.setComputePipelineState(computePipeline)
        computeEncoder.setBuffer(probBuffer, offset: 0, index: 0)
        computeEncoder.setBuffer(positionsBuffer, offset: 0, index: 1)
        computeEncoder.setBuffer(particleBuffer, offset: 0, index: 2)
        computeEncoder.setBytes(&vocabSizeU, length: MemoryLayout<UInt32>.size, index: 3)

        let threadGroupSize = min(computePipeline.maxTotalThreadsPerThreadgroup, 256)
        let gridSize = MTLSize(width: n, height: 1, depth: 1)
        let threadGroup = MTLSize(width: threadGroupSize, height: 1, depth: 1)
        computeEncoder.dispatchThreads(gridSize, threadsPerThreadgroup: threadGroup)
        computeEncoder.endEncoding()

        // --- Render pass: particles → texture ---
        // HDR texture for additive blending
        let texDesc = MTLTextureDescriptor.texture2DDescriptor(
            pixelFormat: .rgba16Float,
            width: width,
            height: height,
            mipmapped: false
        )
        texDesc.usage = [.renderTarget, .shaderRead]
        guard let hdrTexture = device.makeTexture(descriptor: texDesc) else { return nil }

        let rpd = MTLRenderPassDescriptor()
        rpd.colorAttachments[0].texture = hdrTexture
        rpd.colorAttachments[0].loadAction = .clear
        rpd.colorAttachments[0].clearColor = MTLClearColor(red: 0.02, green: 0.01, blue: 0.03, alpha: 1.0)
        rpd.colorAttachments[0].storeAction = .store

        guard let renderEncoder = commandBuffer.makeRenderCommandEncoder(descriptor: rpd) else { return nil }
        renderEncoder.setRenderPipelineState(renderPipeline)
        renderEncoder.setVertexBuffer(particleBuffer, offset: 0, index: 0)

        // MVP matrix: simple orthographic projection mapping position range to clip space
        let mvp = buildMVP(width: Float(width), height: Float(height))
        var mvpMatrix = mvp
        renderEncoder.setVertexBytes(&mvpMatrix, length: MemoryLayout<simd_float4x4>.size, index: 1)

        renderEncoder.drawPrimitives(type: .point, vertexStart: 0, vertexCount: n)
        renderEncoder.endEncoding()

        commandBuffer.commit()
        commandBuffer.waitUntilCompleted()

        // Tonemap HDR → LDR and export as PNG
        return hdrTextureToPNG(device: device, texture: hdrTexture, width: width, height: height)
    }

    /// Build an orthographic MVP that auto-fits to position bounding box.
    private func buildMVP(width: Float, height: Float) -> simd_float4x4 {
        let scale: Float = 0.9 / positionExtent  // fit 90% of viewport
        let aspect = width / height
        let cx = positionCenter.x
        let cy = positionCenter.y

        // Scale then translate to center
        let s = simd_float4x4(
            SIMD4<Float>(scale / aspect, 0, 0, 0),
            SIMD4<Float>(0, scale, 0, 0),
            SIMD4<Float>(0, 0, scale, 0),
            SIMD4<Float>(0, 0, 0, 1)
        )
        let t = simd_float4x4(
            SIMD4<Float>(1, 0, 0, 0),
            SIMD4<Float>(0, 1, 0, 0),
            SIMD4<Float>(0, 0, 1, 0),
            SIMD4<Float>(-cx, -cy, 0, 1)
        )
        return s * t
    }

    /// Convert HDR float16 texture to tonemapped LDR PNG.
    private func hdrTextureToPNG(device: MTLDevice, texture: MTLTexture, width: Int, height: Int) -> Data? {
        let bytesPerPixel = 8 // rgba16Float = 4 × 2 bytes
        let bytesPerRow = width * bytesPerPixel
        var hdrPixels = [UInt16](repeating: 0, count: width * height * 4)

        texture.getBytes(
            &hdrPixels,
            bytesPerRow: bytesPerRow,
            from: MTLRegionMake2D(0, 0, width, height),
            mipmapLevel: 0
        )

        // Simple tonemap: HDR float16 → sRGB uint8
        let ldrBytesPerRow = width * 4
        var ldrPixels = [UInt8](repeating: 0, count: height * ldrBytesPerRow)

        for y in 0..<height {
            for x in 0..<width {
                let srcIdx = (y * width + x) * 4
                let dstIdx = (y * width + x) * 4

                // float16 → float32
                let r = float16to32(hdrPixels[srcIdx])
                let g = float16to32(hdrPixels[srcIdx + 1])
                let b = float16to32(hdrPixels[srcIdx + 2])

                // Reinhard tonemap + gamma
                let tr = powf(r / (1.0 + r), 1.0 / 2.2)
                let tg = powf(g / (1.0 + g), 1.0 / 2.2)
                let tb = powf(b / (1.0 + b), 1.0 / 2.2)

                ldrPixels[dstIdx] = UInt8(min(max(tr * 255.0, 0), 255))
                ldrPixels[dstIdx + 1] = UInt8(min(max(tg * 255.0, 0), 255))
                ldrPixels[dstIdx + 2] = UInt8(min(max(tb * 255.0, 0), 255))
                ldrPixels[dstIdx + 3] = 255
            }
        }

        let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
        guard let context = CGContext(
            data: &ldrPixels,
            width: width,
            height: height,
            bitsPerComponent: 8,
            bytesPerRow: ldrBytesPerRow,
            space: colorSpace,
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        ) else { return nil }

        guard let cgImage = context.makeImage() else { return nil }

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

    /// Convert IEEE 754 half-precision float to single-precision.
    private func float16to32(_ h: UInt16) -> Float {
        let sign = UInt32(h >> 15) & 1
        let exp = UInt32(h >> 10) & 0x1F
        let frac = UInt32(h) & 0x3FF

        if exp == 0 {
            if frac == 0 { return sign == 1 ? -0.0 : 0.0 }
            // Denorm
            var f = Float(frac) / 1024.0
            f *= powf(2.0, -14.0)
            return sign == 1 ? -f : f
        } else if exp == 31 {
            return frac == 0 ? (sign == 1 ? -.infinity : .infinity) : .nan
        }

        let bits = (sign << 31) | ((exp + 112) << 23) | (frac << 13)
        return Float(bitPattern: bits)
    }

    // Keep the old renderToImage for backwards compat during transition
    func renderToImage(data: Data, width: Int, height: Int) -> Data? {
        // Generate test data if no real data provided
        return renderTestNebula(width: width, height: height)
    }

    /// Render with hardcoded test data: bimodal distribution + random positions.
    func renderTestNebula(width: Int, height: Int) -> Data? {
        let n = 128256  // Llama 3.2 vocab size

        // Generate test positions if not loaded
        if positionsBuffer == nil {
            var positions = [Float](repeating: 0, count: n * 3)
            var rng = SystemRandomNumberGenerator()
            for i in 0..<n {
                // Gaussian-ish distribution via Box-Muller
                let u1 = Float.random(in: 0.001...1.0, using: &rng)
                let u2 = Float.random(in: 0...1.0, using: &rng)
                let u3 = Float.random(in: 0.001...1.0, using: &rng)
                let u4 = Float.random(in: 0...1.0, using: &rng)
                positions[i * 3] = sqrtf(-2.0 * logf(u1)) * cosf(2.0 * .pi * u2) * 10.0
                positions[i * 3 + 1] = sqrtf(-2.0 * logf(u1)) * sinf(2.0 * .pi * u2) * 10.0
                positions[i * 3 + 2] = sqrtf(-2.0 * logf(u3)) * cosf(2.0 * .pi * u4) * 10.0
            }
            setVocabPositions(positions)
        }

        // Bimodal test distribution: two peaks with noise floor
        var probs = [Float](repeating: 1e-7, count: n)
        // Peak 1: tokens 1000-1100 get moderate probability
        for i in 1000..<1100 {
            probs[i] = 0.005
        }
        // Peak 2: token 5000 gets high probability
        probs[5000] = 0.3
        // Normalize
        let sum = probs.reduce(0, +)
        for i in 0..<n { probs[i] /= sum }

        return renderNebula(probabilities: probs, width: width, height: height)
    }
}
