#include <metal_stdlib>
using namespace metal;

// --- Particle nebula ---

struct Particle {
    float3 position;
    float4 color;
    float size;
};

struct VertexOut {
    float4 position [[position]];
    float4 color;
    float pointSize [[point_size]];
};

kernel void particleCompute(
    device const float* probabilities [[buffer(0)]],
    device const float* positions     [[buffer(1)]],
    device Particle* particles        [[buffer(2)]],
    constant uint& vocabSize          [[buffer(3)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= vocabSize) return;

    float prob = probabilities[id];
    float3 pos = float3(positions[id * 3], positions[id * 3 + 1], positions[id * 3 + 2]);

    float threshold = 1e-5;
    float visible = step(threshold, prob);
    float logProb = visible * saturate((log2(prob + 1e-10) + 20.0) / 20.0);

    float3 lo = float3(0.15, 0.05, 0.0);
    float3 mid = float3(0.9, 0.5, 0.05);
    float3 hi = float3(1.0, 0.95, 0.85);
    float3 rgb = mix(lo, mid, saturate(logProb * 2.0));
    rgb = mix(rgb, hi, saturate(logProb * 2.0 - 1.0));

    Particle p;
    p.position = pos;
    p.color = float4(rgb * visible, logProb * visible);
    p.size = visible * (2.0 + logProb * 12.0);

    particles[id] = p;
}

kernel void particleComputeLerp(
    device const float* probA         [[buffer(0)]],
    device const float* probB         [[buffer(1)]],
    device const float* positions     [[buffer(2)]],
    device Particle* particles        [[buffer(3)]],
    constant uint& vocabSize          [[buffer(4)]],
    constant float& t                 [[buffer(5)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= vocabSize) return;

    float prob = mix(probA[id], probB[id], t);
    float3 pos = float3(positions[id * 3], positions[id * 3 + 1], positions[id * 3 + 2]);

    float threshold = 1e-5;
    float visible = step(threshold, prob);
    float logProb = visible * saturate((log2(prob + 1e-10) + 20.0) / 20.0);

    float3 lo = float3(0.15, 0.05, 0.0);
    float3 mid = float3(0.9, 0.5, 0.05);
    float3 hi = float3(1.0, 0.95, 0.85);
    float3 rgb = mix(lo, mid, saturate(logProb * 2.0));
    rgb = mix(rgb, hi, saturate(logProb * 2.0 - 1.0));

    Particle p;
    p.position = pos;
    p.color = float4(rgb * visible, logProb * visible);
    p.size = visible * (2.0 + logProb * 12.0);

    particles[id] = p;
}

vertex VertexOut particleVertex(
    device const Particle* particles [[buffer(0)]],
    constant float4x4& mvp          [[buffer(1)]],
    uint vid [[vertex_id]]
) {
    Particle p = particles[vid];

    VertexOut out;
    out.position = mvp * float4(p.position, 1.0);
    out.color = p.color;
    out.pointSize = p.size;
    return out;
}

fragment float4 particleFragment(
    VertexOut in [[stage_in]],
    float2 pointCoord [[point_coord]]
) {
    float dist = length(pointCoord - float2(0.5));
    float alpha = 1.0 - smoothstep(0.3, 0.5, dist);

    return float4(in.color.rgb * alpha * in.color.a, alpha * in.color.a);
}

// --- Generation trail ---

struct TrailVertexOut {
    float4 position [[position]];
    float3 color;
    float alpha;
};

vertex TrailVertexOut trailVertex(
    device const float* positions [[buffer(0)]],
    constant float4x4& mvp       [[buffer(1)]],
    constant uint& trailCount    [[buffer(2)]],
    constant int& scrubIndex     [[buffer(3)]],
    constant float& driftStep    [[buffer(4)]],
    uint vid [[vertex_id]]
) {
    float3 pos = float3(positions[vid * 3], positions[vid * 3 + 1], positions[vid * 3 + 2]);

    // Drift: newest point (head) sits on the nebula, older points trail behind.
    int headIndex = (scrubIndex >= 0) ? scrubIndex : int(trailCount - 1);
    pos.x += float(int(vid) - headIndex) * driftStep;

    TrailVertexOut out;
    out.position = mvp * float4(pos, 1.0);

    if (scrubIndex < 0) {
        // Live mode — newest brightest, oldest fades (warm white)
        float age = float(trailCount - 1 - vid) / max(1.0, float(trailCount - 1));
        out.alpha = mix(1.0, 0.05, age * age);
        out.color = float3(1.0, 0.85, 0.6);
    } else {
        // Scrub mode — warm up to scrub point, cool/dim beyond
        int si = scrubIndex;
        if (int(vid) <= si) {
            float age = float(si - int(vid)) / max(1.0, float(si));
            out.alpha = mix(1.0, 0.1, age * age);
            out.color = float3(1.0, 0.85, 0.6);  // warm
        } else {
            float future = float(int(vid) - si) / max(1.0, float(int(trailCount) - 1 - si));
            out.alpha = mix(0.15, 0.03, future);
            out.color = float3(0.4, 0.5, 0.7);   // cool blue
        }
    }

    return out;
}

fragment float4 trailFragment(TrailVertexOut in [[stage_in]]) {
    return float4(in.color * in.alpha * 0.7, 1.0);
}
