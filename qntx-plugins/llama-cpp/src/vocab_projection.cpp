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

// ─── Poincaré ball (hyperbolic) embeddings ───────────────────────────────
//
// Maps the token vocabulary into a 3D Poincaré ball where:
//   - Center = high-frequency common tokens
//   - Boundary (at infinity) = rare, specific tokens
//   - Angular position = semantic domain
//
// Algorithm:
//   1. Start from PCA 3D positions (preserves angular/semantic structure)
//   2. Map into the ball via exponential map at origin: exp_0(v) = tanh(||v||/2) * v/||v||
//   3. Build k-NN graph from cosine similarities of original embeddings
//   4. Refine via Riemannian SGD to preserve neighborhood structure in hyperbolic space
//   5. Cache as .vocab3d.hyp

static constexpr int HYP_K = 15;           // neighbors per token
static constexpr int HYP_NEG_SAMPLES = 10; // negative samples per edge
static constexpr int HYP_EPOCHS = 200;
static constexpr int HYP_BURNIN = 20;
static constexpr float HYP_LR = 0.01f;
static constexpr float HYP_BURNIN_LR = 0.001f;
static constexpr float HYP_EPS = 1e-5f;    // boundary clamp
static constexpr float HYP_INIT_SCALE = 0.5f;  // exp map scale for initialization

// Poincaré distance between two 3D points inside the unit ball.
static float poincare_dist(const float* u, const float* v) {
    float diff_sq = 0, u_sq = 0, v_sq = 0;
    for (int i = 0; i < 3; i++) {
        float d = u[i] - v[i];
        diff_sq += d * d;
        u_sq += u[i] * u[i];
        v_sq += v[i] * v[i];
    }
    float denom = (1.0f - u_sq) * (1.0f - v_sq);
    if (denom < HYP_EPS) denom = HYP_EPS;
    float arg = 1.0f + 2.0f * diff_sq / denom;
    if (arg < 1.0f) arg = 1.0f;
    return std::acosh(arg);
}

// Gradient of poincare_dist with respect to u (3D vector written to grad).
static void poincare_grad(const float* u, const float* v, float* grad) {
    float diff_sq = 0, u_sq = 0, v_sq = 0;
    for (int i = 0; i < 3; i++) {
        float d = u[i] - v[i];
        diff_sq += d * d;
        u_sq += u[i] * u[i];
        v_sq += v[i] * v[i];
    }
    float alpha = 1.0f - u_sq;
    float beta  = 1.0f - v_sq;
    if (alpha < HYP_EPS) alpha = HYP_EPS;
    if (beta  < HYP_EPS) beta  = HYP_EPS;

    float gamma = 1.0f + 2.0f * diff_sq / (alpha * beta);
    if (gamma < 1.0f + HYP_EPS) gamma = 1.0f + HYP_EPS;
    float sqrt_g = std::sqrt(gamma * gamma - 1.0f);
    if (sqrt_g < HYP_EPS) sqrt_g = HYP_EPS;

    float coeff_a = (v_sq - 2.0f * cblas_sdot(3, u, 1, v, 1) + 1.0f) / (alpha * alpha);
    float coeff_b = -1.0f / alpha;

    float scale = 4.0f / (beta * sqrt_g);
    for (int i = 0; i < 3; i++) {
        grad[i] = scale * (coeff_a * u[i] + coeff_b * v[i]);
    }
}

// Project point back inside the Poincaré ball: ||x|| < 1 - eps.
static void project_to_ball(float* x) {
    float norm_sq = x[0]*x[0] + x[1]*x[1] + x[2]*x[2];
    float max_norm = 1.0f - HYP_EPS;
    if (norm_sq >= max_norm * max_norm) {
        float norm = std::sqrt(norm_sq);
        float s = max_norm / norm;
        x[0] *= s; x[1] *= s; x[2] *= s;
    }
}

bool InferenceEngine::load_hyp_cache() {
    std::string cache_path = model_path_ + ".vocab3d.hyp";
    std::ifstream f(cache_path, std::ios::binary | std::ios::ate);
    if (!f.is_open()) return false;

    auto file_size = f.tellg();
    f.seekg(0);

    if (!model_) return false;
    struct ggml_tensor* tok_embd = model_->tok_embd;
    if (!tok_embd) return false;
    int n_vocab = tok_embd->ne[1];

    size_t expected = static_cast<size_t>(n_vocab) * 3 * sizeof(float);
    if (static_cast<size_t>(file_size) != expected) {
        std::cout << "[llama-cpp] HYP cache size mismatch: " << file_size
                  << " bytes, expected " << expected << " for n_vocab=" << n_vocab << std::endl;
        return false;
    }

    vocab_positions_hyp_.resize(n_vocab * 3);
    f.read(reinterpret_cast<char*>(vocab_positions_hyp_.data()), expected);
    if (!f.good()) {
        vocab_positions_hyp_.clear();
        return false;
    }
    return true;
}

void InferenceEngine::write_hyp_cache() {
    std::string cache_path = model_path_ + ".vocab3d.hyp";
    std::ofstream f(cache_path, std::ios::binary | std::ios::trunc);
    if (!f.is_open()) {
        std::cout << "[llama-cpp] Failed to write HYP cache: " << cache_path << std::endl;
        return;
    }
    f.write(reinterpret_cast<const char*>(vocab_positions_hyp_.data()),
            vocab_positions_hyp_.size() * sizeof(float));
    std::cout << "[llama-cpp] Wrote HYP cache: " << cache_path
              << " (" << vocab_positions_hyp_.size() / 3 << " positions)" << std::endl;
}

void InferenceEngine::compute_hyperbolic_positions() {
    if (!model_) return;

    struct ggml_tensor* tok_embd = model_->tok_embd;
    if (!tok_embd) {
        std::cout << "[llama-cpp] tok_embd tensor not found for HYP" << std::endl;
        return;
    }

    int n_vocab = tok_embd->ne[1];
    int n_embd  = tok_embd->ne[0];
    std::cout << "[llama-cpp] Computing hyperbolic positions: " << n_vocab
              << " tokens × " << n_embd << " dims" << std::endl;

    // ── Step 0: Ensure PCA positions exist (we initialize from them) ──
    if (vocab_positions_.empty()) {
        if (!load_vocab_cache()) {
            compute_vocab_positions();
        }
    }
    if (vocab_positions_.empty() || (int)(vocab_positions_.size() / 3) != n_vocab) {
        std::cout << "[llama-cpp] PCA positions unavailable, cannot compute HYP" << std::endl;
        return;
    }

    // ── Step 1: Initialize Poincaré ball coords from PCA via exponential map ──
    vocab_positions_hyp_.resize(n_vocab * 3);
    for (int i = 0; i < n_vocab; i++) {
        float x = vocab_positions_[i*3+0];
        float y = vocab_positions_[i*3+1];
        float z = vocab_positions_[i*3+2];
        float norm = std::sqrt(x*x + y*y + z*z);
        if (norm < HYP_EPS) {
            // Near-zero PCA position → place near ball center
            vocab_positions_hyp_[i*3+0] = x * 0.001f;
            vocab_positions_hyp_[i*3+1] = y * 0.001f;
            vocab_positions_hyp_[i*3+2] = z * 0.001f;
        } else {
            // exp_0(v) = tanh(||v|| * scale / 2) * v/||v||
            float r = std::tanh(norm * HYP_INIT_SCALE * 0.5f);
            vocab_positions_hyp_[i*3+0] = r * x / norm;
            vocab_positions_hyp_[i*3+1] = r * y / norm;
            vocab_positions_hyp_[i*3+2] = r * z / norm;
        }
        project_to_ball(&vocab_positions_hyp_[i*3]);
    }
    std::cout << "[llama-cpp] HYP initialized from PCA via exponential map" << std::endl;

    // ── Step 2: Dequantize embeddings for k-NN graph ──
    size_t row_size = ggml_row_size(tok_embd->type, n_embd);
    std::vector<uint8_t> raw_row(row_size);
    std::vector<float> X(static_cast<size_t>(n_vocab) * n_embd);

    auto* traits = ggml_get_type_traits(tok_embd->type);
    for (int i = 0; i < n_vocab; i++) {
        float* dst = X.data() + static_cast<size_t>(i) * n_embd;
        ggml_backend_tensor_get(tok_embd, raw_row.data(), i * row_size, row_size);
        if (tok_embd->type == GGML_TYPE_F32) {
            memcpy(dst, raw_row.data(), n_embd * sizeof(float));
        } else {
            traits->to_float(raw_row.data(), dst, n_embd);
        }
    }

    // Normalize rows to unit length for cosine similarity via dot product
    for (int i = 0; i < n_vocab; i++) {
        float* row = X.data() + static_cast<size_t>(i) * n_embd;
        float norm = cblas_snrm2(n_embd, row, 1);
        if (norm > HYP_EPS) {
            cblas_sscal(n_embd, 1.0f / norm, row, 1);
        }
    }

    // ── Step 3: Build k-NN graph via batched cosine similarity ──
    const int batch_size = 512;
    int k = std::min(HYP_K, n_vocab - 1);
    std::vector<std::vector<int>> knn(n_vocab);

    std::vector<float> sim_block(static_cast<size_t>(batch_size) * n_vocab);
    std::vector<int> indices(n_vocab);

    auto knn_start = std::chrono::steady_clock::now();

    for (int b = 0; b < n_vocab; b += batch_size) {
        int rows = std::min(batch_size, n_vocab - b);

        // sim_block(rows × n_vocab) = X_batch(rows × n_embd) × X^T(n_embd × n_vocab)
        cblas_sgemm(CblasRowMajor, CblasNoTrans, CblasTrans,
                    rows, n_vocab, n_embd,
                    1.0f, X.data() + static_cast<size_t>(b) * n_embd, n_embd,
                    X.data(), n_embd,
                    0.0f, sim_block.data(), n_vocab);

        for (int r = 0; r < rows; r++) {
            int token_id = b + r;
            float* sims = sim_block.data() + static_cast<size_t>(r) * n_vocab;

            // Zero self-similarity to exclude self from neighbors
            sims[token_id] = -1.0f;

            // Partial sort for top-k by similarity
            for (int i = 0; i < n_vocab; i++) indices[i] = i;
            std::partial_sort(indices.begin(), indices.begin() + k, indices.end(),
                              [sims](int a, int b) { return sims[a] > sims[b]; });

            knn[token_id].resize(k);
            for (int i = 0; i < k; i++) {
                knn[token_id][i] = indices[i];
            }
        }
    }

    auto knn_end = std::chrono::steady_clock::now();
    auto knn_ms = std::chrono::duration_cast<std::chrono::milliseconds>(knn_end - knn_start).count();
    std::cout << "[llama-cpp] HYP k-NN graph built: " << n_vocab << " tokens × "
              << k << " neighbors in " << knn_ms << "ms" << std::endl;

    // Free the embedding matrix — no longer needed
    X.clear();
    X.shrink_to_fit();

    // ── Step 4: Riemannian SGD with negative sampling ──
    //
    // Loss per edge (u, v): -log( exp(-d(u,v)) / Σ exp(-d(u,v')) )
    // where v' includes v and negative samples.
    // Gradient: Euclidean grad scaled by (1 - ||θ||²)² / 4 (inverse metric tensor).

    std::mt19937 rng(42);
    std::uniform_int_distribution<int> neg_dist(0, n_vocab - 1);
    float grad_u[3], grad_neg[3];

    auto sgd_start = std::chrono::steady_clock::now();

    for (int epoch = 0; epoch < HYP_EPOCHS; epoch++) {
        float lr = (epoch < HYP_BURNIN) ? HYP_BURNIN_LR : HYP_LR;
        float total_loss = 0;

        for (int u = 0; u < n_vocab; u++) {
            float* u_pos = &vocab_positions_hyp_[u * 3];

            for (int ni = 0; ni < k; ni++) {
                int v = knn[u][ni];
                float* v_pos = &vocab_positions_hyp_[v * 3];

                float d_pos = poincare_dist(u_pos, v_pos);

                // Compute exp(-d) for positive and negative samples
                float exp_pos = std::exp(-d_pos);
                float exp_sum = exp_pos;

                // Accumulate negative sample gradients
                float neg_grad_acc[3] = {0, 0, 0};

                for (int s = 0; s < HYP_NEG_SAMPLES; s++) {
                    int neg = neg_dist(rng);
                    if (neg == u || neg == v) { s--; continue; }
                    float* neg_pos = &vocab_positions_hyp_[neg * 3];
                    float d_neg = poincare_dist(u_pos, neg_pos);
                    float exp_neg = std::exp(-d_neg);
                    exp_sum += exp_neg;

                    // Gradient contribution from negative: push apart
                    float weight = exp_neg / (exp_sum + HYP_EPS);
                    poincare_grad(u_pos, neg_pos, grad_neg);
                    for (int i = 0; i < 3; i++) {
                        neg_grad_acc[i] += weight * grad_neg[i];
                    }
                }

                total_loss += -std::log(exp_pos / (exp_sum + HYP_EPS) + HYP_EPS);

                // Gradient: pull toward positive, push from negatives
                poincare_grad(u_pos, v_pos, grad_u);
                float weight_pos = 1.0f - exp_pos / (exp_sum + HYP_EPS);

                // Riemannian scaling: (1 - ||u||²)² / 4
                float u_sq = u_pos[0]*u_pos[0] + u_pos[1]*u_pos[1] + u_pos[2]*u_pos[2];
                float riem = (1.0f - u_sq) * (1.0f - u_sq) / 4.0f;

                for (int i = 0; i < 3; i++) {
                    u_pos[i] -= lr * riem * (weight_pos * grad_u[i] - neg_grad_acc[i]);
                }
                project_to_ball(u_pos);
            }
        }

        if (epoch % 50 == 0 || epoch == HYP_EPOCHS - 1) {
            std::cout << "[llama-cpp] HYP epoch " << epoch << "/" << HYP_EPOCHS
                      << " loss=" << total_loss / (n_vocab * k) << std::endl;
        }
    }

    auto sgd_end = std::chrono::steady_clock::now();
    auto sgd_ms = std::chrono::duration_cast<std::chrono::milliseconds>(sgd_end - sgd_start).count();
    std::cout << "[llama-cpp] HYP Riemannian SGD complete: " << HYP_EPOCHS
              << " epochs in " << sgd_ms << "ms" << std::endl;

    write_hyp_cache();
}

const std::vector<float>& InferenceEngine::vocab_positions_hyp() {
    if (vocab_positions_hyp_.empty() && model_) {
        if (load_hyp_cache()) {
            std::cout << "[llama-cpp] Loaded HYP positions from cache ("
                      << vocab_positions_hyp_.size() / 3 << " positions)" << std::endl;
        } else {
            compute_hyperbolic_positions();
        }
    }
    return vocab_positions_hyp_;
}

const std::vector<float>& InferenceEngine::active_positions() {
    if (projection_mode_ == ProjectionMode::HYP) {
        return vocab_positions_hyp();
    }
    return vocab_positions_3d();
}

void InferenceEngine::set_projection_mode(ProjectionMode mode) {
    projection_mode_ = mode;
}
