// Stub for non-Apple platforms — Metal renderer requires macOS.
// All methods are no-ops. is_ready() returns false, so plugin.cpp
// skips all renderer paths (no glyph registration, no WebSocket frames).

#include "metal_renderer.h"

MetalRenderer::MetalRenderer() {}
MetalRenderer::~MetalRenderer() {}

bool MetalRenderer::setup() { return false; }
void MetalRenderer::teardown() {}
bool MetalRenderer::is_ready() const { return false; }
std::string MetalRenderer::device_name() const { return "none (no Metal)"; }

void MetalRenderer::set_vocab_positions(const float*, int) {}
void MetalRenderer::submit_distribution(const float*, int) {}
void MetalRenderer::store_keyframe(const float*, int) {}
void MetalRenderer::set_scrub_index(int) {}
void MetalRenderer::set_param(const std::string&, float) {}
void MetalRenderer::add_trail_point(int) {}
void MetalRenderer::clear_trail() {}
void MetalRenderer::add_ghost_branches(int, const std::vector<std::pair<int,float>>&) {}

void MetalRenderer::start_render_loop(int, int) {}
void MetalRenderer::stop_render_loop() {}

std::vector<uint8_t> MetalRenderer::render_nebula(const float*, int, int, int) { return {}; }
std::vector<uint8_t> MetalRenderer::render_lerp(int, int, float) { return {}; }
std::vector<uint8_t> MetalRenderer::render_test(int, int) { return {}; }

void MetalRenderer::set_latest_frame(std::vector<uint8_t>, int, int) {}
std::vector<uint8_t> MetalRenderer::get_latest_frame(int& w, int& h) { w = 0; h = 0; return {}; }
std::vector<uint8_t> MetalRenderer::wait_for_frame(int) { return {}; }
