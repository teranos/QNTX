#pragma once

#include <cstdint>
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

private:
    MTL::Device* device_ = nullptr;
    MTL::CommandQueue* queue_ = nullptr;
    MTL::ComputePipelineState* compute_pipeline_ = nullptr;
    MTL::RenderPipelineState* render_pipeline_ = nullptr;
    MTL::Buffer* positions_buffer_ = nullptr;
    int vocab_size_ = 0;
    float center_x_ = 0, center_y_ = 0, extent_ = 1.0f;
};
