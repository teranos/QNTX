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
    library->release();

    if (!render_pipeline_) return false;

    std::cout << "[metal-llama] GPU ready: " << device_name() << std::endl;
    return true;
}

void MetalRenderer::teardown() {
    if (positions_buffer_) { positions_buffer_->release(); positions_buffer_ = nullptr; }
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
}

std::vector<uint8_t> MetalRenderer::render_nebula(const float* probabilities, int vocab_size,
                                                   int width, int height) {
    if (!is_ready() || !positions_buffer_ || vocab_size != vocab_size_) return {};

    int n = vocab_size;

    auto prob_buf = device_->newBuffer(probabilities, n * sizeof(float),
                                       MTL::ResourceStorageModeShared);
    if (!prob_buf) return {};

    // Particle buffer: position(3) + color(4) + size(1) = 32 bytes
    auto particle_buf = device_->newBuffer(n * 32, MTL::ResourceStorageModeShared);
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

void MetalRenderer::set_latest_frame(std::vector<uint8_t> pixels, int width, int height) {
    std::lock_guard<std::mutex> lock(frame_mutex_);
    latest_frame_ = std::move(pixels);
    frame_width_ = width;
    frame_height_ = height;
}

std::vector<uint8_t> MetalRenderer::get_latest_frame(int& width, int& height) {
    std::lock_guard<std::mutex> lock(frame_mutex_);
    width = frame_width_;
    height = frame_height_;
    return latest_frame_;
}
