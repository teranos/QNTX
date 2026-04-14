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

MetalRenderer::MetalRenderer() {
    // Initialize root branch
    branches_.push_back(BranchRenderData{});
}

MetalRenderer::~MetalRenderer() {
    teardown();
}

bool MetalRenderer::setup() {
    std::cout << "[metal-scry] setup() entered" << std::endl;

    // Clean up any partial state from a previous failed attempt
    if (device_ || queue_ || compute_pipeline_ || render_pipeline_) {
        std::cout << "[metal-scry] Cleaning up partial state from previous attempt" << std::endl;
        teardown();
    }

    device_ = MTL::CreateSystemDefaultDevice();
    if (!device_) {
        std::cout << "[metal-scry] MTL::CreateSystemDefaultDevice() returned null" << std::endl;
        return false;
    }
    std::cout << "[metal-scry] Device acquired: " << device_->name()->utf8String() << std::endl;

    queue_ = device_->newCommandQueue();
    if (!queue_) {
        std::cout << "[metal-scry] newCommandQueue() returned null" << std::endl;
        return false;
    }
    std::cout << "[metal-scry] Command queue created" << std::endl;

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
            std::cout << "[metal-scry] Loaded pre-compiled " << lib_path << std::endl;
        }
    }

    if (!library) {
        // Runtime compilation from embedded source
        error = nullptr;
        auto src = NS::String::string(kShaderSource, NS::UTF8StringEncoding);
        library = device_->newLibrary(src, nullptr, &error);
        if (!library) {
            if (error) {
                std::cout << "[metal-scry] Shader compilation failed: "
                          << error->localizedDescription()->utf8String() << std::endl;
            }
            return false;
        }
        std::cout << "[metal-scry] Compiled shaders from source" << std::endl;
    }

    // Compute pipeline
    auto compute_fn = library->newFunction(NS::String::string("particleCompute", NS::UTF8StringEncoding));
    if (!compute_fn) {
        std::cout << "[metal-scry] particleCompute not found" << std::endl;
        return false;
    }
    compute_pipeline_ = device_->newComputePipelineState(compute_fn, &error);
    compute_fn->release();
    if (!compute_pipeline_) return false;

    // Lerp compute pipeline
    auto lerp_fn = library->newFunction(NS::String::string("particleComputeLerp", NS::UTF8StringEncoding));
    if (!lerp_fn) {
        std::cout << "[metal-scry] particleComputeLerp not found" << std::endl;
        return false;
    }
    lerp_pipeline_ = device_->newComputePipelineState(lerp_fn, &error);
    lerp_fn->release();
    if (!lerp_pipeline_) return false;

    // Render pipeline
    auto vertex_fn = library->newFunction(NS::String::string("particleVertex", NS::UTF8StringEncoding));
    auto fragment_fn = library->newFunction(NS::String::string("particleFragment", NS::UTF8StringEncoding));
    if (!vertex_fn || !fragment_fn) {
        std::cout << "[metal-scry] vertex/fragment functions not found" << std::endl;
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
        std::cout << "[metal-scry] trail vertex/fragment functions not found" << std::endl;
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
    auto ghost_fragment_fn = library->newFunction(NS::String::string("ghostFragment", NS::UTF8StringEncoding));
    if (!ghost_vertex_fn || !ghost_fragment_fn) {
        std::cout << "[metal-scry] ghost branch shaders not found" << std::endl;
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
        std::cout << "[metal-scry] pick shaders not found" << std::endl;
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
        std::cout << "[metal-scry] highlight shaders not found" << std::endl;
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

    std::cout << "[metal-scry] GPU ready: " << device_name() << std::endl;
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

// render_nebula, render_test, render_lerp, start_render_loop, stop_render_loop
// moved to metal_render_pass.cpp

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

void MetalRenderer::add_trail_point(int token_id, int branch_id) {
    if (!vocab_positions_ptr_ || token_id < 0 || token_id >= vocab_size_) return;

    std::lock_guard<std::mutex> lock(trail_mutex_);
    float x = vocab_positions_ptr_[token_id * 6];
    float y = vocab_positions_ptr_[token_id * 6 + 1];
    float z = vocab_positions_ptr_[token_id * 6 + 2];

    // Always write to root trail (used by existing render paths)
    if (branch_id == 0) {
        trail_positions_.push_back(x);
        trail_positions_.push_back(y);
        trail_positions_.push_back(z);
    }

    // Write to per-branch data
    if (branch_id >= 0 && branch_id < (int)branches_.size()) {
        branches_[branch_id].trail_positions.push_back(x);
        branches_[branch_id].trail_positions.push_back(y);
        branches_[branch_id].trail_positions.push_back(z);
    }
}

void MetalRenderer::clear_trail() {
    std::lock_guard<std::mutex> lock(trail_mutex_);
    trail_positions_.clear();
    ghost_vertices_.clear();
    // Clear all branches, keep root
    branches_.clear();
    branches_.push_back(BranchRenderData{});
    active_branch_ = 0;
    drift_count_ = 0;
    {
        std::lock_guard<std::mutex> dlock(dist_mutex_);
        keyframe_history_.clear();
    }
}

void MetalRenderer::add_ghost_branches(int chosen_token_id,
                                        const std::vector<std::pair<int,float>>& runners,
                                        int branch_id) {
    if (!vocab_positions_ptr_ || chosen_token_id < 0 || chosen_token_id >= vocab_size_) return;

    std::lock_guard<std::mutex> lock(trail_mutex_);

    // Determine trail index from the target branch
    auto& target_ghosts = (branch_id >= 0 && branch_id < (int)branches_.size())
        ? branches_[branch_id].ghost_vertices : ghost_vertices_;
    auto& target_trail = (branch_id >= 0 && branch_id < (int)branches_.size())
        ? branches_[branch_id].trail_positions : trail_positions_;

    int trail_index = (int)(target_trail.size() / 3) - 1;
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
        target_ghosts.push_back(cx);
        target_ghosts.push_back(cy);
        target_ghosts.push_back(cz);
        target_ghosts.push_back(prob);
        target_ghosts.push_back(ti);

        // To vertex (runner-up position)
        target_ghosts.push_back(rx);
        target_ghosts.push_back(ry);
        target_ghosts.push_back(rz);
        target_ghosts.push_back(prob);
        target_ghosts.push_back(ti);
    }

    // Also mirror to root ghost_vertices_ for backward compat
    if (branch_id == 0) {
        ghost_vertices_ = branches_[0].ghost_vertices;
    }
}

void MetalRenderer::store_keyframe(const float* probabilities, int vocab_size, int branch_id) {
    std::lock_guard<std::mutex> lock(dist_mutex_);
    if (keyframe_history_.size() >= 512) return;  // cap at 512 entries

    std::vector<float> kf(probabilities, probabilities + vocab_size);

    // Store in per-branch data
    if (branch_id >= 0 && branch_id < (int)branches_.size()) {
        if (branches_[branch_id].keyframes.size() < 512) {
            branches_[branch_id].keyframes.push_back(kf);
        }
    }

    // Also store in flat keyframe_history_ (active branch for scrub)
    if (branch_id == active_branch_) {
        keyframe_history_.push_back(std::move(kf));
    }

    drift_count_++;
}

int MetalRenderer::add_fork_branch(int parent_branch_id, int fork_position) {
    std::lock_guard<std::mutex> lock(trail_mutex_);

    BranchRenderData branch;
    // Copy parent trail up to fork_position
    if (parent_branch_id >= 0 && parent_branch_id < (int)branches_.size()) {
        auto& parent = branches_[parent_branch_id];
        int copy_floats = std::min(fork_position * 3, (int)parent.trail_positions.size());
        branch.trail_positions.assign(
            parent.trail_positions.begin(),
            parent.trail_positions.begin() + copy_floats);
        // Phase offset: parent phase + π/2
        branch.orbit_phase = parent.orbit_phase + (float)M_PI / 2.0f;
        branch.active = true;
    }

    int id = (int)branches_.size();
    branches_.push_back(std::move(branch));
    return id;
}

int MetalRenderer::branch_count() const {
    return (int)branches_.size();
}

void MetalRenderer::set_active_branch(int branch_id) {
    if (branch_id >= 0 && branch_id < (int)branches_.size()) {
        // Mark old active as inactive
        if (active_branch_ < (int)branches_.size()) {
            branches_[active_branch_].active = false;
        }
        active_branch_ = branch_id;
        branches_[branch_id].active = true;

        // Switch keyframe_history_ to the new branch's keyframes for scrub
        std::lock_guard<std::mutex> dlock(dist_mutex_);
        keyframe_history_ = branches_[branch_id].keyframes;
    }
}

void MetalRenderer::set_scrub_index(int idx) {
    scrub_index_.store(idx, std::memory_order_release);
    ranked_dirty_ = true;
    candidate_rank_ = -1;
    // Invalidate pick texture — new keyframe, new distribution
    {
        std::lock_guard<std::mutex> plock(pick_mutex_);
        if (pick_texture_) { pick_texture_->release(); pick_texture_ = nullptr; }
        if (pick_depth_) { pick_depth_->release(); pick_depth_ = nullptr; }
    }
    hovered_token_.store(-1, std::memory_order_release);
    // In examine mode, animate camera to the new distribution's center
    if (token_examine_.load(std::memory_order_acquire) && idx >= 0) {
        camera_.fly_to(glm::vec3(center_x_, center_y_, 0.0f));
    }
    // Wake render loop for scrub
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

void MetalRenderer::set_token_examine(bool focused) {
    bool was_focused = token_examine_.exchange(focused, std::memory_order_release);
    if (focused && !was_focused) {
        // Fly camera to distribution center instead of snapping
        camera_.fly_to(glm::vec3(center_x_, center_y_, 0.0f));
    }
    // Invalidate pick texture so stale picks from previous mode don't linger
    {
        std::lock_guard<std::mutex> plock(pick_mutex_);
        if (pick_texture_) { pick_texture_->release(); pick_texture_ = nullptr; }
        if (pick_depth_) { pick_depth_->release(); pick_depth_ = nullptr; }
    }
    hovered_token_.store(-1, std::memory_order_release);
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

// render_lerp, start_render_loop, stop_render_loop — in metal_render_pass.cpp

// Pick, hover, label, rank navigation, and mouse methods live in metal_pick.cpp
