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
    constant float& particleScale     [[buffer(4)]],
    device const float* colors        [[buffer(5)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= vocabSize) return;

    float prob = probabilities[id];
    float3 pos = float3(positions[id * 3], positions[id * 3 + 1], positions[id * 3 + 2]);
    float3 baseColor = float3(colors[id * 3], colors[id * 3 + 1], colors[id * 3 + 2]);

    float threshold = 1e-5;
    float visible = step(threshold, prob);
    float logProb = visible * saturate((log2(prob + 1e-10) + 20.0) / 20.0);

    // Base color from PCA 4-6, boosted for visibility. Probability drives brightness.
    float3 rgb = baseColor * (0.4 + logProb * 1.6);

    Particle p;
    p.position = pos;
    p.color = float4(rgb * visible, logProb * visible);
    p.size = visible * (2.0 + logProb * 12.0) * particleScale;

    particles[id] = p;
}

kernel void particleComputeLerp(
    device const float* probA         [[buffer(0)]],
    device const float* probB         [[buffer(1)]],
    device const float* positions     [[buffer(2)]],
    device Particle* particles        [[buffer(3)]],
    constant uint& vocabSize          [[buffer(4)]],
    constant float& t                 [[buffer(5)]],
    constant float& particleScale     [[buffer(6)]],
    device const float* colors        [[buffer(7)]],
    constant float3& posOffset        [[buffer(8)]],
    constant float& fadeMul           [[buffer(9)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= vocabSize) return;

    float prob = mix(probA[id], probB[id], t);
    float3 pos = float3(positions[id * 3], positions[id * 3 + 1], positions[id * 3 + 2]);
    pos += posOffset;
    float3 baseColor = float3(colors[id * 3], colors[id * 3 + 1], colors[id * 3 + 2]);

    float threshold = 1e-5;
    float visible = step(threshold, prob);
    float logProb = visible * saturate((log2(prob + 1e-10) + 20.0) / 20.0);

    // Base color from PCA 4-6, boosted for visibility. Probability drives brightness.
    float3 rgb = baseColor * (0.4 + logProb * 1.6);

    Particle p;
    p.position = pos;
    p.color = float4(rgb * visible * fadeMul, logProb * visible * fadeMul);
    p.size = visible * (2.0 + logProb * 12.0) * particleScale * fadeMul;

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
    constant float& orbitRadius  [[buffer(4)]],
    constant float& angleStep    [[buffer(5)]],
    uint vid [[vertex_id]]
) {
    float3 pos = float3(positions[vid * 3], positions[vid * 3 + 1], positions[vid * 3 + 2]);

    // Drift: newest point (head) sits on the nebula, older points curve
    // along a circular arc. Each token advances by angleStep radians
    // around a circle of orbitRadius, centered below the head.
    int headIndex = (scrubIndex >= 0) ? scrubIndex : int(trailCount - 1);
    float age = float(headIndex - int(vid));
    float theta = age * angleStep;
    pos.x += orbitRadius * sin(theta);
    pos.y -= orbitRadius * (1.0 - cos(theta));

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

// --- Ghost branches: runner-up paths at low-certainty tokens ---
// Buffer layout: 5 floats per vertex [x, y, z, alpha, trailIndex]
// Drawn as Line primitives (pairs of vertices: chosen->runner-up)

// --- Ghost branches ---

vertex TrailVertexOut ghostBranchVertex(
    device const float* data     [[buffer(0)]],
    constant float4x4& mvp      [[buffer(1)]],
    constant uint& trailCount   [[buffer(2)]],
    constant int& scrubIndex    [[buffer(3)]],
    constant float& orbitRadius [[buffer(4)]],
    constant float& angleStep   [[buffer(5)]],
    uint vid [[vertex_id]]
) {
    float3 pos = float3(data[vid*5], data[vid*5+1], data[vid*5+2]);
    float prob = data[vid*5+3];
    int trailIdx = int(data[vid*5+4]);

    // Same orbit transform as the main trail
    int headIndex = (scrubIndex >= 0) ? scrubIndex : int(trailCount - 1);
    float age = float(headIndex - trailIdx);
    float theta = age * angleStep;
    pos.x += orbitRadius * sin(theta);
    pos.y -= orbitRadius * (1.0 - cos(theta));

    TrailVertexOut out;
    out.position = mvp * float4(pos, 1.0);
    // Cool blue-violet tint — visually distinct from warm main trail
    out.color = float3(0.4, 0.55, 0.9);
    // Alpha scales with probability — high-prob runners bright, low-prob fade
    out.alpha = saturate(prob * 4.0) * 0.6;
    return out;
}

// --- Pick buffer: render token IDs to R32Uint for hover identification ---

struct PickVertexOut {
    float4 position [[position]];
    float pointSize [[point_size]];
    uint tokenId [[flat]];
};

vertex PickVertexOut pickVertex(
    device const Particle* particles [[buffer(0)]],
    constant float4x4& mvp          [[buffer(1)]],
    uint vid [[vertex_id]]
) {
    Particle p = particles[vid];

    PickVertexOut out;
    out.position = mvp * float4(p.position, 1.0);
    // Minimum 8px hitbox so small/distant particles are still pickable
    out.pointSize = max(p.size, 8.0);
    out.tokenId = vid;
    return out;
}

fragment uint pickFragment(
    PickVertexOut in [[stage_in]],
    float2 pointCoord [[point_coord]]
) {
    // Circular cutout — but use the inflated radius for picking
    float dist = length(pointCoord - float2(0.5));
    if (dist > 0.5) discard_fragment();

    return in.tokenId;
}

// --- Highlight: square outline around a picked particle ---

struct HighlightVertexOut {
    float4 position [[position]];
    float2 uv;
};

vertex HighlightVertexOut highlightVertex(
    constant float4& clipCenter [[buffer(0)]],  // clip-space position of picked particle
    constant float2& halfSize   [[buffer(1)]],  // half-size in NDC
    uint vid [[vertex_id]]
) {
    // Quad: 6 vertices (2 triangles), CCW
    float2 corners[6] = {
        float2(-1, -1), float2(1, -1), float2(1, 1),
        float2(-1, -1), float2(1, 1), float2(-1, 1)
    };
    float2 c = corners[vid];

    HighlightVertexOut out;
    float2 ndc = clipCenter.xy / clipCenter.w;
    out.position = float4(ndc + c * halfSize, clipCenter.z / clipCenter.w, 1.0);
    out.uv = c * 0.5 + 0.5;  // 0..1
    return out;
}

fragment float4 highlightFragment(
    HighlightVertexOut in [[stage_in]]
) {
    // Hollow square: only draw the border (2px-equivalent in UV space)
    float border = 0.08;
    float2 uv = in.uv;
    bool onEdge = uv.x < border || uv.x > (1.0 - border) ||
                  uv.y < border || uv.y > (1.0 - border);
    if (!onEdge) discard_fragment();

    return float4(0.0, 1.0, 1.0, 0.9);  // cyan
}

// --- Cursor: crosshair with semi-transparent fill ---

fragment float4 cursorFragment(
    HighlightVertexOut in [[stage_in]]
) {
    float2 uv = in.uv;
    // Semi-transparent fill
    float4 fill = float4(0.0, 1.0, 1.0, 0.08);
    // Border — thicker than highlight (visible at small sizes)
    float border = 0.15;
    bool onEdge = uv.x < border || uv.x > (1.0 - border) ||
                  uv.y < border || uv.y > (1.0 - border);
    // Crosshair lines through center
    float cross = 0.06;
    bool onCross = (abs(uv.x - 0.5) < cross) || (abs(uv.y - 0.5) < cross);
    if (onEdge || onCross) return float4(0.0, 1.0, 1.0, 0.7);
    return fill;
}

// --- Label: textured quad for CoreText-rendered text ---

struct LabelVertexOut {
    float4 position [[position]];
    float2 uv;
};

vertex LabelVertexOut labelVertex(
    constant float4& rect [[buffer(0)]],  // x, y, w, h in pixels
    constant float2& viewport [[buffer(1)]],  // viewport width, height
    uint vid [[vertex_id]]
) {
    float2 corners[6] = {
        float2(0, 0), float2(1, 0), float2(1, 1),
        float2(0, 0), float2(1, 1), float2(0, 1)
    };
    float2 c = corners[vid];

    // Pixel coords to NDC
    float2 pos = float2(rect.x + c.x * rect.z, rect.y + c.y * rect.w);
    float2 ndc = pos / viewport * 2.0 - 1.0;
    ndc.y = -ndc.y;  // flip Y

    LabelVertexOut out;
    out.position = float4(ndc, 0.0, 1.0);
    out.uv = float2(c.x, c.y);
    return out;
}

fragment float4 labelFragment(
    LabelVertexOut in [[stage_in]],
    texture2d<float> tex [[texture(0)]]
) {
    constexpr sampler s(filter::linear);
    float4 t = tex.sample(s, in.uv);
    return t;
}
