// SwiftUI host for the Metal nebula. Wraps an MTKView and a NebulaRenderer,
// hooks gestures into the renderer's OrbitCamera, exposes a tap-to-pick
// callback that returns the tokenId of the touched particle.

import SwiftUI
import MetalKit

#if canImport(UIKit)
import UIKit

public struct NebulaView: UIViewRepresentable {
    @Binding public var particles: [ParticleSpec]
    public var onPick: (Int32) -> Void

    public init(particles: Binding<[ParticleSpec]>, onPick: @escaping (Int32) -> Void) {
        self._particles = particles
        self.onPick = onPick
    }

    public func makeCoordinator() -> Coordinator {
        Coordinator(onPick: onPick)
    }

    public func makeUIView(context: Context) -> MTKView {
        let view = MTKView()
        view.device = MTLCreateSystemDefaultDevice()
        view.clearColor = MTLClearColor(red: 0.02, green: 0.02, blue: 0.04, alpha: 1.0)
        view.colorPixelFormat = .bgra8Unorm
        view.preferredFramesPerSecond = 60
        view.isPaused = false
        view.enableSetNeedsDisplay = false
        view.backgroundColor = .black

        let renderer = NebulaRenderer(device: view.device!)
        view.delegate = renderer
        context.coordinator.renderer = renderer
        context.coordinator.attachGestures(to: view)
        return view
    }

    public func updateUIView(_ uiView: MTKView, context: Context) {
        context.coordinator.renderer?.setParticles(particles)
    }

    @MainActor
    public final class Coordinator: NSObject {
        var renderer: NebulaRenderer?
        let onPick: (Int32) -> Void
        private var lastPan: CGPoint = .zero
        private var lastPinch: CGFloat = 1.0

        init(onPick: @escaping (Int32) -> Void) {
            self.onPick = onPick
        }

        func attachGestures(to view: UIView) {
            let pan = UIPanGestureRecognizer(target: self, action: #selector(handlePan(_:)))
            view.addGestureRecognizer(pan)
            let pinch = UIPinchGestureRecognizer(target: self, action: #selector(handlePinch(_:)))
            view.addGestureRecognizer(pinch)
            let tap = UITapGestureRecognizer(target: self, action: #selector(handleTap(_:)))
            view.addGestureRecognizer(tap)
            // Two-finger pan = camera pan (translate target).
            let twoFinger = UIPanGestureRecognizer(target: self, action: #selector(handleTwoFingerPan(_:)))
            twoFinger.minimumNumberOfTouches = 2
            twoFinger.maximumNumberOfTouches = 2
            view.addGestureRecognizer(twoFinger)
            pan.require(toFail: twoFinger)
        }

        @objc func handlePan(_ g: UIPanGestureRecognizer) {
            guard let renderer = renderer else { return }
            let translation = g.translation(in: g.view)
            let dx = Float(translation.x - lastPan.x)
            let dy = Float(translation.y - lastPan.y)
            renderer.camera.rotate(deltaYaw: dx * 0.005, deltaPitch: -dy * 0.005)
            lastPan = translation
            if g.state == .ended || g.state == .cancelled { lastPan = .zero }
            if g.state == .began { lastPan = translation }
        }

        @objc func handleTwoFingerPan(_ g: UIPanGestureRecognizer) {
            guard let renderer = renderer else { return }
            let t = g.translation(in: g.view)
            let dx = Float(t.x) * 0.005 * renderer.camera.distance
            let dy = Float(t.y) * 0.005 * renderer.camera.distance
            renderer.camera.pan(deltaX: -dx, deltaY: dy)
            g.setTranslation(.zero, in: g.view)
        }

        @objc func handlePinch(_ g: UIPinchGestureRecognizer) {
            guard let renderer = renderer else { return }
            if g.state == .began { lastPinch = 1.0 }
            let factor = Float(lastPinch / g.scale)
            renderer.camera.zoom(by: factor)
            lastPinch = g.scale
            if g.state == .ended { lastPinch = 1.0 }
        }

        @objc func handleTap(_ g: UITapGestureRecognizer) {
            guard let renderer = renderer, let view = g.view as? MTKView else { return }
            let location = g.location(in: view)
            let scale = view.contentScaleFactor
            let pixelPoint = CGPoint(x: location.x * scale, y: location.y * scale)
            renderer.requestPick(at: pixelPoint, in: view.drawableSize) { [weak self] tokenId in
                guard let id = tokenId else { return }
                self?.onPick(id)
            }
        }
    }
}
#endif
