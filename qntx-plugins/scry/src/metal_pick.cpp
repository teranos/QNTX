// Pick buffer, cursor, highlight, label rendering, rank navigation.
// Split from metal_renderer.cpp — all methods operate on MetalRenderer state.

#include "metal_renderer.h"

#ifdef __APPLE__

#include <Foundation/Foundation.hpp>
#include <Metal/Metal.hpp>

#include <algorithm>
#include <cmath>
#include <cstring>

#include <CoreGraphics/CoreGraphics.h>
#include <CoreText/CoreText.h>

#define GLM_FORCE_DEPTH_ZERO_TO_ONE
#include <glm/glm.hpp>

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

// ── Rank navigation ─────────────────────────────────────────────────

void MetalRenderer::rebuild_ranked_index() {
    std::lock_guard<std::mutex> lock(dist_mutex_);
    ranked_tokens_.clear();
    if (!prob_b_ || vocab_size_ <= 0) { ranked_dirty_ = false; return; }

    auto* probs = (float*)prob_b_->contents();
    float threshold = 1e-5f;

    // Collect visible tokens
    for (int i = 0; i < vocab_size_; i++) {
        if (probs[i] > threshold) ranked_tokens_.push_back(i);
    }

    // Sort descending by probability
    std::sort(ranked_tokens_.begin(), ranked_tokens_.end(),
        [probs](int a, int b) { return probs[a] > probs[b]; });

    ranked_dirty_ = false;
}

void MetalRenderer::nudge_camera_to_token(int token_id) {
    if (!positions_buffer_ || token_id < 0 || token_id >= vocab_size_) return;

    auto* pos = (float*)positions_buffer_->contents();
    float tx = pos[token_id * 3];
    float ty = pos[token_id * 3 + 1];
    float tz = pos[token_id * 3 + 2];

    // Check if the token is within the view frustum
    float mvp[16];
    build_mvp(mvp, render_width_, render_height_);
    glm::mat4 mvp_mat;
    std::memcpy(&mvp_mat[0][0], mvp, 16 * sizeof(float));
    glm::vec4 clip = mvp_mat * glm::vec4(tx, ty, tz, 1.0f);

    if (clip.w > 0) {
        float ndc_x = clip.x / clip.w;
        float ndc_y = clip.y / clip.w;
        // If within 80% of screen (10% margin on each side), no nudge needed
        if (fabsf(ndc_x) < 0.8f && fabsf(ndc_y) < 0.8f) return;
    }

    // Token is off-screen or behind camera — fly toward it
    camera_.fly_to(glm::vec3(tx, ty, tz));
    {
        std::lock_guard<std::mutex> lock(render_wake_mutex_);
        render_dirty_ = true;
    }
    render_wake_cv_.notify_one();
}

int MetalRenderer::step_candidate(int dir) {
    if (!token_examine_.load(std::memory_order_acquire)) return -1;

    if (ranked_dirty_) rebuild_ranked_index();
    if (ranked_tokens_.empty()) return -1;

    int new_rank = candidate_rank_ + dir;
    if (new_rank < 0) new_rank = 0;
    if (new_rank >= (int)ranked_tokens_.size()) return -1;

    candidate_rank_ = new_rank;
    int token_id = ranked_tokens_[new_rank];
    set_hovered_token(token_id);
    nudge_camera_to_token(token_id);
    return token_id;
}

void MetalRenderer::select_candidate(int token_id) {
    if (ranked_dirty_) rebuild_ranked_index();

    // Find rank of this token
    for (int i = 0; i < (int)ranked_tokens_.size(); i++) {
        if (ranked_tokens_[i] == token_id) {
            candidate_rank_ = i;
            return;
        }
    }
    // Token not in visible set — reset rank
    candidate_rank_ = -1;
}

// ── Hover label + mouse ─────────────────────────────────────────────

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

// ── Label rendering ─────────────────────────────────────────────────

void MetalRenderer::render_label(MTL::RenderCommandEncoder* enc, const std::string& text,
                                  float screen_x, float screen_y, int width, int height) {
    if (!label_pipeline_ || text.empty()) return;

    // Cache: only rasterize when text changes
    if (text != label_cache_text_ || !label_cache_tex_) {
        if (label_cache_tex_) { label_cache_tex_->release(); label_cache_tex_ = nullptr; }

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

        int tex_w = (int)ceil(line_width) + 8;
        int tex_h = (int)ceil(ascent + descent) + 6;

        auto colorspace = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        auto cg_ctx = CGBitmapContextCreate(nullptr, tex_w, tex_h, 8, tex_w * 4, colorspace,
                                             kCGImageAlphaPremultipliedLast);
        if (!cg_ctx) {
            CFRelease(line); CFRelease(attr_str); CFRelease(attrs);
            CFRelease(cf_text); CFRelease(font); CGColorSpaceRelease(colorspace);
            return;
        }

        CGContextSetRGBFillColor(cg_ctx, 0.05, 0.05, 0.1, 0.5);
        CGContextFillRect(cg_ctx, CGRectMake(0, 0, tex_w, tex_h));

        CGContextSetRGBFillColor(cg_ctx, 0.9, 0.95, 0.95, 0.9);
        CGContextSetTextPosition(cg_ctx, 4, descent + 3);
        CTLineDraw(line, cg_ctx);

        auto* pixels = (uint8_t*)CGBitmapContextGetData(cg_ctx);

        auto td = MTL::TextureDescriptor::texture2DDescriptor(
            MTL::PixelFormatRGBA8Unorm, tex_w, tex_h, false);
        td->setUsage(MTL::TextureUsageShaderRead);
        td->setStorageMode(MTL::StorageModeShared);
        label_cache_tex_ = device_->newTexture(td);
        label_cache_tex_->replaceRegion(MTL::Region(0, 0, tex_w, tex_h), 0, pixels, tex_w * 4);

        label_cache_text_ = text;
        label_cache_w_ = tex_w;
        label_cache_h_ = tex_h;

        CGContextRelease(cg_ctx);
        CGColorSpaceRelease(colorspace);
        CFRelease(line);
        CFRelease(attr_str);
        CFRelease(attrs);
        CFRelease(cf_text);
        CFRelease(font);
    }

    // Draw cached texture as textured quad
    float rect_data[4] = { screen_x, screen_y, (float)label_cache_w_, (float)label_cache_h_ };
    float viewport[2] = { (float)width, (float)height };

    enc->setRenderPipelineState(label_pipeline_);
    enc->setVertexBytes(rect_data, sizeof(rect_data), 0);
    enc->setVertexBytes(viewport, sizeof(viewport), 1);
    enc->setFragmentTexture(label_cache_tex_, 0);
    enc->drawPrimitives(MTL::PrimitiveTypeTriangle, (NS::UInteger)0, (NS::UInteger)6);
}

#endif // __APPLE__
