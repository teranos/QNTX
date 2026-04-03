#pragma once

// TODO(#751): Ghost trail triangulation — connect high-scoring runner-ups with
//   filled triangles to create visible "decision surfaces."
// TODO(B64): WebSocket frames are base64-encoded PNG — 33% overhead. Binary
//   WebSocket frames would eliminate this.
// TODO(KFC): Keyframe history capped at 512 (64MB). Longer generations lose
//   early frames. No disk persistence — closing the glyph loses all history.
// TODO(TRU): Trail positions vector is unbounded while keyframes are capped.
// TODO(WSPT): WebSocket message parsing in plugin.cpp (mouse:, examine:, cam:,
//   scrub:) is string prefix matching with no tests. Extract and unit-test.
// TODO(KFNV): Keyframe navigation — [/] keys step between tokens in the
//   generation sequence. Camera animates to the new distribution's center
//   (subsumes CSNP). Works in both orange (scrub) and red (examine) modes.
//   JS finds adjacent token span, triggers scrubTo(). C++ animates camera.
// TODO(RNAV): Rank navigation — ,/. keys step through visible tokens sorted
//   by probability (descending). Sorted index rebuilt on keyframe change.
//   Pick (click) jumps to arbitrary rank. Camera nudges only when the
//   selected candidate is off-screen (pull to 10% from edge, don't center).
// TODO(CLST): Cluster navigation — 'c' toggles cluster mode overlay using
//   HDBSCAN on visible token positions. ,/. shifts meaning: step between
//   clusters (camera centers), press 'c' again to enter cluster (,/. steps
//   within). Escape backs out. Flat ,/. rank nav always available outside
//   c-mode. Builds on RNAV's sorted candidate index.
// TODO(DCUR): Cursor is a fixed-size screen-space quad — doesn't scale with
//   distance to the particle. Closer particles should get a larger cursor.
// TODO(STR): GPU-accelerated steering — Metal compute shader could modify the
//   logit buffer before sampling. Click a region of the nebula to boost tokens
//   in that region. Infrastructure exists (writable Metal buffer, sampler reads
//   from same memory) but no input→buffer→sampler path is wired.
// TODO(PVH): PCA projection accesses private llama-model.h header to read
//   tok_embd.weight. Version-fragile against llama.cpp internal changes.

#include "camera.h"

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

#ifdef __APPLE__
// Forward declarations — Metal-cpp types
namespace MTL {
    class Device;
    class CommandQueue;
    class ComputePipelineState;
    class RenderPipelineState;
    class RenderCommandEncoder;
    class Buffer;
    class Texture;
}
#endif

class MetalRenderer {
public:
    MetalRenderer();
    ~MetalRenderer();

    bool setup();
    void teardown();
    bool is_ready() const;
    std::string device_name() const;

    void set_vocab_positions(const float* positions, int vocab_size);

    // Submit a new probability distribution as a keyframe.
    // The previous distribution becomes the lerp source; this becomes the target.
    // The render loop interpolates between them at 60fps.
    void submit_distribution(const float* probabilities, int vocab_size);

    // Store a copy of the distribution for scrub playback.
    void store_keyframe(const float* probabilities, int vocab_size);

    // Set scrub target: index >= 0 renders that keyframe, -1 resumes live.
    void set_scrub_index(int idx);

    // Examine mode: isolate single keyframe (no orbit, no trail, no fade).
    void set_token_examine(bool examine);

    // Record the chosen token's position in the generation trail.
    void add_trail_point(int token_id);
    void clear_trail();

    // Record ghost branches: lines from the chosen token to runner-up positions.
    // Call after add_trail_point for the same generation step.
    void add_ghost_branches(int chosen_token_id,
                            const std::vector<std::pair<int,float>>& runners);

    // Camera: pan (dx/dy in world units), zoom (dz multiplicative),
    // rotation (dyaw/dpitch in radians).
    void apply_camera(float dx, float dy, float dz, float dyaw, float dpitch);
    void reset_camera();

    // Pick: read token ID at pixel coordinates. Returns -1 if no particle.
    int pick_at(int px, int py);

    // Mouse position for GPU-side cursor rendering. -1,-1 = no cursor.
    void set_mouse(int px, int py);

    // Set/clear the highlighted (hovered) token for visual feedback.
    void set_hovered_token(int token_id);
    int hovered_token() const;

    // Check if mouse has been idle for 400ms and a pick result is ready.
    // Returns token_id >= 0 if ready, -1 otherwise. Resets after read.
    int consume_pick_result();

    // Set the hover label text (called from plugin when pick fires).
    // Cleared automatically when mouse moves.
    void set_hover_label(const std::string& text);

    // Read probability of a token from the current distribution.
    float token_probability(int token_id);

    // Runtime-adjustable parameters
    void set_param(const std::string& key, float value);

    // Start/stop the background render loop (60fps interpolation).
    void start_render_loop(int width, int height);
    void stop_render_loop();

    // Render a probability distribution as a particle nebula. Returns PNG data.
    std::vector<uint8_t> render_nebula(const float* probabilities, int vocab_size,
                                        int width, int height);

    // Render with interpolated distributions. t=0 is probA, t=1 is probB.
    std::vector<uint8_t> render_lerp(int width, int height, float t);

    // Render with test data for verification.
    std::vector<uint8_t> render_test(int width, int height);

    // Store/retrieve the latest rendered frame (thread-safe).
    void set_latest_frame(std::vector<uint8_t> png, int width, int height);
    std::vector<uint8_t> get_latest_frame(int& width, int& height);

    // Block until a new frame is available (for WebSocket push).
    std::vector<uint8_t> wait_for_frame(int timeout_ms);

private:
#ifdef __APPLE__
    // Frame output
    std::mutex frame_mutex_;
    std::condition_variable frame_cv_;
    std::vector<uint8_t> latest_frame_;
    int frame_width_ = 0, frame_height_ = 0;
    uint64_t frame_seq_ = 0;

    // GPU resources
    MTL::Device* device_ = nullptr;
    MTL::CommandQueue* queue_ = nullptr;
    MTL::ComputePipelineState* compute_pipeline_ = nullptr;
    MTL::ComputePipelineState* lerp_pipeline_ = nullptr;
    MTL::RenderPipelineState* render_pipeline_ = nullptr;
    MTL::RenderPipelineState* trail_pipeline_ = nullptr;
    MTL::Buffer* positions_buffer_ = nullptr;
    MTL::Buffer* colors_buffer_ = nullptr;  // per-token RGB from PCA 4-6
    int vocab_size_ = 0;
    float center_x_ = 0, center_y_ = 0, extent_ = 1.0f;

    Camera camera_;

    void build_mvp(float* mvp, int width, int height);

    // Generation trail — chosen token positions
    std::vector<float> trail_positions_;  // flat float3 array
    std::mutex trail_mutex_;
    const float* vocab_positions_ptr_ = nullptr;  // borrowed pointer to cached positions

    // Ghost branches — runner-up paths at each generation step
    // Flat buffer: 5 floats per vertex [x, y, z, prob, trail_index]
    // Vertices come in pairs (chosen->runner-up) drawn as Line primitives
    std::vector<float> ghost_vertices_;
    MTL::RenderPipelineState* ghost_pipeline_ = nullptr;

    // Pick buffer — R32Uint texture for hover identification
    MTL::RenderPipelineState* pick_pipeline_ = nullptr;
    MTL::Texture* pick_texture_ = nullptr;
    MTL::Texture* pick_depth_ = nullptr;
    int pick_width_ = 0, pick_height_ = 0;
    std::mutex pick_mutex_;

    // Mouse position in render-texture pixels (-1 = no cursor)
    std::atomic<int> mouse_x_{-1}, mouse_y_{-1};

    // Highlight — square around hovered particle
    MTL::RenderPipelineState* highlight_pipeline_ = nullptr;
    // Cursor — persistent crosshair with semi-transparent fill
    MTL::RenderPipelineState* cursor_pipeline_ = nullptr;
    std::atomic<int> hovered_token_{-1};

    // Label rendering — textured quad pipeline
    MTL::RenderPipelineState* label_pipeline_ = nullptr;
    void render_label(MTL::RenderCommandEncoder* enc, const std::string& text,
                      float screen_x, float screen_y, int width, int height);

    // Mouse idle timer for debounced pick response
    std::chrono::steady_clock::time_point mouse_last_move_;
    bool pick_sent_ = false;  // true after sending picked: for current idle

    // Hover label — set by plugin when pick fires, cleared on mouse move
    std::string hover_label_;
    std::mutex hover_label_mutex_;

    void ensure_pick_textures(int width, int height);

    // Interpolation state
    MTL::Buffer* prob_a_ = nullptr;  // previous distribution
    MTL::Buffer* prob_b_ = nullptr;  // current distribution
    std::mutex dist_mutex_;
    std::chrono::steady_clock::time_point keyframe_time_;
    std::chrono::milliseconds keyframe_interval_{100};  // ~10 tok/s default

    // Scrub playback — CPU-side keyframe history
    std::vector<std::vector<float>> keyframe_history_;  // one distribution per token
    std::atomic<int> scrub_index_{-1};  // -1 = live mode
    std::atomic<bool> token_examine_{false};  // true = isolate single keyframe

    // Drift — fixed-step camera offset per token, makes time into space
    int drift_count_ = 0;        // how many tokens have been recorded
    float drift_step_ = 0.0f;    // world-space units per token (set after positions loaded)

    // Orbit — trail curves along a circular arc
    int orbit_period_ = 1024;    // tokens per full 360° rotation
    float orbit_radius_mult_ = 3.0f;  // orbit radius as multiple of extent
    float particle_scale_ = 1.0f;     // multiplier on particle point size

    // Render loop
    std::thread render_thread_;
    std::atomic<bool> render_running_{false};
    int render_width_ = 800, render_height_ = 600;

    // Idle suppression — sleep when interpolation is complete and no new data
    std::condition_variable render_wake_cv_;
    std::mutex render_wake_mutex_;
    bool render_dirty_ = false;  // new data arrived, need to render

#endif
};
