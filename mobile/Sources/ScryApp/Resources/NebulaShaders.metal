// Particle nebula shaders. Each particle is a token: position from vocab
// PCA, color from vocab PCA components 4-6, alpha/size from current
// probability. Rendered as point sprites with a soft circular falloff.

#include <metal_stdlib>
using namespace metal;

struct CameraUniforms {
    float4x4 mvp;
    float pointScale;     // size multiplier
    float globalAlpha;    // 0..1, fades whole nebula
    float _pad0;
    float _pad1;
};

struct Particle {
    packed_float3 position;
    packed_float3 color;
    float alpha;          // 0..1 from probability
    float size;           // 0..N world units
    int   tokenId;
};

struct VOut {
    float4 position [[position]];
    float  pointSize [[point_size]];
    float3 color;
    float  alpha;
    int    tokenId [[flat]];
};

vertex VOut nebula_vertex(
    const device Particle *particles [[buffer(0)]],
    constant CameraUniforms &uniforms [[buffer(1)]],
    uint vid [[vertex_id]])
{
    Particle p = particles[vid];
    float4 clip = uniforms.mvp * float4(p.position, 1.0);

    VOut o;
    o.position = clip;
    // Perspective-aware point size.
    o.pointSize = max(2.0, p.size * uniforms.pointScale / max(0.01, clip.w));
    o.color = p.color;
    o.alpha = p.alpha * uniforms.globalAlpha;
    o.tokenId = p.tokenId;
    return o;
}

fragment float4 nebula_fragment(
    VOut in [[stage_in]],
    float2 pointCoord [[point_coord]])
{
    float2 c = pointCoord - float2(0.5);
    float r2 = dot(c, c);
    if (r2 > 0.25) discard_fragment();
    // Gaussian-ish falloff for a soft nebula look.
    float falloff = exp(-r2 * 12.0);
    return float4(in.color * falloff, in.alpha * falloff);
}

// Pick pass: writes tokenId+1 to an R32Uint texture (0 = miss).
fragment uint pick_fragment(
    VOut in [[stage_in]],
    float2 pointCoord [[point_coord]])
{
    float2 c = pointCoord - float2(0.5);
    if (dot(c, c) > 0.25) discard_fragment();
    return uint(in.tokenId + 1);
}
