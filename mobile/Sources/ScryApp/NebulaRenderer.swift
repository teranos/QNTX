// Metal renderer for the token-fan-out view. Receives a focus token plus
// its top-K alternatives, renders them as billboarded points at their PCA
// positions with size and alpha driven by probability. The chosen token
// gets a brighter, larger sprite. Camera is orbit-style.

import Foundation
import Metal
import MetalKit
import ScryKit
import simd

public struct ParticleSpec: Sendable {
    public let tokenId: Int32
    public let position: SIMD3<Float>   // from vocab PCA xyz
    public let color: SIMD3<Float>      // from vocab PCA rgb
    public let alpha: Float             // 0..1
    public let size: Float              // world units
    public init(tokenId: Int32, position: SIMD3<Float>, color: SIMD3<Float>,
                alpha: Float, size: Float) {
        self.tokenId = tokenId
        self.position = position
        self.color = color
        self.alpha = alpha
        self.size = size
    }
}

@MainActor
public final class NebulaRenderer: NSObject, MTKViewDelegate {
    public let device: MTLDevice
    private let commandQueue: MTLCommandQueue
    private let renderPipeline: MTLRenderPipelineState
    private let pickPipeline: MTLRenderPipelineState

    private var particleBuffer: MTLBuffer?
    private var particleCount: Int = 0

    // Pick texture — written when pickRequested is set, read back next frame.
    private var pickTexture: MTLTexture?
    private var pickRequest: (x: Int, y: Int)?
    private var pendingPickResult: Int32??

    public var camera = OrbitCamera()
    public var globalAlpha: Float = 1.0
    public var pointScale: Float = 80.0

    public init?(device: MTLDevice = MTLCreateSystemDefaultDevice()!) {
        self.device = device
        guard let queue = device.makeCommandQueue() else { return nil }
        self.commandQueue = queue

        // Load Shaders.metal from the ScryApp module bundle.
        guard let library = try? device.makeDefaultLibrary(bundle: .module) else {
            return nil
        }

        let vfn = library.makeFunction(name: "nebula_vertex")!
        let ffn = library.makeFunction(name: "nebula_fragment")!
        let pickfn = library.makeFunction(name: "pick_fragment")!

        let desc = MTLRenderPipelineDescriptor()
        desc.vertexFunction = vfn
        desc.fragmentFunction = ffn
        desc.colorAttachments[0].pixelFormat = .bgra8Unorm
        desc.colorAttachments[0].isBlendingEnabled = true
        desc.colorAttachments[0].rgbBlendOperation = .add
        desc.colorAttachments[0].alphaBlendOperation = .add
        desc.colorAttachments[0].sourceRGBBlendFactor = .one
        desc.colorAttachments[0].sourceAlphaBlendFactor = .one
        desc.colorAttachments[0].destinationRGBBlendFactor = .one
        desc.colorAttachments[0].destinationAlphaBlendFactor = .one  // additive

        guard let pipeline = try? device.makeRenderPipelineState(descriptor: desc) else {
            return nil
        }
        self.renderPipeline = pipeline

        let pickDesc = MTLRenderPipelineDescriptor()
        pickDesc.vertexFunction = vfn
        pickDesc.fragmentFunction = pickfn
        pickDesc.colorAttachments[0].pixelFormat = .r32Uint
        guard let pickPL = try? device.makeRenderPipelineState(descriptor: pickDesc) else {
            return nil
        }
        self.pickPipeline = pickPL

        super.init()
    }

    public func setParticles(_ particles: [ParticleSpec]) {
        particleCount = particles.count
        guard particleCount > 0 else { particleBuffer = nil; return }
        let stride = MemoryLayout<ParticleRaw>.stride
        let raws = particles.map { ParticleRaw($0) }
        particleBuffer = device.makeBuffer(bytes: raws, length: stride * particleCount, options: [.storageModeShared])
    }

    public func requestPick(at point: CGPoint, in viewSize: CGSize, completion: @escaping (Int32?) -> Void) {
        // Convert CGPoint to texture pixels (assuming pickTexture matches viewSize × scale).
        let px = Int(point.x)
        let py = Int(point.y)
        pickRequest = (px, py)
        pendingPickResult = .some(.none) // marker
        // The result is delivered on the next frame after the GPU finishes.
        // Caller polls via consumePickResult() after one frame; here we just
        // record the request — see consumePickResult below.
        _pickCompletion = completion
    }

    private var _pickCompletion: ((Int32?) -> Void)?

    // MARK: MTKViewDelegate

    public func mtkView(_ view: MTKView, drawableSizeWillChange size: CGSize) {
        ensurePickTexture(size: size)
    }

    private func ensurePickTexture(size: CGSize) {
        let w = max(1, Int(size.width))
        let h = max(1, Int(size.height))
        if let t = pickTexture, t.width == w, t.height == h { return }
        let desc = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: .r32Uint,
                                                             width: w, height: h,
                                                             mipmapped: false)
        desc.usage = [.renderTarget, .shaderRead]
        desc.storageMode = .private
        pickTexture = device.makeTexture(descriptor: desc)
    }

    public func draw(in view: MTKView) {
        guard let drawable = view.currentDrawable,
              let rpd = view.currentRenderPassDescriptor else { return }
        ensurePickTexture(size: view.drawableSize)

        camera.tick()

        let aspect = Float(view.drawableSize.width / max(1, view.drawableSize.height))
        var uniforms = CameraUniformsRaw(
            mvp: camera.mvp(aspect: aspect),
            pointScale: pointScale,
            globalAlpha: globalAlpha,
            _pad0: 0, _pad1: 0
        )

        guard let cmd = commandQueue.makeCommandBuffer() else { return }

        // Color pass.
        if let buf = particleBuffer, particleCount > 0 {
            if let enc = cmd.makeRenderCommandEncoder(descriptor: rpd) {
                enc.setRenderPipelineState(renderPipeline)
                enc.setVertexBuffer(buf, offset: 0, index: 0)
                enc.setVertexBytes(&uniforms, length: MemoryLayout<CameraUniformsRaw>.stride, index: 1)
                enc.drawPrimitives(type: .point, vertexStart: 0, vertexCount: particleCount)
                enc.endEncoding()
            }
        } else if let enc = cmd.makeRenderCommandEncoder(descriptor: rpd) {
            // Empty pass to clear the frame.
            enc.endEncoding()
        }

        // Pick pass: only when requested.
        var pickBuffer: MTLBuffer?
        if let req = pickRequest, let pickTex = pickTexture, let buf = particleBuffer, particleCount > 0 {
            let pickRPD = MTLRenderPassDescriptor()
            pickRPD.colorAttachments[0].texture = pickTex
            pickRPD.colorAttachments[0].loadAction = .clear
            pickRPD.colorAttachments[0].clearColor = MTLClearColor(red: 0, green: 0, blue: 0, alpha: 0)
            pickRPD.colorAttachments[0].storeAction = .store
            if let enc = cmd.makeRenderCommandEncoder(descriptor: pickRPD) {
                enc.setRenderPipelineState(pickPipeline)
                enc.setVertexBuffer(buf, offset: 0, index: 0)
                enc.setVertexBytes(&uniforms, length: MemoryLayout<CameraUniformsRaw>.stride, index: 1)
                enc.drawPrimitives(type: .point, vertexStart: 0, vertexCount: particleCount)
                enc.endEncoding()
            }
            // Read back the single pixel.
            pickBuffer = device.makeBuffer(length: MemoryLayout<UInt32>.stride, options: [.storageModeShared])
            if let pb = pickBuffer, let blit = cmd.makeBlitCommandEncoder() {
                let x = max(0, min(req.x, pickTex.width - 1))
                let y = max(0, min(req.y, pickTex.height - 1))
                blit.copy(from: pickTex,
                          sourceSlice: 0, sourceLevel: 0,
                          sourceOrigin: MTLOrigin(x: x, y: y, z: 0),
                          sourceSize: MTLSize(width: 1, height: 1, depth: 1),
                          to: pb,
                          destinationOffset: 0,
                          destinationBytesPerRow: 4,
                          destinationBytesPerImage: 4)
                blit.endEncoding()
            }
            pickRequest = nil
        }

        cmd.present(drawable)

        if let pb = pickBuffer, let completion = _pickCompletion {
            cmd.addCompletedHandler { _ in
                let raw = pb.contents().load(as: UInt32.self)
                let result: Int32? = raw == 0 ? nil : Int32(raw - 1)
                DispatchQueue.main.async { completion(result) }
            }
            _pickCompletion = nil
        }

        cmd.commit()
    }
}

// MARK: - Raw structs matching Metal layout

private struct ParticleRaw {
    var px: Float; var py: Float; var pz: Float
    var cr: Float; var cg: Float; var cb: Float
    var alpha: Float
    var size: Float
    var tokenId: Int32

    init(_ p: ParticleSpec) {
        px = p.position.x; py = p.position.y; pz = p.position.z
        cr = p.color.x; cg = p.color.y; cb = p.color.z
        alpha = p.alpha
        size = p.size
        tokenId = p.tokenId
    }
}

private struct CameraUniformsRaw {
    var mvp: simd_float4x4
    var pointScale: Float
    var globalAlpha: Float
    var _pad0: Float
    var _pad1: Float
}
