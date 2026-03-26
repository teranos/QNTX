#pragma once

#include <condition_variable>
#include <cstdint>
#include <mutex>
#include <string>
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

    // Render a probability distribution as a particle nebula. Returns PNG data.
    std::vector<uint8_t> render_nebula(const float* probabilities, int vocab_size,
                                        int width, int height);

    // Render with test data for verification.
    std::vector<uint8_t> render_test(int width, int height);

    // Store/retrieve the latest rendered frame (thread-safe).
    void set_latest_frame(std::vector<uint8_t> png, int width, int height);
    std::vector<uint8_t> get_latest_frame(int& width, int& height);

    // Block until a new frame is available (for WebSocket push).
    // Returns false if timed out (no new frame within timeout_ms).
    std::vector<uint8_t> wait_for_frame(int timeout_ms);

private:
    std::mutex frame_mutex_;
    std::condition_variable frame_cv_;
    std::vector<uint8_t> latest_frame_;
    int frame_width_ = 0, frame_height_ = 0;
    uint64_t frame_seq_ = 0;
    MTL::Device* device_ = nullptr;
    MTL::CommandQueue* queue_ = nullptr;
    MTL::ComputePipelineState* compute_pipeline_ = nullptr;
    MTL::RenderPipelineState* render_pipeline_ = nullptr;
    MTL::Buffer* positions_buffer_ = nullptr;
    int vocab_size_ = 0;
    float center_x_ = 0, center_y_ = 0, extent_ = 1.0f;
};
