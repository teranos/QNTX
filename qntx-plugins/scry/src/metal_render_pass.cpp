// Render passes — split from metal_renderer.cpp to keep file sizes manageable.
// Contains: render_nebula, render_test, render_lerp, start_render_loop, stop_render_loop.

#include <Foundation/Foundation.hpp>
#include <Metal/Metal.hpp>
#include <QuartzCore/QuartzCore.hpp>

#include "metal_renderer.h"

#include <cmath>
#include <cstring>
#include <random>
#include <vector>

#include <CoreGraphics/CoreGraphics.h>
#include <ImageIO/ImageIO.h>

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

// Tonemap HDR (RGBA16Float) → LDR PNG via Reinhard + gamma 2.2.
// The HDR texture is read back to CPU, tonemapped, then encoded as PNG via CoreGraphics.
static std::vector<uint8_t> tonemap_to_png(MTL::Texture* hdr_tex, int width, int height) {
    int bpp_hdr = 8; // rgba16Float = 4 × 2 bytes
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

    return png_data;
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

    auto png_data = tonemap_to_png(hdr_tex, width, height);

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

    // --- Trail + ghost branches draw (per-branch) ---
    {
        std::lock_guard<std::mutex> lock(trail_mutex_);
        int si = scrub_index_.load(std::memory_order_relaxed);
        float or_ = extent_ * orbit_radius_mult_;
        float as = (2.0f * M_PI) / orbit_period_;

        for (int bi = 0; bi < (int)branches_.size(); bi++) {
            auto& branch = branches_[bi];
            int trail_count = branch.trail_positions.size() / 3;
            uint32_t tc = (uint32_t)trail_count;
            float phase = branch.orbit_phase;

            if (trail_count >= 2 && trail_pipeline_) {
                auto trail_buf = device_->newBuffer(branch.trail_positions.data(),
                    branch.trail_positions.size() * sizeof(float), MTL::ResourceStorageModeShared);
                if (trail_buf) {
                    render_enc->setRenderPipelineState(trail_pipeline_);
                    render_enc->setVertexBuffer(trail_buf, 0, 0);
                    render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                    render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                    render_enc->setVertexBytes(&si, sizeof(int), 3);
                    render_enc->setVertexBytes(&or_, sizeof(float), 4);
                    render_enc->setVertexBytes(&as, sizeof(float), 5);
                    render_enc->setVertexBytes(&phase, sizeof(float), 6);
                    render_enc->drawPrimitives(MTL::PrimitiveTypeLineStrip,
                        (NS::UInteger)0, (NS::UInteger)trail_count);
                    trail_buf->release();
                }
            }

            // Ghost branches for this fork branch
            int ghost_vertex_count = branch.ghost_vertices.size() / 5;
            if (ghost_vertex_count >= 2 && ghost_pipeline_) {
                auto ghost_buf = device_->newBuffer(branch.ghost_vertices.data(),
                    branch.ghost_vertices.size() * sizeof(float), MTL::ResourceStorageModeShared);
                if (ghost_buf) {
                    render_enc->setRenderPipelineState(ghost_pipeline_);
                    render_enc->setVertexBuffer(ghost_buf, 0, 0);
                    render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                    render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                    render_enc->setVertexBytes(&si, sizeof(int), 3);
                    render_enc->setVertexBytes(&or_, sizeof(float), 4);
                    render_enc->setVertexBytes(&as, sizeof(float), 5);
                    render_enc->setVertexBytes(&phase, sizeof(float), 6);
                    render_enc->drawPrimitives(MTL::PrimitiveTypeLine,
                        (NS::UInteger)0, (NS::UInteger)ghost_vertex_count);
                    ghost_buf->release();
                }
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

    auto png_data = tonemap_to_png(hdr_tex, width, height);

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
                bool examining = token_examine_.load(std::memory_order_acquire);

                // Multi-cloud scrub: render every keyframe's cloud at its orbit
                // position, fading with distance from the selected keyframe.
                // In examine mode: single keyframe, no orbit, no fade.
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

                        // In examine mode, only render the selected keyframe
                        int ki_start = examining ? scrub : 0;
                        int ki_end = examining ? scrub + 1 : kf_count;

                        for (int ki = ki_start; ki < ki_end; ki++) {
                            // Distance-based fade: selected = 1.0, fades with distance
                            int dist = std::abs(ki - scrub);
                            float fade = examining ? 1.0f : expf(-0.0375f * (float)dist);
                            if (fade < 0.02f) continue;  // skip near-invisible clouds

                            std::vector<float> kf;
                            {
                                std::lock_guard<std::mutex> lock(dist_mutex_);
                                if (ki < (int)keyframe_history_.size())
                                    kf = keyframe_history_[ki];
                            }
                            if (kf.empty()) continue;

                            // Orbit offset — zero in examine mode
                            float off[3] = {0, 0, 0};
                            if (!examining) {
                                float age = (float)(scrub - ki);
                                float theta = age * as;
                                off[0] = or_ * sinf(theta);
                                off[1] = -or_ * (1.0f - cosf(theta));
                            }

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

                            // Draw trail + ghost branches on last pass (skip in examine mode)
                            // Draws all fork branches, each with its own orbit phase offset.
                            if (!examining && (ki == kf_count - 1 || ki == scrub)) {
                                std::lock_guard<std::mutex> lock(trail_mutex_);
                                int si = scrub;
                                float or_t = or_;
                                float as_t = as;

                                for (int bi = 0; bi < (int)branches_.size(); bi++) {
                                    auto& branch = branches_[bi];
                                    int trail_count = branch.trail_positions.size() / 3;
                                    uint32_t tc = (uint32_t)trail_count;
                                    float phase = branch.orbit_phase;

                                    if (trail_count >= 2 && trail_pipeline_) {
                                        auto trail_buf = device_->newBuffer(branch.trail_positions.data(),
                                            branch.trail_positions.size() * sizeof(float), MTL::ResourceStorageModeShared);
                                        if (trail_buf) {
                                            render_enc->setRenderPipelineState(trail_pipeline_);
                                            render_enc->setVertexBuffer(trail_buf, 0, 0);
                                            render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                                            render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                                            render_enc->setVertexBytes(&si, sizeof(int), 3);
                                            render_enc->setVertexBytes(&or_t, sizeof(float), 4);
                                            render_enc->setVertexBytes(&as_t, sizeof(float), 5);
                                            render_enc->setVertexBytes(&phase, sizeof(float), 6);
                                            render_enc->drawPrimitives(MTL::PrimitiveTypeLineStrip,
                                                (NS::UInteger)0, (NS::UInteger)trail_count);
                                            trail_buf->release();
                                        }
                                    }

                                    int ghost_vertex_count = branch.ghost_vertices.size() / 5;
                                    if (ghost_vertex_count >= 2 && ghost_pipeline_) {
                                        auto ghost_buf = device_->newBuffer(branch.ghost_vertices.data(),
                                            branch.ghost_vertices.size() * sizeof(float), MTL::ResourceStorageModeShared);
                                        if (ghost_buf) {
                                            render_enc->setRenderPipelineState(ghost_pipeline_);
                                            render_enc->setVertexBuffer(ghost_buf, 0, 0);
                                            render_enc->setVertexBytes(mvp, sizeof(mvp), 1);
                                            render_enc->setVertexBytes(&tc, sizeof(uint32_t), 2);
                                            render_enc->setVertexBytes(&si, sizeof(int), 3);
                                            render_enc->setVertexBytes(&or_t, sizeof(float), 4);
                                            render_enc->setVertexBytes(&as_t, sizeof(float), 5);
                                            render_enc->setVertexBytes(&phase, sizeof(float), 6);
                                            render_enc->drawPrimitives(MTL::PrimitiveTypeLine,
                                                (NS::UInteger)0, (NS::UInteger)ghost_vertex_count);
                                            ghost_buf->release();
                                        }
                                    }
                                }
                            }

                            // Mouse-driven cursor + highlight — only in examine mode
                            if (ki == scrub && examining) {
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

                            // Pick pass — only in examine mode
                            if (ki == scrub && examining && pick_pipeline_) {
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

                        auto png_data = tonemap_to_png(hdr_tex, w, h);
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
                    if (!mouse_active && !camera_.tracking) {
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

            was_idle = (t >= 1.0f) && mouse_x_.load(std::memory_order_acquire) < 0 && !camera_.tracking;
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
