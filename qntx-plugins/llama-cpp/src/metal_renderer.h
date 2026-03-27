#pragma once

// TODO(GHB): Ghost branches — draw faint trails from chosen token to top-k
//   runner-up positions at each generation step. Data exists in TokenSignal.top_k.
// TODO(B64): WebSocket frames are base64-encoded PNG — 33% overhead. Binary
//   WebSocket frames would eliminate this.
// TODO(CAM): No camera control — fixed orthographic MVP auto-fitted to bounds.
//   Interactive rotation/zoom/pan would let users explore vocabulary space.
// TODO(KFC): Keyframe history capped at 512 (64MB). Longer generations lose
//   early frames. No disk persistence — closing the glyph loses all history.
// TODO(TRU): Trail positions vector is unbounded while keyframes are capped.
// TODO(STR): GPU-accelerated steering — Metal compute shader could modify the
//   logit buffer before sampling. Click a region of the nebula to boost tokens
//   in that region. Infrastructure exists (writable Metal buffer, sampler reads
//   from same memory) but no input→buffer→sampler path is wired.
// TODO(PVH): PCA projection accesses private llama-model.h header to read
//   tok_embd.weight. Version-fragile against llama.cpp internal changes.

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
    class Buffer;
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

    // Record the chosen token's position in the generation trail.
    void add_trail_point(int token_id);
    void clear_trail();

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
    int vocab_size_ = 0;
    float center_x_ = 0, center_y_ = 0, extent_ = 1.0f;

    // Generation trail — chosen token positions
    std::vector<float> trail_positions_;  // flat float3 array
    std::mutex trail_mutex_;
    const float* vocab_positions_ptr_ = nullptr;  // borrowed pointer to cached positions

    // Interpolation state
    MTL::Buffer* prob_a_ = nullptr;  // previous distribution
    MTL::Buffer* prob_b_ = nullptr;  // current distribution
    std::mutex dist_mutex_;
    std::chrono::steady_clock::time_point keyframe_time_;
    std::chrono::milliseconds keyframe_interval_{100};  // ~10 tok/s default

    // Scrub playback — CPU-side keyframe history
    std::vector<std::vector<float>> keyframe_history_;  // one distribution per token
    int scrub_index_ = -1;  // -1 = live mode

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
