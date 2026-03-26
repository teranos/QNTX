// Metal-cpp private implementation — must be defined in exactly one translation unit.
#define NS_PRIVATE_IMPLEMENTATION
#define CA_PRIVATE_IMPLEMENTATION
#define MTL_PRIVATE_IMPLEMENTATION

#include <Foundation/Foundation.hpp>
#include <Metal/Metal.hpp>
#include <QuartzCore/QuartzCore.hpp>

#include "metal_renderer.h"

#include <cmath>
#include <cstring>
#include <iostream>
#include <random>
#include <vector>

#include <CoreGraphics/CoreGraphics.h>
#include <ImageIO/ImageIO.h>

// Same MSL shader source proven in the Swift prototype.
static const char* shader_source = R"(
#include <metal_stdlib>
using namespace metal;

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

// --- Trail shaders ---

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
    // headIndex is the token currently being viewed.
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
)";

// IEEE 754 half → single precision
static float float16to32(uint16_t h) {
    uint32_t sign = (h >> 15) & 1;
    uint32_t exp = (h >> 10) & 0x1F;
    uint32_t frac = h & 0x3FF;

    if (exp == 0) {
        if (frac == 0) return sign ? -0.0f : 0.0f;
        float f = float(frac) / 1024.0f;
        f *= powf(2.0f, -14.0f);
        return sign ? -f : f;
    } else if (exp == 31) {
        return frac == 0 ? (sign ? -INFINITY : INFINITY) : NAN;
    }

    uint32_t bits = (sign << 31) | ((exp + 112) << 23) | (frac << 13);
    float result;
    memcpy(&result, &bits, 4);
    return result;
}

MetalRenderer::MetalRenderer() {}

MetalRenderer::~MetalRenderer() {
    teardown();
}

bool MetalRenderer::setup() {
    device_ = MTL::CreateSystemDefaultDevice();
    if (!device_) return false;

    queue_ = device_->newCommandQueue();
    if (!queue_) return false;

    // Compile shaders from source
    NS::Error* error = nullptr;
    auto source = NS::String::string(shader_source, NS::UTF8StringEncoding);
    auto library = device_->newLibrary(source, nullptr, &error);
    if (!library) {
        if (error) {
            std::cerr << "[metal-llama] Shader compilation failed: "
                      << error->localizedDescription()->utf8String() << std::endl;
        }
        return false;
    }

    // Compute pipeline
    auto compute_fn = library->newFunction(NS::String::string("particleCompute", NS::UTF8StringEncoding));
    if (!compute_fn) {
        std::cerr << "[metal-llama] particleCompute not found" << std::endl;
        return false;
    }
    compute_pipeline_ = device_->newComputePipelineState(compute_fn, &error);
    compute_fn->release();
    if (!compute_pipeline_) return false;

    // Lerp compute pipeline
    auto lerp_fn = library->newFunction(NS::String::string("particleComputeLerp", NS::UTF8StringEncoding));
    if (!lerp_fn) {
        std::cerr << "[metal-llama] particleComputeLerp not found" << std::endl;
        return false;
    }
    lerp_pipeline_ = device_->newComputePipelineState(lerp_fn, &error);
    lerp_fn->release();
    if (!lerp_pipeline_) return false;

    // Render pipeline
    auto vertex_fn = library->newFunction(NS::String::string("particleVertex", NS::UTF8StringEncoding));
    auto fragment_fn = library->newFunction(NS::String::string("particleFragment", NS::UTF8StringEncoding));
    if (!vertex_fn || !fragment_fn) {
        std::cerr << "[metal-llama] vertex/fragment functions not found" << std::endl;
        return false;
    }

    auto rpd = MTL::RenderPipelineDescriptor::alloc()->init();
    rpd->setVertexFunction(vertex_fn);
    rpd->setFragmentFunction(fragment_fn);
    rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatRGBA16Float);
    // Additive blending
    rpd->colorAttachments()->object(0)->setBlendingEnabled(true);
    rpd->colorAttachments()->object(0)->setSourceRGBBlendFactor(MTL::BlendFactorOne);
    rpd->colorAttachments()->object(0)->setDestinationRGBBlendFactor(MTL::BlendFactorOne);
    rpd->colorAttachments()->object(0)->setSourceAlphaBlendFactor(MTL::BlendFactorOne);
    rpd->colorAttachments()->object(0)->setDestinationAlphaBlendFactor(MTL::BlendFactorOne);

    render_pipeline_ = device_->newRenderPipelineState(rpd, &error);
    rpd->release();
    vertex_fn->release();
    fragment_fn->release();

    if (!render_pipeline_) { library->release(); return false; }

    // Trail render pipeline
    auto trail_vertex_fn = library->newFunction(NS::String::string("trailVertex", NS::UTF8StringEncoding));
    auto trail_fragment_fn = library->newFunction(NS::String::string("trailFragment", NS::UTF8StringEncoding));
    if (!trail_vertex_fn || !trail_fragment_fn) {
        std::cerr << "[metal-llama] trail vertex/fragment functions not found" << std::endl;
        library->release();
        return false;
    }

    auto trail_rpd = MTL::RenderPipelineDescriptor::alloc()->init();
    trail_rpd->setVertexFunction(trail_vertex_fn);
    trail_rpd->setFragmentFunction(trail_fragment_fn);
    trail_rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatRGBA16Float);
    trail_rpd->colorAttachments()->object(0)->setBlendingEnabled(true);
    trail_rpd->colorAttachments()->object(0)->setSourceRGBBlendFactor(MTL::BlendFactorOne);
    trail_rpd->colorAttachments()->object(0)->setDestinationRGBBlendFactor(MTL::BlendFactorOne);
    trail_rpd->colorAttachments()->object(0)->setSourceAlphaBlendFactor(MTL::BlendFactorOne);
    trail_rpd->colorAttachments()->object(0)->setDestinationAlphaBlendFactor(MTL::BlendFactorOne);

    trail_pipeline_ = device_->newRenderPipelineState(trail_rpd, &error);
    trail_rpd->release();
    trail_vertex_fn->release();
    trail_fragment_fn->release();
    library->release();

    if (!trail_pipeline_) return false;

    std::cout << "[metal-llama] GPU ready: " << device_name() << std::endl;
    return true;
}

void MetalRenderer::teardown() {
    stop_render_loop();
    if (prob_a_) { prob_a_->release(); prob_a_ = nullptr; }
    if (prob_b_) { prob_b_->release(); prob_b_ = nullptr; }
    if (positions_buffer_) { positions_buffer_->release(); positions_buffer_ = nullptr; }
    if (trail_pipeline_) { trail_pipeline_->release(); trail_pipeline_ = nullptr; }
    if (lerp_pipeline_) { lerp_pipeline_->release(); lerp_pipeline_ = nullptr; }
    if (compute_pipeline_) { compute_pipeline_->release(); compute_pipeline_ = nullptr; }
    if (render_pipeline_) { render_pipeline_->release(); render_pipeline_ = nullptr; }
    if (queue_) { queue_->release(); queue_ = nullptr; }
    if (device_) { device_->release(); device_ = nullptr; }
}

bool MetalRenderer::is_ready() const {
    return device_ && queue_ && compute_pipeline_ && render_pipeline_;
}

std::string MetalRenderer::device_name() const {
    if (!device_) return "none";
    return device_->name()->utf8String();
}

void MetalRenderer::set_vocab_positions(const float* positions, int vocab_size) {
    if (!device_) return;

    vocab_size_ = vocab_size;
    if (positions_buffer_) positions_buffer_->release();
    positions_buffer_ = device_->newBuffer(positions, vocab_size * 3 * sizeof(float),
                                           MTL::ResourceStorageModeShared);
    // Cache pointer for trail lookups (caller must keep data alive)
    vocab_positions_ptr_ = positions;

    // Compute bounding box for auto-fit
    float min_x = INFINITY, max_x = -INFINITY;
    float min_y = INFINITY, max_y = -INFINITY;
    for (int i = 0; i < vocab_size; i++) {
        float x = positions[i * 3];
        float y = positions[i * 3 + 1];
        if (x < min_x) min_x = x; if (x > max_x) max_x = x;
        if (y < min_y) min_y = y; if (y > max_y) max_y = y;
    }
    center_x_ = (min_x + max_x) / 2.0f;
    center_y_ = (min_y + max_y) / 2.0f;
    extent_ = std::max(max_x - min_x, max_y - min_y) / 2.0f;
    if (extent_ < 1e-6f) extent_ = 1.0f;

    // Drift step: fixed offset per token so the trail unrolls in space.
    // 0.15% of extent per token — after ~1300 tokens the trail spans
    // one full cloud diameter. Subtle enough to stay in the viewport.
    drift_step_ = extent_ * 0.0015f;
}

std::vector<uint8_t> MetalRenderer::render_nebula(const float* probabilities, int vocab_size,
                                                   int width, int height) {
    if (!is_ready() || !positions_buffer_ || vocab_size != vocab_size_) return {};

    int n = vocab_size;

    auto prob_buf = device_->newBuffer(probabilities, n * sizeof(float),
                                       MTL::ResourceStorageModeShared);
    if (!prob_buf) return {};

    // Particle struct in MSL: float3(16) + float4(16) + float(4) = 36, padded to 48 bytes
    // float3 has 16-byte alignment in Metal, so stride is 48 not 32.
    auto particle_buf = device_->newBuffer(n * 48, MTL::ResourceStorageModeShared);
    if (!particle_buf) { prob_buf->release(); return {}; }

    uint32_t vocab_u = (uint32_t)n;

    auto cmd = queue_->commandBuffer();
    if (!cmd) { prob_buf->release(); particle_buf->release(); return {}; }

    // --- Compute pass ---
    auto compute_enc = cmd->computeCommandEncoder();
    compute_enc->setComputePipelineState(compute_pipeline_);
    compute_enc->setBuffer(prob_buf, 0, 0);
    compute_enc->setBuffer(positions_buffer_, 0, 1);
    compute_enc->setBuffer(particle_buf, 0, 2);
    compute_enc->setBytes(&vocab_u, sizeof(uint32_t), 3);

    NS::UInteger tg_size = std::min(compute_pipeline_->maxTotalThreadsPerThreadgroup(), (NS::UInteger)256);
    compute_enc->dispatchThreads(MTL::Size(n, 1, 1), MTL::Size(tg_size, 1, 1));
    compute_enc->endEncoding();

    // --- Render pass ---
    auto tex_desc = MTL::TextureDescriptor::texture2DDescriptor(
        MTL::PixelFormatRGBA16Float, width, height, false);
    tex_desc->setUsage(MTL::TextureUsageRenderTarget | MTL::TextureUsageShaderRead);
    auto hdr_tex = device_->newTexture(tex_desc);
    if (!hdr_tex) { prob_buf->release(); particle_buf->release(); return {}; }

    auto rp_desc = MTL::RenderPassDescriptor::alloc()->init();
    rp_desc->colorAttachments()->object(0)->setTexture(hdr_tex);
    rp_desc->colorAttachments()->object(0)->setLoadAction(MTL::LoadActionClear);
    rp_desc->colorAttachments()->object(0)->setClearColor(MTL::ClearColor(0.02, 0.01, 0.03, 1.0));
    rp_desc->colorAttachments()->object(0)->setStoreAction(MTL::StoreActionStore);

    auto render_enc = cmd->renderCommandEncoder(rp_desc);
    render_enc->setRenderPipelineState(render_pipeline_);
    render_enc->setVertexBuffer(particle_buf, 0, 0);

    // MVP: scale to fit + center
    float scale = 0.9f / extent_;
    float aspect = (float)width / (float)height;
    float mvp[16] = {
        scale / aspect, 0, 0, 0,
        0, scale, 0, 0,
        0, 0, scale, 0,
        -center_x_ * scale / aspect, -center_y_ * scale, 0, 1
    };
    render_enc->setVertexBytes(mvp, sizeof(mvp), 1);

    render_enc->drawPrimitives(MTL::PrimitiveTypePoint, (NS::UInteger)0, (NS::UInteger)n);
    render_enc->endEncoding();

    cmd->commit();
    cmd->waitUntilCompleted();

    // --- Tonemap HDR → LDR PNG ---
    int bpp_hdr = 8; // rgba16Float = 4 × 2 bytes
    std::vector<uint16_t> hdr(width * height * 4);
    hdr_tex->getBytes(hdr.data(), width * bpp_hdr,
                      MTL::Region(0, 0, width, height), 0);

    std::vector<uint8_t> ldr(width * height * 4);
    for (int i = 0; i < width * height; i++) {
        float r = float16to32(hdr[i * 4]);
        float g = float16to32(hdr[i * 4 + 1]);
        float b = float16to32(hdr[i * 4 + 2]);

        // Reinhard tonemap + gamma
        float tr = powf(r / (1.0f + r), 1.0f / 2.2f);
        float tg = powf(g / (1.0f + g), 1.0f / 2.2f);
        float tb = powf(b / (1.0f + b), 1.0f / 2.2f);

        ldr[i * 4]     = (uint8_t)fminf(fmaxf(tr * 255.0f, 0), 255);
        ldr[i * 4 + 1] = (uint8_t)fminf(fmaxf(tg * 255.0f, 0), 255);
        ldr[i * 4 + 2] = (uint8_t)fminf(fmaxf(tb * 255.0f, 0), 255);
        ldr[i * 4 + 3] = 255;
    }

    // PNG encoding via CoreGraphics
    auto colorspace = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
    auto cg_ctx = CGBitmapContextCreate(
        ldr.data(), width, height, 8, width * 4, colorspace,
        kCGImageAlphaPremultipliedLast);

    std::vector<uint8_t> png_data;
    if (cg_ctx) {
        auto image = CGBitmapContextCreateImage(cg_ctx);
        if (image) {
            auto mutable_data = CFDataCreateMutable(nullptr, 0);
            auto dest = CGImageDestinationCreateWithData(mutable_data, CFSTR("public.png"), 1, nullptr);
            if (dest) {
                CGImageDestinationAddImage(dest, image, nullptr);
                if (CGImageDestinationFinalize(dest)) {
                    auto len = CFDataGetLength(mutable_data);
                    auto ptr = CFDataGetBytePtr(mutable_data);
                    png_data.assign(ptr, ptr + len);
                }
                CFRelease(dest);
            }
            CFRelease(mutable_data);
            CGImageRelease(image);
        }
        CGContextRelease(cg_ctx);
    }
    CGColorSpaceRelease(colorspace);

    rp_desc->release();
    hdr_tex->release();
    particle_buf->release();
    prob_buf->release();

    return png_data;
}

std::vector<uint8_t> MetalRenderer::render_test(int width, int height) {
    int n = 128256; // Llama 3.2 vocab size

    // Generate random positions if not loaded
    if (!positions_buffer_) {
        std::vector<float> positions(n * 3);
        std::mt19937 rng(42);
        std::normal_distribution<float> dist(0.0f, 10.0f);
        for (int i = 0; i < n * 3; i++) {
            positions[i] = dist(rng);
        }
        set_vocab_positions(positions.data(), n);
    }

    // Bimodal test distribution
    std::vector<float> probs(n, 1e-7f);
    for (int i = 1000; i < 1100; i++) probs[i] = 0.005f;
    probs[5000] = 0.3f;
    float sum = 0;
    for (float p : probs) sum += p;
    for (float& p : probs) p /= sum;

    return render_nebula(probs.data(), n, width, height);
}

void MetalRenderer::set_latest_frame(std::vector<uint8_t> png, int width, int height) {
    {
        std::lock_guard<std::mutex> lock(frame_mutex_);
        latest_frame_ = std::move(png);
        frame_width_ = width;
        frame_height_ = height;
        frame_seq_++;
    }
    frame_cv_.notify_all();
}

std::vector<uint8_t> MetalRenderer::get_latest_frame(int& width, int& height) {
    std::lock_guard<std::mutex> lock(frame_mutex_);
    width = frame_width_;
    height = frame_height_;
    return latest_frame_;
}

std::vector<uint8_t> MetalRenderer::wait_for_frame(int timeout_ms) {
    std::unique_lock<std::mutex> lock(frame_mutex_);
    uint64_t seen = frame_seq_;
    if (frame_cv_.wait_for(lock, std::chrono::milliseconds(timeout_ms),
                           [&] { return frame_seq_ > seen; })) {
        return latest_frame_;
    }
    return {};
}

void MetalRenderer::submit_distribution(const float* probabilities, int vocab_size) {
    if (!device_ || vocab_size != vocab_size_) return;

    std::lock_guard<std::mutex> lock(dist_mutex_);

    // Swap: current becomes previous
    if (prob_b_) {
        if (prob_a_) prob_a_->release();
        prob_a_ = prob_b_;
    }

    // Upload new distribution as current
    prob_b_ = device_->newBuffer(probabilities, vocab_size * sizeof(float),
                                  MTL::ResourceStorageModeShared);
    keyframe_time_ = std::chrono::steady_clock::now();

    // If no previous distribution yet, duplicate current as previous
    if (!prob_a_) {
        prob_a_ = device_->newBuffer(probabilities, vocab_size * sizeof(float),
                                      MTL::ResourceStorageModeShared);
    }
}

void MetalRenderer::add_trail_point(int token_id) {
    if (!vocab_positions_ptr_ || token_id < 0 || token_id >= vocab_size_) return;

    std::lock_guard<std::mutex> lock(trail_mutex_);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 3]);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 3 + 1]);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 3 + 2]);
}

void MetalRenderer::clear_trail() {
    std::lock_guard<std::mutex> lock(trail_mutex_);
    trail_positions_.clear();
    drift_count_ = 0;
}

void MetalRenderer::store_keyframe(const float* probabilities, int vocab_size) {
    std::lock_guard<std::mutex> lock(dist_mutex_);
    if (keyframe_history_.size() >= 512) return;  // cap at 512 entries
    keyframe_history_.emplace_back(probabilities, probabilities + vocab_size);
    drift_count_++;
}

void MetalRenderer::set_scrub_index(int idx) {
    std::lock_guard<std::mutex> lock(dist_mutex_);
    scrub_index_ = idx;
}

std::vector<uint8_t> MetalRenderer::render_lerp(int width, int height, float t) {
    if (!is_ready() || !positions_buffer_ || !prob_a_ || !prob_b_) return {};

    int n = vocab_size_;
    t = std::max(0.0f, std::min(1.0f, t));

    auto particle_buf = device_->newBuffer(n * 48, MTL::ResourceStorageModeShared);
    if (!particle_buf) return {};

    uint32_t vocab_u = (uint32_t)n;

    auto cmd = queue_->commandBuffer();
    if (!cmd) { particle_buf->release(); return {}; }

    // --- Lerp compute pass ---
    auto compute_enc = cmd->computeCommandEncoder();
    {
        std::lock_guard<std::mutex> lock(dist_mutex_);
        compute_enc->setComputePipelineState(lerp_pipeline_);
        compute_enc->setBuffer(prob_a_, 0, 0);
        compute_enc->setBuffer(prob_b_, 0, 1);
        compute_enc->setBuffer(positions_buffer_, 0, 2);
        compute_enc->setBuffer(particle_buf, 0, 3);
        compute_enc->setBytes(&vocab_u, sizeof(uint32_t), 4);
        compute_enc->setBytes(&t, sizeof(float), 5);
    }

    NS::UInteger tg_size = std::min(lerp_pipeline_->maxTotalThreadsPerThreadgroup(), (NS::UInteger)256);
    compute_enc->dispatchThreads(MTL::Size(n, 1, 1), MTL::Size(tg_size, 1, 1));
    compute_enc->endEncoding();

    // --- Render pass ---
    auto tex_desc = MTL::TextureDescriptor::texture2DDescriptor(
        MTL::PixelFormatRGBA16Float, width, height, false);
    tex_desc->setUsage(MTL::TextureUsageRenderTarget | MTL::TextureUsageShaderRead);
    auto hdr_tex = device_->newTexture(tex_desc);
    if (!hdr_tex) { particle_buf->release(); return {}; }

    auto rp_desc = MTL::RenderPassDescriptor::alloc()->init();
    rp_desc->colorAttachments()->object(0)->setTexture(hdr_tex);
    rp_desc->colorAttachments()->object(0)->setLoadAction(MTL::LoadActionClear);
    rp_desc->colorAttachments()->object(0)->setClearColor(MTL::ClearColor(0.02, 0.01, 0.03, 1.0));
    rp_desc->colorAttachments()->object(0)->setStoreAction(MTL::StoreActionStore);

    auto render_enc = cmd->renderCommandEncoder(rp_desc);
    render_enc->setRenderPipelineState(render_pipeline_);
    render_enc->setVertexBuffer(particle_buf, 0, 0);

    float scale = 0.9f / extent_;
    float aspect = (float)width / (float)height;
    float mvp[16] = {
        scale / aspect, 0, 0, 0,
        0, scale, 0, 0,
        0, 0, scale, 0,
        -center_x_ * scale / aspect, -center_y_ * scale, 0, 1
    };
    render_enc->setVertexBytes(mvp, sizeof(mvp), 1);

    render_enc->drawPrimitives(MTL::PrimitiveTypePoint, (NS::UInteger)0, (NS::UInteger)n);

    // --- Trail draw ---
    {
        std::lock_guard<std::mutex> lock(trail_mutex_);
        int trail_count = trail_positions_.size() / 3;
        if (trail_count >= 2 && trail_pipeline_) {
            auto trail_buf = device_->newBuffer(trail_positions_.data(),
                trail_positions_.size() * sizeof(float), MTL::ResourceStorageModeShared);
            if (trail_buf) {
                uint32_t tc = (uint32_t)trail_count;
                int si = scrub_index_;
                float ds = drift_step_;
                render_enc->setRenderPipelineState(trail_pipeline_);
                render_enc->setVertexBuffer(trail_buf, 0, 0);
                render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                render_enc->setVertexBytes(&si, sizeof(int), 3);
                render_enc->setVertexBytes(&ds, sizeof(float), 4);
                render_enc->drawPrimitives(MTL::PrimitiveTypeLineStrip,
                    (NS::UInteger)0, (NS::UInteger)trail_count);
                trail_buf->release();
            }
        }
    }

    render_enc->endEncoding();

    cmd->commit();
    cmd->waitUntilCompleted();

    // --- Tonemap HDR → LDR PNG ---
    int bpp_hdr = 8;
    std::vector<uint16_t> hdr(width * height * 4);
    hdr_tex->getBytes(hdr.data(), width * bpp_hdr,
                      MTL::Region(0, 0, width, height), 0);

    std::vector<uint8_t> ldr(width * height * 4);
    for (int i = 0; i < width * height; i++) {
        float r = float16to32(hdr[i * 4]);
        float g = float16to32(hdr[i * 4 + 1]);
        float b = float16to32(hdr[i * 4 + 2]);

        float tr = powf(r / (1.0f + r), 1.0f / 2.2f);
        float tg = powf(g / (1.0f + g), 1.0f / 2.2f);
        float tb = powf(b / (1.0f + b), 1.0f / 2.2f);

        ldr[i * 4]     = (uint8_t)fminf(fmaxf(tr * 255.0f, 0), 255);
        ldr[i * 4 + 1] = (uint8_t)fminf(fmaxf(tg * 255.0f, 0), 255);
        ldr[i * 4 + 2] = (uint8_t)fminf(fmaxf(tb * 255.0f, 0), 255);
        ldr[i * 4 + 3] = 255;
    }

    auto colorspace = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
    auto cg_ctx = CGBitmapContextCreate(
        ldr.data(), width, height, 8, width * 4, colorspace,
        kCGImageAlphaPremultipliedLast);

    std::vector<uint8_t> png_data;
    if (cg_ctx) {
        auto image = CGBitmapContextCreateImage(cg_ctx);
        if (image) {
            auto mutable_data = CFDataCreateMutable(nullptr, 0);
            auto dest = CGImageDestinationCreateWithData(mutable_data, CFSTR("public.png"), 1, nullptr);
            if (dest) {
                CGImageDestinationAddImage(dest, image, nullptr);
                if (CGImageDestinationFinalize(dest)) {
                    auto len = CFDataGetLength(mutable_data);
                    auto ptr = CFDataGetBytePtr(mutable_data);
                    png_data.assign(ptr, ptr + len);
                }
                CFRelease(dest);
            }
            CFRelease(mutable_data);
            CGImageRelease(image);
        }
        CGContextRelease(cg_ctx);
    }
    CGColorSpaceRelease(colorspace);

    rp_desc->release();
    hdr_tex->release();
    particle_buf->release();

    return png_data;
}

void MetalRenderer::start_render_loop(int width, int height) {
    if (render_running_.load()) return;
    render_width_ = width;
    render_height_ = height;
    render_running_.store(true);

    render_thread_ = std::thread([this]() {
        while (render_running_.load()) {
            // Check for scrub mode
            int scrub;
            {
                std::lock_guard<std::mutex> lock(dist_mutex_);
                scrub = scrub_index_;
            }

            if (scrub >= 0) {
                // Scrub mode — upload the stored keyframe as both A and B,
                // render via render_lerp (trail shader handles the visual split).
                std::vector<float> kf;
                {
                    std::lock_guard<std::mutex> lock(dist_mutex_);
                    if (scrub < (int)keyframe_history_.size()) {
                        kf = keyframe_history_[scrub];
                    }
                }
                if (!kf.empty() && is_ready() && positions_buffer_) {
                    // Temporarily swap distributions to the scrub keyframe
                    MTL::Buffer* save_a;
                    MTL::Buffer* save_b;
                    {
                        std::lock_guard<std::mutex> lock(dist_mutex_);
                        save_a = prob_a_;
                        save_b = prob_b_;
                        prob_a_ = device_->newBuffer(kf.data(), kf.size() * sizeof(float),
                                                      MTL::ResourceStorageModeShared);
                        prob_b_ = device_->newBuffer(kf.data(), kf.size() * sizeof(float),
                                                      MTL::ResourceStorageModeShared);
                    }

                    auto png = render_lerp(render_width_, render_height_, 1.0f);

                    // Restore distributions
                    {
                        std::lock_guard<std::mutex> lock(dist_mutex_);
                        if (prob_a_) prob_a_->release();
                        if (prob_b_) prob_b_->release();
                        prob_a_ = save_a;
                        prob_b_ = save_b;
                    }

                    if (!png.empty()) {
                        set_latest_frame(std::move(png), render_width_, render_height_);
                    }
                }
                std::this_thread::sleep_for(std::chrono::milliseconds(16));
                continue;
            }

            // Live mode — compute lerp t based on time since last keyframe
            float t;
            {
                std::lock_guard<std::mutex> lock(dist_mutex_);
                if (!prob_a_ || !prob_b_) {
                    std::this_thread::sleep_for(std::chrono::milliseconds(16));
                    continue;
                }
                auto elapsed = std::chrono::steady_clock::now() - keyframe_time_;
                auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();
                t = std::min(1.0f, (float)ms / (float)keyframe_interval_.count());
            }

            auto png = render_lerp(render_width_, render_height_, t);
            if (!png.empty()) {
                set_latest_frame(std::move(png), render_width_, render_height_);
            }

            // ~60fps
            std::this_thread::sleep_for(std::chrono::milliseconds(16));
        }
    });
}

void MetalRenderer::stop_render_loop() {
    render_running_.store(false);
    if (render_thread_.joinable()) {
        render_thread_.join();
    }
}
