#pragma once

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

// Forward declarations — Metal-cpp types
namespace MTL {
    class Device;
    class CommandQueue;
    class ComputePipelineState;
    class RenderPipelineState;
    class Buffer;
}

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
    MTL::Buffer* positions_buffer_ = nullptr;
    int vocab_size_ = 0;
    float center_x_ = 0, center_y_ = 0, extent_ = 1.0f;

    // Interpolation state
    MTL::Buffer* prob_a_ = nullptr;  // previous distribution
    MTL::Buffer* prob_b_ = nullptr;  // current distribution
    std::mutex dist_mutex_;
    std::chrono::steady_clock::time_point keyframe_time_;
    std::chrono::milliseconds keyframe_interval_{100};  // ~10 tok/s default

    // Render loop
    std::thread render_thread_;
    std::atomic<bool> render_running_{false};
    int render_width_ = 800, render_height_ = 600;
};
