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
    library->release();

    if (!ghost_pipeline_) return false;

    std::cout << "[metal-llama] GPU ready: " << device_name() << std::endl;
    return true;
}

void MetalRenderer::teardown() {
    stop_render_loop();
    if (ghost_pipeline_) { ghost_pipeline_->release(); ghost_pipeline_ = nullptr; }
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
    float ps = particle_scale_;
    compute_enc->setBytes(&ps, sizeof(float), 4);

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
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 3]);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 3 + 1]);
    trail_positions_.push_back(vocab_positions_ptr_[token_id * 3 + 2]);
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

    float cx = vocab_positions_ptr_[chosen_token_id * 3];
    float cy = vocab_positions_ptr_[chosen_token_id * 3 + 1];
    float cz = vocab_positions_ptr_[chosen_token_id * 3 + 2];
    float ti = (float)trail_index;

    for (const auto& [runner_id, prob] : runners) {
        if (runner_id < 0 || runner_id >= vocab_size_ || runner_id == chosen_token_id) continue;

        float rx = vocab_positions_ptr_[runner_id * 3];
        float ry = vocab_positions_ptr_[runner_id * 3 + 1];
        float rz = vocab_positions_ptr_[runner_id * 3 + 2];

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
    {
        std::lock_guard<std::mutex> lock(dist_mutex_);
        scrub_index_ = idx;
    }
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
    camera_.reset();
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
        int si = scrub_index_;
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
        bool was_idle = false;

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
                was_idle = false;
                // Clear dirty flag — we just rendered the scrub frame
                {
                    std::lock_guard<std::mutex> lock(render_wake_mutex_);
                    render_dirty_ = false;
                }
                // Wait for next scrub change or resume
                {
                    std::unique_lock<std::mutex> lock(render_wake_mutex_);
                    render_wake_cv_.wait_for(lock, std::chrono::milliseconds(16),
                        [this]{ return render_dirty_ || !render_running_.load(); });
                }
                continue;
            }

            // Live mode — compute lerp t based on time since last keyframe
            float t;
            {
                std::lock_guard<std::mutex> lock(dist_mutex_);
                if (!prob_a_ || !prob_b_) {
                    // No data yet — sleep until woken by submit_distribution
                    std::unique_lock<std::mutex> wlock(render_wake_mutex_);
                    render_wake_cv_.wait_for(wlock, std::chrono::milliseconds(500),
                        [this]{ return render_dirty_ || !render_running_.load(); });
                    render_dirty_ = false;
                    continue;
                }
                auto elapsed = std::chrono::steady_clock::now() - keyframe_time_;
                auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();
                t = std::min(1.0f, (float)ms / (float)keyframe_interval_.count());
            }

            // If interpolation is complete (t=1) and we already rendered the final frame,
            // sleep until new data arrives instead of burning GPU
            if (t >= 1.0f && was_idle) {
                std::unique_lock<std::mutex> lock(render_wake_mutex_);
                render_wake_cv_.wait_for(lock, std::chrono::milliseconds(500),
                    [this]{ return render_dirty_ || !render_running_.load(); });
                if (render_dirty_) {
                    render_dirty_ = false;
                    was_idle = false;  // re-render with updated params
                }
                continue;
            }

            auto png = render_lerp(render_width_, render_height_, t);
            if (!png.empty()) {
                set_latest_frame(std::move(png), render_width_, render_height_);
            }

            was_idle = (t >= 1.0f);
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
