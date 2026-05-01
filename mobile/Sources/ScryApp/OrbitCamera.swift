// Orbit camera with touch-friendly inputs. Spherical coordinates around a
// target point: yaw (around Y), pitch (around X), distance. Smooth lerp
// toward target values so flicks decay naturally and fly-to animates.

import Foundation
import simd

public final class OrbitCamera {
    public var target: SIMD3<Float> = .zero
    public var yaw: Float = 0
    public var pitch: Float = 0.2
    public var distance: Float = 3.0

    public var targetYaw: Float = 0
    public var targetPitch: Float = 0.2
    public var targetDistance: Float = 3.0
    public var targetCenter: SIMD3<Float> = .zero

    public var lerpSpeed: Float = 8.0
    public var fov: Float = 60.0
    public var nearPlane: Float = 0.01
    public var farPlane: Float = 100.0

    private var lastTick: CFTimeInterval = CACurrentMediaTime()

    public init() {}

    // Touch handlers — feed gesture velocities/deltas in here.
    public func rotate(deltaYaw: Float, deltaPitch: Float) {
        targetYaw += deltaYaw
        targetPitch += deltaPitch
        targetPitch = max(-1.5, min(1.5, targetPitch))
    }

    public func zoom(by factor: Float) {
        targetDistance = max(0.3, min(20.0, targetDistance * factor))
    }

    public func pan(deltaX: Float, deltaY: Float) {
        let r = right()
        let u = up()
        targetCenter += r * deltaX + u * deltaY
    }

    public func flyTo(target: SIMD3<Float>, distance: Float? = nil) {
        targetCenter = target
        if let d = distance { targetDistance = d }
    }

    public func reset() {
        targetCenter = .zero
        targetYaw = 0
        targetPitch = 0.2
        targetDistance = 3.0
    }

    public func tick() {
        let now = CACurrentMediaTime()
        let dt = Float(min(0.1, max(0.0001, now - lastTick)))
        lastTick = now
        let t = 1.0 - exp(-lerpSpeed * dt)
        yaw += (targetYaw - yaw) * t
        pitch += (targetPitch - pitch) * t
        distance += (targetDistance - distance) * t
        target += (targetCenter - target) * t
    }

    public func position() -> SIMD3<Float> {
        let cp = cos(pitch), sp = sin(pitch)
        let cy = cos(yaw),   sy = sin(yaw)
        let dir = SIMD3<Float>(sy * cp, sp, -cy * cp)
        return target - dir * distance
    }

    public func forward() -> SIMD3<Float> {
        normalize(target - position())
    }

    public func right() -> SIMD3<Float> {
        normalize(cross(forward(), SIMD3<Float>(0, 1, 0)))
    }

    public func up() -> SIMD3<Float> {
        normalize(cross(right(), forward()))
    }

    public func mvp(aspect: Float) -> simd_float4x4 {
        let p = perspective(fovDegrees: fov, aspect: aspect, near: nearPlane, far: farPlane)
        let v = lookAt(eye: position(), center: target, up: SIMD3<Float>(0, 1, 0))
        return p * v
    }
}

// MARK: - Math

func perspective(fovDegrees: Float, aspect: Float, near: Float, far: Float) -> simd_float4x4 {
    let fovRadians = fovDegrees * .pi / 180.0
    let y = 1.0 / tan(fovRadians * 0.5)
    let x = y / aspect
    let z = far / (far - near)
    return simd_float4x4(rows: [
        SIMD4<Float>(x, 0, 0, 0),
        SIMD4<Float>(0, y, 0, 0),
        SIMD4<Float>(0, 0, z, -near * z),
        SIMD4<Float>(0, 0, 1, 0),
    ])
}

func lookAt(eye: SIMD3<Float>, center: SIMD3<Float>, up: SIMD3<Float>) -> simd_float4x4 {
    let f = normalize(center - eye)
    let s = normalize(cross(f, up))
    let u = cross(s, f)
    return simd_float4x4(rows: [
        SIMD4<Float>(s.x, s.y, s.z, -dot(s, eye)),
        SIMD4<Float>(u.x, u.y, u.z, -dot(u, eye)),
        SIMD4<Float>(f.x, f.y, f.z, -dot(f, eye)),
        SIMD4<Float>(0, 0, 0, 1),
    ])
}
