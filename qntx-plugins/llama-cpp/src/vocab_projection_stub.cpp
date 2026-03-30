// Stub for non-Apple platforms — PCA/HYP projection requires Accelerate BLAS.
// vocab_positions_3d() and vocab_positions_hyp() return empty, renderer won't receive positions.

#include "plugin.h"

void InferenceEngine::compute_vocab_positions() {}
void InferenceEngine::compute_hyperbolic_positions() {}
bool InferenceEngine::load_vocab_cache() { return false; }
void InferenceEngine::write_vocab_cache() {}
bool InferenceEngine::load_hyp_cache() { return false; }
void InferenceEngine::write_hyp_cache() {}

const std::vector<float>& InferenceEngine::vocab_positions_3d() {
    return vocab_positions_;
}

const std::vector<float>& InferenceEngine::vocab_positions_hyp() {
    return vocab_positions_hyp_;
}

const std::vector<float>& InferenceEngine::active_positions() {
    return vocab_positions_;
}

void InferenceEngine::set_projection_mode(ProjectionMode mode) {
    projection_mode_ = mode;
}
