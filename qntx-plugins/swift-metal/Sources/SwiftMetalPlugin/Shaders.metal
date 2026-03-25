#include <metal_stdlib>
using namespace metal;

// Per-particle data output by compute shader, consumed by vertex shader
struct Particle {
    float3 position;
    float4 color;
    float size;
};

// Vertex output to fragment shader
struct VertexOut {
    float4 position [[position]];
    float4 color;
    float pointSize [[point_size]];
};

// Compute shader: transform probabilities + positions into renderable particles.
// Probability controls brightness/size, position is the 3D PCA coordinate.
kernel void particleCompute(
    device const float* probabilities [[buffer(0)]],   // n_vocab floats
    device const float* positions     [[buffer(1)]],   // n_vocab × 3 floats
    device Particle* particles        [[buffer(2)]],   // output: n_vocab particles
    constant uint& vocabSize          [[buffer(3)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= vocabSize) return;

    float prob = probabilities[id];
    float3 pos = float3(positions[id * 3], positions[id * 3 + 1], positions[id * 3 + 2]);

    // Probability → visual mapping
    // Below threshold: invisible (size 0, GPU culls)
    float threshold = 1e-5;
    float visible = step(threshold, prob);

    // Log scale for better dynamic range (probabilities span many orders of magnitude)
    float logProb = visible * saturate((log2(prob + 1e-10) + 20.0) / 20.0);

    // Color: dark → amber → white based on probability
    float3 lo = float3(0.15, 0.05, 0.0);   // dark ember
    float3 mid = float3(0.9, 0.5, 0.05);    // amber
    float3 hi = float3(1.0, 0.95, 0.85);    // near-white
    float3 rgb = mix(lo, mid, saturate(logProb * 2.0));
    rgb = mix(rgb, hi, saturate(logProb * 2.0 - 1.0));

    Particle p;
    p.position = pos;
    p.color = float4(rgb * visible, logProb * visible);
    p.size = visible * (2.0 + logProb * 12.0);

    particles[id] = p;
}

// Vertex shader: project 3D particle positions to 2D screen space.
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

// Fragment shader: soft circular point sprite with additive blending.
fragment float4 particleFragment(
    VertexOut in [[stage_in]],
    float2 pointCoord [[point_coord]]
) {
    // Soft circle falloff
    float dist = length(pointCoord - float2(0.5));
    float alpha = 1.0 - smoothstep(0.3, 0.5, dist);

    return float4(in.color.rgb * alpha * in.color.a, alpha * in.color.a);
}
