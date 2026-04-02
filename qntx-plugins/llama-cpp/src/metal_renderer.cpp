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
#include <CoreText/CoreText.h>
#include <ImageIO/ImageIO.h>

// Shader source — read from src/shaders.metal at compile time.
// If a pre-compiled default.metallib exists next to the binary, it's loaded instead.
static const char* kShaderSource =
#include "shaders.metal.inc"
;

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
    std::cout << "[metal-llama] setup() entered" << std::endl;

    // Clean up any partial state from a previous failed attempt
    if (device_ || queue_ || compute_pipeline_ || render_pipeline_) {
        std::cout << "[metal-llama] Cleaning up partial state from previous attempt" << std::endl;
        teardown();
    }

    device_ = MTL::CreateSystemDefaultDevice();
    if (!device_) {
        std::cout << "[metal-llama] MTL::CreateSystemDefaultDevice() returned null" << std::endl;
        return false;
    }
    std::cout << "[metal-llama] Device acquired: " << device_->name()->utf8String() << std::endl;

    queue_ = device_->newCommandQueue();
    if (!queue_) {
        std::cout << "[metal-llama] newCommandQueue() returned null" << std::endl;
        return false;
    }
    std::cout << "[metal-llama] Command queue created" << std::endl;

    // Try pre-compiled metallib first, fall back to runtime compilation
    NS::Error* error = nullptr;
    MTL::Library* library = nullptr;

    auto exe_path = NS::Bundle::mainBundle()->executablePath();
    if (exe_path) {
        std::string exe_str = exe_path->utf8String();
        auto slash = exe_str.rfind('/');
        std::string lib_path = (slash != std::string::npos)
            ? exe_str.substr(0, slash) + "/default.metallib"
            : "default.metallib";
        auto lib_url = NS::URL::fileURLWithPath(
            NS::String::string(lib_path.c_str(), NS::UTF8StringEncoding));
        library = device_->newLibrary(lib_url, &error);
        if (library) {
            std::cout << "[metal-llama] Loaded pre-compiled " << lib_path << std::endl;
        }
    }

    if (!library) {
        // Runtime compilation from embedded source
        error = nullptr;
        auto src = NS::String::string(kShaderSource, NS::UTF8StringEncoding);
        library = device_->newLibrary(src, nullptr, &error);
        if (!library) {
            if (error) {
                std::cout << "[metal-llama] Shader compilation failed: "
                          << error->localizedDescription()->utf8String() << std::endl;
            }
            return false;
        }
        std::cout << "[metal-llama] Compiled shaders from source" << std::endl;
    }

    // Compute pipeline
    auto compute_fn = library->newFunction(NS::String::string("particleCompute", NS::UTF8StringEncoding));
    if (!compute_fn) {
        std::cout << "[metal-llama] particleCompute not found" << std::endl;
        return false;
    }
    compute_pipeline_ = device_->newComputePipelineState(compute_fn, &error);
    compute_fn->release();
    if (!compute_pipeline_) return false;

    // Lerp compute pipeline
    auto lerp_fn = library->newFunction(NS::String::string("particleComputeLerp", NS::UTF8StringEncoding));
    if (!lerp_fn) {
        std::cout << "[metal-llama] particleComputeLerp not found" << std::endl;
        return false;
    }
    lerp_pipeline_ = device_->newComputePipelineState(lerp_fn, &error);
    lerp_fn->release();
    if (!lerp_pipeline_) return false;

    // Render pipeline
    auto vertex_fn = library->newFunction(NS::String::string("particleVertex", NS::UTF8StringEncoding));
    auto fragment_fn = library->newFunction(NS::String::string("particleFragment", NS::UTF8StringEncoding));
    if (!vertex_fn || !fragment_fn) {
        std::cout << "[metal-llama] vertex/fragment functions not found" << std::endl;
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
        std::cout << "[metal-llama] trail vertex/fragment functions not found" << std::endl;
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

    if (!trail_pipeline_) { library->release(); return false; }

    // Ghost branch pipeline — runner-up paths at uncertain tokens
    auto ghost_vertex_fn = library->newFunction(NS::String::string("ghostBranchVertex", NS::UTF8StringEncoding));
    auto ghost_fragment_fn = library->newFunction(NS::String::string("trailFragment", NS::UTF8StringEncoding));
    if (!ghost_vertex_fn || !ghost_fragment_fn) {
        std::cout << "[metal-llama] ghost branch shaders not found" << std::endl;
        if (ghost_vertex_fn) ghost_vertex_fn->release();
        if (ghost_fragment_fn) ghost_fragment_fn->release();
        library->release();
        return false;
    }

    auto ghost_rpd = MTL::RenderPipelineDescriptor::alloc()->init();
    ghost_rpd->setVertexFunction(ghost_vertex_fn);
    ghost_rpd->setFragmentFunction(ghost_fragment_fn);
    ghost_rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatRGBA16Float);
    ghost_rpd->colorAttachments()->object(0)->setBlendingEnabled(true);
    ghost_rpd->colorAttachments()->object(0)->setSourceRGBBlendFactor(MTL::BlendFactorOne);
    ghost_rpd->colorAttachments()->object(0)->setDestinationRGBBlendFactor(MTL::BlendFactorOne);
    ghost_rpd->colorAttachments()->object(0)->setSourceAlphaBlendFactor(MTL::BlendFactorOne);
    ghost_rpd->colorAttachments()->object(0)->setDestinationAlphaBlendFactor(MTL::BlendFactorOne);

    ghost_pipeline_ = device_->newRenderPipelineState(ghost_rpd, &error);
    ghost_rpd->release();
    ghost_vertex_fn->release();
    ghost_fragment_fn->release();

    if (!ghost_pipeline_) { library->release(); return false; }

    // Pick pipeline — renders token IDs to R32Uint for hover identification
    auto pick_vertex_fn = library->newFunction(NS::String::string("pickVertex", NS::UTF8StringEncoding));
    auto pick_fragment_fn = library->newFunction(NS::String::string("pickFragment", NS::UTF8StringEncoding));
    if (!pick_vertex_fn || !pick_fragment_fn) {
        std::cout << "[metal-llama] pick shaders not found" << std::endl;
        if (pick_vertex_fn) pick_vertex_fn->release();
        if (pick_fragment_fn) pick_fragment_fn->release();
        library->release();
        return false;
    }

    auto pick_rpd = MTL::RenderPipelineDescriptor::alloc()->init();
    pick_rpd->setVertexFunction(pick_vertex_fn);
    pick_rpd->setFragmentFunction(pick_fragment_fn);
    pick_rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatR32Uint);
    pick_rpd->colorAttachments()->object(0)->setBlendingEnabled(false);
    pick_rpd->setDepthAttachmentPixelFormat(MTL::PixelFormatDepth32Float);

    pick_pipeline_ = device_->newRenderPipelineState(pick_rpd, &error);
    pick_rpd->release();
    pick_vertex_fn->release();
    pick_fragment_fn->release();

    if (!pick_pipeline_) { library->release(); return false; }

    // Highlight pipeline — cyan square around hovered particle
    auto hl_vertex_fn = library->newFunction(NS::String::string("highlightVertex", NS::UTF8StringEncoding));
    auto hl_fragment_fn = library->newFunction(NS::String::string("highlightFragment", NS::UTF8StringEncoding));
    if (!hl_vertex_fn || !hl_fragment_fn) {
        std::cout << "[metal-llama] highlight shaders not found" << std::endl;
        if (hl_vertex_fn) hl_vertex_fn->release();
        if (hl_fragment_fn) hl_fragment_fn->release();
        library->release();
        return false;
    }

    auto hl_rpd = MTL::RenderPipelineDescriptor::alloc()->init();
    hl_rpd->setVertexFunction(hl_vertex_fn);
    hl_rpd->setFragmentFunction(hl_fragment_fn);
    hl_rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatRGBA16Float);
    hl_rpd->colorAttachments()->object(0)->setBlendingEnabled(true);
    hl_rpd->colorAttachments()->object(0)->setSourceRGBBlendFactor(MTL::BlendFactorSourceAlpha);
    hl_rpd->colorAttachments()->object(0)->setDestinationRGBBlendFactor(MTL::BlendFactorOneMinusSourceAlpha);
    hl_rpd->colorAttachments()->object(0)->setSourceAlphaBlendFactor(MTL::BlendFactorOne);
    hl_rpd->colorAttachments()->object(0)->setDestinationAlphaBlendFactor(MTL::BlendFactorOne);

    highlight_pipeline_ = device_->newRenderPipelineState(hl_rpd, &error);
    hl_rpd->release();
    hl_vertex_fn->release();
    hl_fragment_fn->release();

    if (!highlight_pipeline_) { library->release(); return false; }

    // Cursor pipeline — same vertex shader as highlight, cursor fragment with crosshair + fill
    auto cursor_fragment_fn = library->newFunction(NS::String::string("cursorFragment", NS::UTF8StringEncoding));
    if (cursor_fragment_fn) {
        auto cursor_vertex_fn = library->newFunction(NS::String::string("highlightVertex", NS::UTF8StringEncoding));
        auto cur_rpd = MTL::RenderPipelineDescriptor::alloc()->init();
        cur_rpd->setVertexFunction(cursor_vertex_fn);
        cur_rpd->setFragmentFunction(cursor_fragment_fn);
        cur_rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatRGBA16Float);
        cur_rpd->colorAttachments()->object(0)->setBlendingEnabled(true);
        cur_rpd->colorAttachments()->object(0)->setSourceRGBBlendFactor(MTL::BlendFactorSourceAlpha);
        cur_rpd->colorAttachments()->object(0)->setDestinationRGBBlendFactor(MTL::BlendFactorOneMinusSourceAlpha);
        cur_rpd->colorAttachments()->object(0)->setSourceAlphaBlendFactor(MTL::BlendFactorOne);
        cur_rpd->colorAttachments()->object(0)->setDestinationAlphaBlendFactor(MTL::BlendFactorOne);
        cursor_pipeline_ = device_->newRenderPipelineState(cur_rpd, &error);
        cur_rpd->release();
        cursor_vertex_fn->release();
        cursor_fragment_fn->release();
    }

    // Label pipeline — textured quad for CoreText-rendered text
    auto label_vertex_fn = library->newFunction(NS::String::string("labelVertex", NS::UTF8StringEncoding));
    auto label_fragment_fn = library->newFunction(NS::String::string("labelFragment", NS::UTF8StringEncoding));
    if (label_vertex_fn && label_fragment_fn) {
        auto label_rpd = MTL::RenderPipelineDescriptor::alloc()->init();
        label_rpd->setVertexFunction(label_vertex_fn);
        label_rpd->setFragmentFunction(label_fragment_fn);
        label_rpd->colorAttachments()->object(0)->setPixelFormat(MTL::PixelFormatRGBA16Float);
        label_rpd->colorAttachments()->object(0)->setBlendingEnabled(true);
        label_rpd->colorAttachments()->object(0)->setSourceRGBBlendFactor(MTL::BlendFactorSourceAlpha);
        label_rpd->colorAttachments()->object(0)->setDestinationRGBBlendFactor(MTL::BlendFactorOneMinusSourceAlpha);
        label_rpd->colorAttachments()->object(0)->setSourceAlphaBlendFactor(MTL::BlendFactorOne);
        label_rpd->colorAttachments()->object(0)->setDestinationAlphaBlendFactor(MTL::BlendFactorOne);
        label_pipeline_ = device_->newRenderPipelineState(label_rpd, &error);
        label_rpd->release();
    }
    if (label_vertex_fn) label_vertex_fn->release();
    if (label_fragment_fn) label_fragment_fn->release();
    // Label pipeline is optional — don't fail if it's missing

    library->release();

    std::cout << "[metal-llama] GPU ready: " << device_name() << std::endl;
    return true;
}

void MetalRenderer::teardown() {
    stop_render_loop();
    if (label_pipeline_) { label_pipeline_->release(); label_pipeline_ = nullptr; }
    if (highlight_pipeline_) { highlight_pipeline_->release(); highlight_pipeline_ = nullptr; }
    if (pick_pipeline_) { pick_pipeline_->release(); pick_pipeline_ = nullptr; }
    if (pick_depth_) { pick_depth_->release(); pick_depth_ = nullptr; }
    if (pick_texture_) { pick_texture_->release(); pick_texture_ = nullptr; }
    if (ghost_pipeline_) { ghost_pipeline_->release(); ghost_pipeline_ = nullptr; }
    if (prob_a_) { prob_a_->release(); prob_a_ = nullptr; }
    if (prob_b_) { prob_b_->release(); prob_b_ = nullptr; }
    if (colors_buffer_) { colors_buffer_->release(); colors_buffer_ = nullptr; }
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

void MetalRenderer::set_vocab_positions(const float* data, int vocab_size) {
    if (!device_) return;

    vocab_size_ = vocab_size;

    // Input is interleaved: 6 floats per token (3 pos + 3 color).
    // Deinterleave into separate GPU buffers.
    std::vector<float> pos(vocab_size * 3);
    std::vector<float> col(vocab_size * 3);
    for (int i = 0; i < vocab_size; i++) {
        pos[i*3]   = data[i*6];
        pos[i*3+1] = data[i*6+1];
        pos[i*3+2] = data[i*6+2];
        col[i*3]   = data[i*6+3];
        col[i*3+1] = data[i*6+4];
        col[i*3+2] = data[i*6+5];
    }

    if (positions_buffer_) positions_buffer_->release();
    positions_buffer_ = device_->newBuffer(pos.data(), vocab_size * 3 * sizeof(float),
                                           MTL::ResourceStorageModeShared);

    if (colors_buffer_) colors_buffer_->release();
    colors_buffer_ = device_->newBuffer(col.data(), vocab_size * 3 * sizeof(float),
                                         MTL::ResourceStorageModeShared);

    // Cache pointer for trail lookups (caller must keep data alive)
    vocab_positions_ptr_ = data;

    // Compute bounding box for auto-fit
    float min_x = INFINITY, max_x = -INFINITY;
    float min_y = INFINITY, max_y = -INFINITY;
    for (int i = 0; i < vocab_size; i++) {
        float x = pos[i * 3];
        float y = pos[i * 3 + 1];
        if (x < min_x) min_x = x; if (x > max_x) max_x = x;
        if (y < min_y) min_y = y; if (y > max_y) max_y = y;
    }
    center_x_ = (min_x + max_x) / 2.0f;
    center_y_ = (min_y + max_y) / 2.0f;
    extent_ = std::max(max_x - min_x, max_y - min_y) / 2.0f;
    if (extent_ < 1e-6f) extent_ = 1.0f;

    drift_step_ = extent_ * 0.0015f;

    // Position camera to see the cloud
    camera_.reset(center_x_, center_y_, extent_);
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
    float ps = particle_scale_;
    compute_enc->setBytes(&ps, sizeof(float), 4);
    compute_enc->setBuffer(colors_buffer_, 0, 5);

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

    float mvp[16];
    build_mvp(mvp, width, height);
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

    // Wake render loop from idle sleep
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::add_trail_point(int token_id) {
    if (!vocab_positions_ptr_ || token_id < 0 || token_id >= vocab_size_) return;

    std::lock_guard<std::mutex> lock(trail_mutex_);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 6]);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 6 + 1]);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 6 + 2]);
}

void MetalRenderer::clear_trail() {
    std::lock_guard<std::mutex> lock(trail_mutex_);
    trail_positions_.clear();
    ghost_vertices_.clear();
    drift_count_ = 0;
}

void MetalRenderer::add_ghost_branches(int chosen_token_id,
                                        const std::vector<std::pair<int,float>>& runners) {
    if (!vocab_positions_ptr_ || chosen_token_id < 0 || chosen_token_id >= vocab_size_) return;

    std::lock_guard<std::mutex> lock(trail_mutex_);
    int trail_index = (int)(trail_positions_.size() / 3) - 1;
    if (trail_index < 0) return;

    float cx = vocab_positions_ptr_[chosen_token_id * 6];
    float cy = vocab_positions_ptr_[chosen_token_id * 6 + 1];
    float cz = vocab_positions_ptr_[chosen_token_id * 6 + 2];
    float ti = (float)trail_index;

    for (const auto& [runner_id, prob] : runners) {
        if (runner_id < 0 || runner_id >= vocab_size_ || runner_id == chosen_token_id) continue;

        float rx = vocab_positions_ptr_[runner_id * 6];
        float ry = vocab_positions_ptr_[runner_id * 6 + 1];
        float rz = vocab_positions_ptr_[runner_id * 6 + 2];

        // From vertex (chosen position)
        ghost_vertices_.push_back(cx);
        ghost_vertices_.push_back(cy);
        ghost_vertices_.push_back(cz);
        ghost_vertices_.push_back(prob);
        ghost_vertices_.push_back(ti);

        // To vertex (runner-up position)
        ghost_vertices_.push_back(rx);
        ghost_vertices_.push_back(ry);
        ghost_vertices_.push_back(rz);
        ghost_vertices_.push_back(prob);
        ghost_vertices_.push_back(ti);
    }
}

void MetalRenderer::store_keyframe(const float* probabilities, int vocab_size) {
    std::lock_guard<std::mutex> lock(dist_mutex_);
    if (keyframe_history_.size() >= 512) return;  // cap at 512 entries
    keyframe_history_.emplace_back(probabilities, probabilities + vocab_size);
    drift_count_++;
}

void MetalRenderer::set_scrub_index(int idx) {
    scrub_index_.store(idx, std::memory_order_release);
    // Wake render loop for scrub
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::apply_camera(float dx, float dy, float dz, float dyaw, float dpitch) {
    camera_.apply(dx, dy, dz, dyaw, dpitch);
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::reset_camera() {
    camera_.reset(center_x_, center_y_, extent_);
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::build_mvp(float* mvp, int width, int height) {
    camera_.build_mvp(mvp, width, height, center_x_, center_y_, extent_);
}

void MetalRenderer::set_param(const std::string& key, float value) {
    if (key == "orbit_period") {
        orbit_period_ = std::max(16, (int)value);
    } else if (key == "orbit_radius") {
        orbit_radius_mult_ = std::max(0.5f, value);
    } else if (key == "particle_scale") {
        particle_scale_ = std::max(0.1f, std::min(5.0f, value));
    }
    // Wake render loop to reflect the change
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
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
        float ps = particle_scale_;
        compute_enc->setBytes(&ps, sizeof(float), 6);
        compute_enc->setBuffer(colors_buffer_, 0, 7);
        float zero_off[3] = {0, 0, 0};
        compute_enc->setBytes(zero_off, sizeof(float) * 3, 8);
        float full_fade = 1.0f;
        compute_enc->setBytes(&full_fade, sizeof(float), 9);
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

    float mvp[16];
    build_mvp(mvp, width, height);
    render_enc->setVertexBytes(mvp, sizeof(mvp), 1);

    render_enc->drawPrimitives(MTL::PrimitiveTypePoint, (NS::UInteger)0, (NS::UInteger)n);

    // --- Trail + ghost branches draw ---
    {
        std::lock_guard<std::mutex> lock(trail_mutex_);
        int trail_count = trail_positions_.size() / 3;
        uint32_t tc = (uint32_t)trail_count;
        int si = scrub_index_.load(std::memory_order_relaxed);
        float or_ = extent_ * orbit_radius_mult_;
        float as = (2.0f * M_PI) / orbit_period_;

        if (trail_count >= 2 && trail_pipeline_) {
            auto trail_buf = device_->newBuffer(trail_positions_.data(),
                trail_positions_.size() * sizeof(float), MTL::ResourceStorageModeShared);
            if (trail_buf) {
                render_enc->setRenderPipelineState(trail_pipeline_);
                render_enc->setVertexBuffer(trail_buf, 0, 0);
                render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                render_enc->setVertexBytes(&si, sizeof(int), 3);
                render_enc->setVertexBytes(&or_, sizeof(float), 4);
                render_enc->setVertexBytes(&as, sizeof(float), 5);
                render_enc->drawPrimitives(MTL::PrimitiveTypeLineStrip,
                    (NS::UInteger)0, (NS::UInteger)trail_count);
                trail_buf->release();
            }
        }

        // Ghost branches — runner-up paths drawn as line pairs
        int ghost_vertex_count = ghost_vertices_.size() / 5;
        if (ghost_vertex_count >= 2 && ghost_pipeline_) {
            auto ghost_buf = device_->newBuffer(ghost_vertices_.data(),
                ghost_vertices_.size() * sizeof(float), MTL::ResourceStorageModeShared);
            if (ghost_buf) {
                render_enc->setRenderPipelineState(ghost_pipeline_);
                render_enc->setVertexBuffer(ghost_buf, 0, 0);
                render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                render_enc->setVertexBytes(&si, sizeof(int), 3);
                render_enc->setVertexBytes(&or_, sizeof(float), 4);
                render_enc->setVertexBytes(&as, sizeof(float), 5);
                render_enc->drawPrimitives(MTL::PrimitiveTypeLine,
                    (NS::UInteger)0, (NS::UInteger)ghost_vertex_count);
                ghost_buf->release();
            }
        }
    }

    // --- Mouse-driven cursor + highlight ---
    int mx = mouse_x_.load(std::memory_order_acquire);
    int my = mouse_y_.load(std::memory_order_acquire);
    if (mx >= 0 && my >= 0) {
        // Read token under cursor from pick buffer
        int picked = pick_at(mx, my);
        hovered_token_.store(picked, std::memory_order_release);

        if (picked >= 0 && picked < n && highlight_pipeline_) {
            glm::mat4 mvp_mat;
            std::memcpy(&mvp_mat[0][0], mvp, 16 * sizeof(float));
            float hpx = ((float*)positions_buffer_->contents())[picked * 3];
            float hpy = ((float*)positions_buffer_->contents())[picked * 3 + 1];
            float hpz = ((float*)positions_buffer_->contents())[picked * 3 + 2];
            glm::vec4 clip = mvp_mat * glm::vec4(hpx, hpy, hpz, 1.0f);

            if (clip.w > 0) {
                float half_size[2] = { 19.0f / (float)width, 19.0f / (float)height };
                float clip_arr[4] = { clip.x, clip.y, clip.z, clip.w };

                render_enc->setRenderPipelineState(highlight_pipeline_);
                render_enc->setVertexBytes(clip_arr, sizeof(clip_arr), 0);
                render_enc->setVertexBytes(half_size, sizeof(half_size), 1);
                render_enc->drawPrimitives(MTL::PrimitiveTypeTriangle, (NS::UInteger)0, (NS::UInteger)6);
            }
        }

        // Persistent cursor — crosshair with semi-transparent fill, scales with particle
        if (cursor_pipeline_) {
            float cursor_ndc_x = ((float)mx / (float)width) * 2.0f - 1.0f;
            float cursor_ndc_y = -(((float)my / (float)height) * 2.0f - 1.0f);
            float cursor_px = 12.0f;
            if (picked >= 0 && picked < n) {
                auto* particles = (float*)particle_buf->contents();
                // Particle struct: float3(16B) + float4(16B) + float(4B) = 48B stride = 12 floats
                float psize = particles[picked * 12 + 8];
                cursor_px = fmaxf(12.0f, psize * 1.2f);
            }
            float cursor_size[2] = { cursor_px / (float)width, cursor_px / (float)height };
            float cursor_clip[4] = { cursor_ndc_x, cursor_ndc_y, 0.0f, 1.0f };

            render_enc->setRenderPipelineState(cursor_pipeline_);
            render_enc->setVertexBytes(cursor_clip, sizeof(cursor_clip), 0);
            render_enc->setVertexBytes(cursor_size, sizeof(cursor_size), 1);
            render_enc->drawPrimitives(MTL::PrimitiveTypeTriangle, (NS::UInteger)0, (NS::UInteger)6);
        }

        // Hover label — rendered at offset from cursor after 400ms idle
        {
            std::lock_guard<std::mutex> lock(hover_label_mutex_);
            if (!hover_label_.empty()) {
                // Position label to the right of the cursor, slightly above
                float label_x = (float)mx + 20.0f;
                float label_y = (float)my - 10.0f;
                // Clamp to keep label on screen
                if (label_x + 200 > width) label_x = (float)mx - 220.0f;
                if (label_y < 0) label_y = (float)my + 20.0f;
                render_label(render_enc, hover_label_, label_x, label_y, width, height);
            }
        }
    }

    render_enc->endEncoding();

    // --- Pick pass: render token IDs to R32Uint with depth test ---
    if (pick_pipeline_) {
        std::lock_guard<std::mutex> lock(pick_mutex_);
        ensure_pick_textures(width, height);

        if (pick_texture_ && pick_depth_) {
            auto pick_rp = MTL::RenderPassDescriptor::alloc()->init();
            pick_rp->colorAttachments()->object(0)->setTexture(pick_texture_);
            pick_rp->colorAttachments()->object(0)->setLoadAction(MTL::LoadActionClear);
            // Clear to 0xFFFFFFFF (no token)
            pick_rp->colorAttachments()->object(0)->setClearColor(MTL::ClearColor(4294967295.0, 0, 0, 0));
            pick_rp->colorAttachments()->object(0)->setStoreAction(MTL::StoreActionStore);
            pick_rp->depthAttachment()->setTexture(pick_depth_);
            pick_rp->depthAttachment()->setLoadAction(MTL::LoadActionClear);
            pick_rp->depthAttachment()->setClearDepth(1.0);
            pick_rp->depthAttachment()->setStoreAction(MTL::StoreActionDontCare);

            auto pick_enc = cmd->renderCommandEncoder(pick_rp);
            pick_enc->setRenderPipelineState(pick_pipeline_);
            pick_enc->setVertexBuffer(particle_buf, 0, 0);
            pick_enc->setVertexBytes(mvp, sizeof(mvp), 1);

            // Depth state: less-equal, write enabled
            auto depth_desc = MTL::DepthStencilDescriptor::alloc()->init();
            depth_desc->setDepthCompareFunction(MTL::CompareFunctionLessEqual);
            depth_desc->setDepthWriteEnabled(true);
            auto depth_state = device_->newDepthStencilState(depth_desc);
            depth_desc->release();
            pick_enc->setDepthStencilState(depth_state);

            pick_enc->drawPrimitives(MTL::PrimitiveTypePoint, (NS::UInteger)0, (NS::UInteger)n);
            pick_enc->endEncoding();
            depth_state->release();
            pick_rp->release();
        }
    }

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
        bool was_idle = false;
        auto last_time = std::chrono::steady_clock::now();

        while (render_running_.load()) {
            // Tick camera interpolation
            auto now = std::chrono::steady_clock::now();
            float dt = std::chrono::duration<float>(now - last_time).count();
            last_time = now;
            camera_.update(dt);

            // Check for scrub mode
            int scrub = scrub_index_.load(std::memory_order_acquire);

            if (scrub >= 0) {
                // Multi-cloud scrub: render every keyframe's cloud at its orbit
                // position, fading with distance from the selected keyframe.
                int kf_count = 0;
                {
                    std::lock_guard<std::mutex> lock(dist_mutex_);
                    kf_count = (int)keyframe_history_.size();
                }

                if (kf_count > 0 && is_ready() && positions_buffer_ && colors_buffer_) {
                    int w = render_width_, h = render_height_;
                    int n = vocab_size_;
                    float or_ = extent_ * orbit_radius_mult_;
                    float as = (2.0f * M_PI) / orbit_period_;

                    // HDR render target — shared across all keyframe passes
                    auto tex_desc = MTL::TextureDescriptor::texture2DDescriptor(
                        MTL::PixelFormatRGBA16Float, w, h, false);
                    tex_desc->setUsage(MTL::TextureUsageRenderTarget | MTL::TextureUsageShaderRead);
                    auto hdr_tex = device_->newTexture(tex_desc);

                    if (hdr_tex) {
                        bool first_pass = true;

                        for (int ki = 0; ki < kf_count; ki++) {
                            // Distance-based fade: selected = 1.0, fades with distance
                            int dist = std::abs(ki - scrub);
                            float fade = expf(-0.0375f * (float)dist);
                            if (fade < 0.02f) continue;  // skip near-invisible clouds

                            std::vector<float> kf;
                            {
                                std::lock_guard<std::mutex> lock(dist_mutex_);
                                if (ki < (int)keyframe_history_.size())
                                    kf = keyframe_history_[ki];
                            }
                            if (kf.empty()) continue;

                            // Orbit offset for this keyframe
                            float age = (float)(scrub - ki);
                            float theta = age * as;
                            float off[3] = {
                                or_ * sinf(theta),
                                -or_ * (1.0f - cosf(theta)),
                                0.0f
                            };

                            auto prob_buf = device_->newBuffer(kf.data(), kf.size() * sizeof(float),
                                                                MTL::ResourceStorageModeShared);
                            auto particle_buf = device_->newBuffer(n * 48, MTL::ResourceStorageModeShared);
                            if (!prob_buf || !particle_buf) {
                                if (prob_buf) prob_buf->release();
                                if (particle_buf) particle_buf->release();
                                continue;
                            }

                            uint32_t vocab_u = (uint32_t)n;
                            float ps = particle_scale_;

                            auto cmd = queue_->commandBuffer();
                            if (!cmd) { prob_buf->release(); particle_buf->release(); continue; }

                            // Compute pass — generate particles with orbit offset and fade
                            auto compute_enc = cmd->computeCommandEncoder();
                            compute_enc->setComputePipelineState(lerp_pipeline_);
                            compute_enc->setBuffer(prob_buf, 0, 0);   // probA
                            compute_enc->setBuffer(prob_buf, 0, 1);   // probB (same — no lerp)
                            compute_enc->setBuffer(positions_buffer_, 0, 2);
                            compute_enc->setBuffer(particle_buf, 0, 3);
                            compute_enc->setBytes(&vocab_u, sizeof(uint32_t), 4);
                            float t_one = 1.0f;
                            compute_enc->setBytes(&t_one, sizeof(float), 5);
                            compute_enc->setBytes(&ps, sizeof(float), 6);
                            compute_enc->setBuffer(colors_buffer_, 0, 7);
                            compute_enc->setBytes(off, sizeof(float) * 3, 8);
                            compute_enc->setBytes(&fade, sizeof(float), 9);

                            NS::UInteger tg = std::min(lerp_pipeline_->maxTotalThreadsPerThreadgroup(), (NS::UInteger)256);
                            compute_enc->dispatchThreads(MTL::Size(n, 1, 1), MTL::Size(tg, 1, 1));
                            compute_enc->endEncoding();

                            // Render pass — additive into shared HDR texture
                            auto rp_desc = MTL::RenderPassDescriptor::alloc()->init();
                            rp_desc->colorAttachments()->object(0)->setTexture(hdr_tex);
                            if (first_pass) {
                                rp_desc->colorAttachments()->object(0)->setLoadAction(MTL::LoadActionClear);
                                rp_desc->colorAttachments()->object(0)->setClearColor(MTL::ClearColor(0.02, 0.01, 0.03, 1.0));
                                first_pass = false;
                            } else {
                                rp_desc->colorAttachments()->object(0)->setLoadAction(MTL::LoadActionLoad);
                            }
                            rp_desc->colorAttachments()->object(0)->setStoreAction(MTL::StoreActionStore);

                            auto render_enc = cmd->renderCommandEncoder(rp_desc);
                            render_enc->setRenderPipelineState(render_pipeline_);
                            render_enc->setVertexBuffer(particle_buf, 0, 0);

                            float mvp[16];
                            build_mvp(mvp, w, h);
                            render_enc->setVertexBytes(mvp, sizeof(mvp), 1);

                            render_enc->drawPrimitives(MTL::PrimitiveTypePoint, (NS::UInteger)0, (NS::UInteger)n);

                            // Draw trail + ghost branches on last pass
                            if (ki == kf_count - 1 || ki == scrub) {
                                std::lock_guard<std::mutex> lock(trail_mutex_);
                                int trail_count = trail_positions_.size() / 3;
                                uint32_t tc = (uint32_t)trail_count;
                                int si = scrub;
                                float or_t = or_;
                                float as_t = as;

                                if (trail_count >= 2 && trail_pipeline_) {
                                    auto trail_buf = device_->newBuffer(trail_positions_.data(),
                                        trail_positions_.size() * sizeof(float), MTL::ResourceStorageModeShared);
                                    if (trail_buf) {
                                        render_enc->setRenderPipelineState(trail_pipeline_);
                                        render_enc->setVertexBuffer(trail_buf, 0, 0);
                                        render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                                        render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                                        render_enc->setVertexBytes(&si, sizeof(int), 3);
                                        render_enc->setVertexBytes(&or_t, sizeof(float), 4);
                                        render_enc->setVertexBytes(&as_t, sizeof(float), 5);
                                        render_enc->drawPrimitives(MTL::PrimitiveTypeLineStrip,
                                            (NS::UInteger)0, (NS::UInteger)trail_count);
                                        trail_buf->release();
                                    }
                                }

                                int ghost_vertex_count = ghost_vertices_.size() / 5;
                                if (ghost_vertex_count >= 2 && ghost_pipeline_) {
                                    auto ghost_buf = device_->newBuffer(ghost_vertices_.data(),
                                        ghost_vertices_.size() * sizeof(float), MTL::ResourceStorageModeShared);
                                    if (ghost_buf) {
                                        render_enc->setRenderPipelineState(ghost_pipeline_);
                                        render_enc->setVertexBuffer(ghost_buf, 0, 0);
                                        render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                                        render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                                        render_enc->setVertexBytes(&si, sizeof(int), 3);
                                        render_enc->setVertexBytes(&or_t, sizeof(float), 4);
                                        render_enc->setVertexBytes(&as_t, sizeof(float), 5);
                                        render_enc->drawPrimitives(MTL::PrimitiveTypeLine,
                                            (NS::UInteger)0, (NS::UInteger)ghost_vertex_count);
                                        ghost_buf->release();
                                    }
                                }
                            }

                            // Mouse-driven cursor + highlight in scrub mode
                            if (ki == scrub) {
                                int smx = mouse_x_.load(std::memory_order_acquire);
                                int smy = mouse_y_.load(std::memory_order_acquire);
                                if (smx >= 0 && smy >= 0) {
                                    int picked_scrub = pick_at(smx, smy);
                                    hovered_token_.store(picked_scrub, std::memory_order_release);

                                    if (picked_scrub >= 0 && picked_scrub < n && highlight_pipeline_) {
                                        glm::mat4 mvp_mat;
                                        std::memcpy(&mvp_mat[0][0], mvp, 16 * sizeof(float));
                                        float hpx = ((float*)positions_buffer_->contents())[picked_scrub * 3];
                                        float hpy = ((float*)positions_buffer_->contents())[picked_scrub * 3 + 1];
                                        float hpz = ((float*)positions_buffer_->contents())[picked_scrub * 3 + 2];
                                        glm::vec4 clip = mvp_mat * glm::vec4(hpx, hpy, hpz, 1.0f);
                                        if (clip.w > 0) {
                                            float hs[2] = { 19.0f / (float)w, 19.0f / (float)h };
                                            float ca[4] = { clip.x, clip.y, clip.z, clip.w };
                                            render_enc->setRenderPipelineState(highlight_pipeline_);
                                            render_enc->setVertexBytes(ca, sizeof(ca), 0);
                                            render_enc->setVertexBytes(hs, sizeof(hs), 1);
                                            render_enc->drawPrimitives(MTL::PrimitiveTypeTriangle, (NS::UInteger)0, (NS::UInteger)6);
                                        }
                                    }

                                    // Persistent cursor — crosshair, scales with particle
                                    if (cursor_pipeline_) {
                                        float cnx = ((float)smx / (float)w) * 2.0f - 1.0f;
                                        float cny = -(((float)smy / (float)h) * 2.0f - 1.0f);
                                        float cpx = 12.0f;
                                        if (picked_scrub >= 0 && picked_scrub < n) {
                                            auto* sp = (float*)particle_buf->contents();
                                            cpx = fmaxf(12.0f, sp[picked_scrub * 12 + 8] * 1.2f);
                                        }
                                        float cs[2] = { cpx / (float)w, cpx / (float)h };
                                        float cc[4] = { cnx, cny, 0.0f, 1.0f };
                                        render_enc->setRenderPipelineState(cursor_pipeline_);
                                        render_enc->setVertexBytes(cc, sizeof(cc), 0);
                                        render_enc->setVertexBytes(cs, sizeof(cs), 1);
                                        render_enc->drawPrimitives(MTL::PrimitiveTypeTriangle, (NS::UInteger)0, (NS::UInteger)6);
                                    }

                                    // Hover label in scrub mode
                                    {
                                        std::lock_guard<std::mutex> llock(hover_label_mutex_);
                                        if (!hover_label_.empty()) {
                                            float lx = (float)smx + 20.0f;
                                            float ly = (float)smy - 10.0f;
                                            if (lx + 200 > w) lx = (float)smx - 220.0f;
                                            if (ly < 0) ly = (float)smy + 20.0f;
                                            render_label(render_enc, hover_label_, lx, ly, w, h);
                                        }
                                    }
                                }
                            }

                            render_enc->endEncoding();

                            // Pick pass for the selected keyframe
                            if (ki == scrub && pick_pipeline_) {
                                std::lock_guard<std::mutex> plock(pick_mutex_);
                                ensure_pick_textures(w, h);
                                if (pick_texture_ && pick_depth_) {
                                    auto pick_rp = MTL::RenderPassDescriptor::alloc()->init();
                                    pick_rp->colorAttachments()->object(0)->setTexture(pick_texture_);
                                    pick_rp->colorAttachments()->object(0)->setLoadAction(MTL::LoadActionClear);
                                    pick_rp->colorAttachments()->object(0)->setClearColor(MTL::ClearColor(4294967295.0, 0, 0, 0));
                                    pick_rp->colorAttachments()->object(0)->setStoreAction(MTL::StoreActionStore);
                                    pick_rp->depthAttachment()->setTexture(pick_depth_);
                                    pick_rp->depthAttachment()->setLoadAction(MTL::LoadActionClear);
                                    pick_rp->depthAttachment()->setClearDepth(1.0);
                                    pick_rp->depthAttachment()->setStoreAction(MTL::StoreActionDontCare);

                                    auto pick_enc = cmd->renderCommandEncoder(pick_rp);
                                    pick_enc->setRenderPipelineState(pick_pipeline_);
                                    pick_enc->setVertexBuffer(particle_buf, 0, 0);
                                    pick_enc->setVertexBytes(mvp, sizeof(mvp), 1);

                                    auto depth_desc = MTL::DepthStencilDescriptor::alloc()->init();
                                    depth_desc->setDepthCompareFunction(MTL::CompareFunctionLessEqual);
                                    depth_desc->setDepthWriteEnabled(true);
                                    auto depth_state = device_->newDepthStencilState(depth_desc);
                                    depth_desc->release();
                                    pick_enc->setDepthStencilState(depth_state);

                                    pick_enc->drawPrimitives(MTL::PrimitiveTypePoint, (NS::UInteger)0, (NS::UInteger)n);
                                    pick_enc->endEncoding();
                                    depth_state->release();
                                    pick_rp->release();
                                }
                            }

                            cmd->commit();
                            cmd->waitUntilCompleted();

                            rp_desc->release();
                            prob_buf->release();
                            particle_buf->release();
                        }

                        // Tonemap HDR → LDR PNG
                        int bpp_hdr = 8;
                        std::vector<uint16_t> hdr(w * h * 4);
                        hdr_tex->getBytes(hdr.data(), w * bpp_hdr,
                                          MTL::Region(0, 0, w, h), 0);

                        std::vector<uint8_t> ldr(w * h * 4);
                        for (int i = 0; i < w * h; i++) {
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
                            ldr.data(), w, h, 8, w * 4, colorspace,
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
                        hdr_tex->release();

                        if (!png_data.empty()) {
                            set_latest_frame(std::move(png_data), w, h);
                        }
                    }
                }
                was_idle = false;
                // Clear dirty flag — we just rendered the scrub frame
                {
                    std::lock_guard<std::mutex> lock(render_wake_mutex_);
                    render_dirty_ = false;
                }
                // Wait for next scrub change, mouse move, or resume
                // Stay awake at 60fps while mouse is active for cursor rendering
                {
                    bool mouse_active = mouse_x_.load(std::memory_order_acquire) >= 0;
                    if (!mouse_active) {
                        std::unique_lock<std::mutex> lock(render_wake_mutex_);
                        render_wake_cv_.wait_for(lock, std::chrono::milliseconds(500),
                            [this]{ return render_dirty_ || !render_running_.load(); });
                    } else {
                        std::this_thread::sleep_for(std::chrono::milliseconds(16));
                    }
                }
                continue;
            }

            // Live mode — compute lerp t based on time since last keyframe
            bool has_data = false;
            float t = 0;
            {
                std::lock_guard<std::mutex> lock(dist_mutex_);
                if (prob_a_ && prob_b_) {
                    auto elapsed = std::chrono::steady_clock::now() - keyframe_time_;
                    auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();
                    t = std::min(1.0f, (float)ms / (float)keyframe_interval_.count());
                    has_data = true;
                }
            }

            if (!has_data) {
                // No distributions yet — sleep without holding dist_mutex_
                std::unique_lock<std::mutex> wlock(render_wake_mutex_);
                render_wake_cv_.wait_for(wlock, std::chrono::milliseconds(500),
                    [this]{ return render_dirty_ || !render_running_.load(); });
                render_dirty_ = false;
                continue;
            }

            // If interpolation is complete (t=1) and we already rendered the final frame,
            // sleep until new data arrives instead of burning GPU
            if (t >= 1.0f && was_idle) {
                // Stay awake while mouse is active — cursor needs continuous rendering
                bool mouse_active = mouse_x_.load(std::memory_order_acquire) >= 0;
                if (mouse_active) {
                    was_idle = false;
                } else {
                    std::unique_lock<std::mutex> lock(render_wake_mutex_);
                    render_wake_cv_.wait_for(lock, std::chrono::milliseconds(500),
                        [this]{ return render_dirty_ || !render_running_.load(); });
                    if (render_dirty_) {
                        render_dirty_ = false;
                        was_idle = false;
                    }
                    continue;
                }
            }

            auto png = render_lerp(render_width_, render_height_, t);
            if (!png.empty()) {
                set_latest_frame(std::move(png), render_width_, render_height_);
            }

            was_idle = (t >= 1.0f) && mouse_x_.load(std::memory_order_acquire) < 0;
            if (was_idle) {
                // Clear dirty so we catch the next submit_distribution
                std::lock_guard<std::mutex> lock(render_wake_mutex_);
                render_dirty_ = false;
            }

            // ~60fps while animating
            std::this_thread::sleep_for(std::chrono::milliseconds(16));
        }
    });
}

void MetalRenderer::stop_render_loop() {
    render_running_.store(false);
    render_wake_cv_.notify_one();  // unblock if sleeping
    if (render_thread_.joinable()) {
        render_thread_.join();
    }
}

void MetalRenderer::ensure_pick_textures(int width, int height) {
    if (pick_texture_ && pick_width_ == width && pick_height_ == height) return;

    if (pick_texture_) pick_texture_->release();
    if (pick_depth_) pick_depth_->release();

    auto td = MTL::TextureDescriptor::texture2DDescriptor(
        MTL::PixelFormatR32Uint, width, height, false);
    td->setUsage(MTL::TextureUsageRenderTarget);
    td->setStorageMode(MTL::StorageModeShared);
    pick_texture_ = device_->newTexture(td);

    auto dd = MTL::TextureDescriptor::texture2DDescriptor(
        MTL::PixelFormatDepth32Float, width, height, false);
    dd->setUsage(MTL::TextureUsageRenderTarget);
    dd->setStorageMode(MTL::StorageModePrivate);
    pick_depth_ = device_->newTexture(dd);

    pick_width_ = width;
    pick_height_ = height;
}

int MetalRenderer::pick_at(int px, int py) {
    std::lock_guard<std::mutex> lock(pick_mutex_);
    if (!pick_texture_ || px < 0 || py < 0 || px >= pick_width_ || py >= pick_height_) return -1;

    uint32_t val = 0xFFFFFFFF;
    pick_texture_->getBytes(&val, sizeof(uint32_t), MTL::Region(px, py, 1, 1), 0);
    if (val == 0xFFFFFFFF || val >= (uint32_t)vocab_size_) return -1;
    return (int)val;
}

void MetalRenderer::set_hovered_token(int token_id) {
    hovered_token_.store(token_id, std::memory_order_release);
    // Wake render loop to draw highlight
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

int MetalRenderer::hovered_token() const {
    return hovered_token_.load(std::memory_order_acquire);
}

int MetalRenderer::consume_pick_result() {
    if (pick_sent_) return -1;
    int mx = mouse_x_.load(std::memory_order_acquire);
    if (mx < 0) return -1;

    auto elapsed = std::chrono::steady_clock::now() - mouse_last_move_;
    if (std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count() < 400) return -1;

    int ht = hovered_token_.load(std::memory_order_acquire);
    if (ht < 0) return -1;

    pick_sent_ = true;
    return ht;
}

float MetalRenderer::token_probability(int token_id) {
    std::lock_guard<std::mutex> lock(dist_mutex_);
    if (!prob_b_ || token_id < 0 || token_id >= vocab_size_) return 0.0f;
    auto* probs = (float*)prob_b_->contents();
    return probs[token_id];
}

void MetalRenderer::set_hover_label(const std::string& text) {
    std::lock_guard<std::mutex> lock(hover_label_mutex_);
    hover_label_ = text;
    // Wake render loop to draw label
    {
        std::lock_guard<std::mutex> wlock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::set_mouse(int px, int py) {
    // px/py are normalized 0..1000 from JS — map to render texture pixels
    int rx = (px >= 0) ? (px * render_width_ / 1000) : -1;
    int ry = (py >= 0) ? (py * render_height_ / 1000) : -1;
    // Clear hover label when mouse moves
    {
        std::lock_guard<std::mutex> lock(hover_label_mutex_);
        hover_label_.clear();
    }
    mouse_x_.store(rx, std::memory_order_release);
    mouse_y_.store(ry, std::memory_order_release);
    mouse_last_move_ = std::chrono::steady_clock::now();
    pick_sent_ = false;
    // Wake render loop
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::render_label(MTL::RenderCommandEncoder* enc, const std::string& text,
                                  float screen_x, float screen_y, int width, int height) {
    if (!label_pipeline_ || text.empty()) return;

    // Render text to a bitmap using CoreText
    float font_size = 11.0f;
    auto font = CTFontCreateWithName(CFSTR("Menlo"), font_size, nullptr);
    if (!font) return;

    CFStringRef cf_text = CFStringCreateWithCString(nullptr, text.c_str(), kCFStringEncodingUTF8);
    if (!cf_text) { CFRelease(font); return; }

    CFStringRef keys[] = { kCTFontAttributeName, kCTForegroundColorFromContextAttributeName };
    CFTypeRef vals[] = { font, kCFBooleanTrue };
    auto attrs = CFDictionaryCreate(nullptr, (const void**)keys, (const void**)vals, 2,
                                     &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    auto attr_str = CFAttributedStringCreate(nullptr, cf_text, attrs);
    auto line = CTLineCreateWithAttributedString(attr_str);

    CGFloat ascent, descent, leading;
    double line_width = CTLineGetTypographicBounds(line, &ascent, &descent, &leading);

    int tex_w = (int)ceil(line_width) + 8;  // padding
    int tex_h = (int)ceil(ascent + descent) + 6;

    // Draw to RGBA bitmap
    auto colorspace = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
    auto cg_ctx = CGBitmapContextCreate(nullptr, tex_w, tex_h, 8, tex_w * 4, colorspace,
                                         kCGImageAlphaPremultipliedLast);
    if (!cg_ctx) {
        CFRelease(line); CFRelease(attr_str); CFRelease(attrs);
        CFRelease(cf_text); CFRelease(font); CGColorSpaceRelease(colorspace);
        return;
    }

    // Semi-transparent background
    CGContextSetRGBFillColor(cg_ctx, 0.05, 0.05, 0.1, 0.5);
    CGContextFillRect(cg_ctx, CGRectMake(0, 0, tex_w, tex_h));

    // Text in soft white
    CGContextSetRGBFillColor(cg_ctx, 0.9, 0.95, 0.95, 0.9);
    CGContextSetTextPosition(cg_ctx, 4, descent + 3);
    CTLineDraw(line, cg_ctx);

    auto* pixels = (uint8_t*)CGBitmapContextGetData(cg_ctx);

    // Upload to Metal texture
    auto td = MTL::TextureDescriptor::texture2DDescriptor(
        MTL::PixelFormatRGBA8Unorm, tex_w, tex_h, false);
    td->setUsage(MTL::TextureUsageShaderRead);
    td->setStorageMode(MTL::StorageModeShared);
    auto label_tex = device_->newTexture(td);
    label_tex->replaceRegion(MTL::Region(0, 0, tex_w, tex_h), 0, pixels, tex_w * 4);

    // Draw textured quad
    float rect_data[4] = { screen_x, screen_y, (float)tex_w, (float)tex_h };
    float viewport[2] = { (float)width, (float)height };

    enc->setRenderPipelineState(label_pipeline_);
    enc->setVertexBytes(rect_data, sizeof(rect_data), 0);
    enc->setVertexBytes(viewport, sizeof(viewport), 1);
    enc->setFragmentTexture(label_tex, 0);
    enc->drawPrimitives(MTL::PrimitiveTypeTriangle, (NS::UInteger)0, (NS::UInteger)6);

    label_tex->release();
    CGContextRelease(cg_ctx);
    CGColorSpaceRelease(colorspace);
    CFRelease(line);
    CFRelease(attr_str);
    CFRelease(attrs);
    CFRelease(cf_text);
    CFRelease(font);
}
