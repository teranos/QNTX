// Stub for non-Apple platforms — PCA projection requires Accelerate BLAS.
// vocab_positions_3d() returns empty, renderer won't receive positions.

#include "plugin.h"

void InferenceEngine::compute_vocab_positions() {}

const std::vector<float>& InferenceEngine::vocab_positions_3d() {
    return vocab_positions_;
}
