#include "plugin.h"

#include <cmath>
#include <cstring>
#include <fstream>
#include <iostream>
#include <random>
#include <vector>

#include "llama-model.h"
#include "ggml.h"
#include "ggml-backend.h"

#ifdef __APPLE__
#include <Accelerate/Accelerate.h>
#else
#include <cblas.h>
#endif

// Power iteration with BLAS matrix-vector multiply.
static void power_iteration(const float* mat, int n,
                            std::vector<float>& vec, int iters = 100) {
    std::mt19937 rng(42);
    std::normal_distribution<float> dist(0.0f, 1.0f);
    vec.resize(n);
    for (int i = 0; i < n; i++) vec[i] = dist(rng);

    std::vector<float> out(n);
    for (int it = 0; it < iters; it++) {
        // out = mat * vec
        cblas_sgemv(CblasRowMajor, CblasNoTrans, n, n,
                    1.0f, mat, n, vec.data(), 1, 0.0f, out.data(), 1);
        float norm = cblas_snrm2(n, out.data(), 1);
        if (norm < 1e-10f) break;
        cblas_sscal(n, 1.0f / norm, out.data(), 1);
        std::swap(vec, out);
    }
}

// Remove the component along eigenvector from the covariance matrix.
static void deflate(float* mat, int n, const std::vector<float>& vec) {
    // lambda = vec^T * mat * vec
    std::vector<float> mv(n);
    cblas_sgemv(CblasRowMajor, CblasNoTrans, n, n,
                1.0f, mat, n, vec.data(), 1, 0.0f, mv.data(), 1);
    float lambda = cblas_sdot(n, vec.data(), 1, mv.data(), 1);
    // mat -= lambda * vec * vec^T
    cblas_sger(CblasRowMajor, n, n, -lambda, vec.data(), 1, vec.data(), 1, mat, n);
}

// Try to load cached positions from <model_path>.vocab3d.
// Returns true if cache was valid and loaded into vocab_positions_.
bool InferenceEngine::load_vocab_cache() {
    std::string cache_path = model_path_ + ".vocab3d";
    std::ifstream f(cache_path, std::ios::binary | std::ios::ate);
    if (!f.is_open()) return false;

    auto file_size = f.tellg();
    f.seekg(0);

    // File must contain exactly n_vocab × 3 floats
    if (!model_) return false;
    struct ggml_tensor* tok_embd = model_->tok_embd;
    if (!tok_embd) return false;
    int n_vocab = tok_embd->ne[1];

    size_t expected = static_cast<size_t>(n_vocab) * 3 * sizeof(float);
    if (static_cast<size_t>(file_size) != expected) {
        std::cout << "[llama-cpp] Cache size mismatch: " << file_size
                  << " bytes, expected " << expected << " for n_vocab=" << n_vocab << std::endl;
        return false;
    }

    vocab_positions_.resize(n_vocab * 3);
    f.read(reinterpret_cast<char*>(vocab_positions_.data()), expected);
    if (!f.good()) {
        vocab_positions_.clear();
        return false;
    }
    return true;
}

void InferenceEngine::write_vocab_cache() {
    std::string cache_path = model_path_ + ".vocab3d";
    std::ofstream f(cache_path, std::ios::binary | std::ios::trunc);
    if (!f.is_open()) {
        std::cout << "[llama-cpp] Failed to write cache: " << cache_path << std::endl;
        return;
    }
    f.write(reinterpret_cast<const char*>(vocab_positions_.data()),
            vocab_positions_.size() * sizeof(float));
    std::cout << "[llama-cpp] Wrote vocab cache: " << cache_path
              << " (" << vocab_positions_.size() / 3 << " positions)" << std::endl;
}

void InferenceEngine::compute_vocab_positions() {
    if (!model_) return;

    struct ggml_tensor* tok_embd = model_->tok_embd;
    if (!tok_embd) {
        std::cout << "[llama-cpp] tok_embd tensor not found" << std::endl;
        return;
    }

    int n_vocab = tok_embd->ne[1];
    int n_embd = tok_embd->ne[0];
    std::cout << "[llama-cpp] Computing vocab positions: " << n_vocab
              << " tokens × " << n_embd << " dims" << std::endl;

    // Dequantize the embedding matrix to a contiguous f32 buffer (n_vocab × n_embd)
    size_t row_size = ggml_row_size(tok_embd->type, n_embd);
    std::vector<uint8_t> raw_row(row_size);
    std::vector<float> X(n_vocab * n_embd);

    auto* traits = ggml_get_type_traits(tok_embd->type);
    for (int i = 0; i < n_vocab; i++) {
        float* dst = X.data() + i * n_embd;
        ggml_backend_tensor_get(tok_embd, raw_row.data(), i * row_size, row_size);
        if (tok_embd->type == GGML_TYPE_F32) {
            memcpy(dst, raw_row.data(), n_embd * sizeof(float));
        } else {
            traits->to_float(raw_row.data(), dst, n_embd);
        }
    }

    // Center: compute mean per dimension, subtract
    std::vector<float> mean(n_embd, 0.0f);
    for (int i = 0; i < n_vocab; i++)
        for (int j = 0; j < n_embd; j++)
            mean[j] += X[i * n_embd + j];
    float inv_n = 1.0f / n_vocab;
    for (int j = 0; j < n_embd; j++) mean[j] *= inv_n;
    for (int i = 0; i < n_vocab; i++)
        for (int j = 0; j < n_embd; j++)
            X[i * n_embd + j] -= mean[j];

    // Covariance: cov = (1/n) * X^T * X  (n_embd × n_embd)
    // BLAS: sgemm computes C = alpha * A^T * B + beta * C
    int d = n_embd;
    std::vector<float> cov(d * d, 0.0f);
    cblas_sgemm(CblasRowMajor, CblasTrans, CblasNoTrans,
                d, d, n_vocab,
                inv_n, X.data(), d, X.data(), d,
                0.0f, cov.data(), d);

    // Top 3 principal components via power iteration + deflation
    std::vector<std::vector<float>> pcs(3);
    for (int pc = 0; pc < 3; pc++) {
        power_iteration(cov.data(), d, pcs[pc]);
        deflate(cov.data(), d, pcs[pc]);
    }

    // Project: positions = X * [pc0 | pc1 | pc2]
    // Build projection matrix (n_embd × 3)
    std::vector<float> proj(d * 3);
    for (int j = 0; j < d; j++) {
        proj[j * 3 + 0] = pcs[0][j];
        proj[j * 3 + 1] = pcs[1][j];
        proj[j * 3 + 2] = pcs[2][j];
    }

    // positions = X * proj  (n_vocab × 3)
    vocab_positions_.resize(n_vocab * 3);
    cblas_sgemm(CblasRowMajor, CblasNoTrans, CblasNoTrans,
                n_vocab, 3, d,
                1.0f, X.data(), d, proj.data(), 3,
                0.0f, vocab_positions_.data(), 3);

    std::cout << "[llama-cpp] Vocab positions computed (" << n_vocab << " × 3)" << std::endl;

    write_vocab_cache();
}

const std::vector<float>& InferenceEngine::vocab_positions_3d() {
    // No mutex_ here — this must not block inference.
    // Only called from the background PCA thread and HTTP handlers.
    // vocab_positions_ is written once, then read-only; pca_ready_ provides ordering.
    if (vocab_positions_.empty() && model_) {
        if (load_vocab_cache()) {
            std::cout << "[llama-cpp] Loaded vocab positions from cache ("
                      << vocab_positions_.size() / 3 << " positions)" << std::endl;
        } else {
            compute_vocab_positions();
        }
    }
    return vocab_positions_;
}
