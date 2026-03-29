#include "plugin.h"

#include <algorithm>
#include <cmath>
#include <iostream>
#include <sstream>
#include <vector>

#include "llama.h"

static constexpr int SIGNAL_TOP_K = 10;
static constexpr int STAGE_TOP_K = 5;  // top-k candidates stored per stage snapshot

// --- Observer sampler ---
// A no-op sampler that snapshots the token distribution as it passes through.
// Insert between real samplers to see what each stage does to the distribution.

struct ObserverCtx {
    std::string stage_name;                      // name of the preceding sampler stage
    std::vector<SamplerStageSnapshot>* snapshots; // output: append here
    const llama_vocab* vocab;                    // for token_to_piece
};

static const char* observer_name(const struct llama_sampler* smpl) {
    auto* ctx = static_cast<ObserverCtx*>(smpl->ctx);
    return ctx->stage_name.c_str();
}

static void observer_apply(struct llama_sampler* smpl, llama_token_data_array* cur_p) {
    auto* ctx = static_cast<ObserverCtx*>(smpl->ctx);

    // Count active tokens (nonzero probability or unsorted logits)
    // After softmax-based samplers, zeroed-out tokens have p=0.
    // Before softmax, all tokens have logits — use cur_p->size as active count.
    int active = 0;
    float top1_prob = 0.0f;

    // Normalize if not already (samplers may leave raw logits)
    // We compute softmax on a copy to avoid mutating the chain
    std::vector<float> probs(cur_p->size);
    float max_val = -1e30f;
    for (size_t i = 0; i < cur_p->size; i++) {
        if (cur_p->data[i].logit > max_val) max_val = cur_p->data[i].logit;
    }
    float sum = 0.0f;
    for (size_t i = 0; i < cur_p->size; i++) {
        probs[i] = std::exp(cur_p->data[i].logit - max_val);
        sum += probs[i];
    }
    if (sum > 0.0f) {
        for (size_t i = 0; i < cur_p->size; i++) probs[i] /= sum;
    }

    for (size_t i = 0; i < cur_p->size; i++) {
        if (probs[i] > 1e-10f) active++;
        if (probs[i] > top1_prob) top1_prob = probs[i];
    }

    // Shannon entropy
    float h = 0.0f;
    for (size_t i = 0; i < cur_p->size; i++) {
        if (probs[i] > 1e-10f) {
            h -= probs[i] * std::log2(probs[i]);
        }
    }

    // Top-k candidates
    std::vector<size_t> indices(cur_p->size);
    for (size_t i = 0; i < cur_p->size; i++) indices[i] = i;
    int k = std::min(STAGE_TOP_K, (int)cur_p->size);
    std::partial_sort(indices.begin(), indices.begin() + k, indices.end(),
                      [&probs](size_t a, size_t b) { return probs[a] > probs[b]; });

    SamplerStageSnapshot snap;
    snap.stage_name = ctx->stage_name;
    snap.active_count = active;
    snap.top1_prob = top1_prob;
    snap.entropy = h;
    snap.top_k.resize(k);
    for (int i = 0; i < k; i++) {
        size_t idx = indices[i];
        int id = cur_p->data[idx].id;
        char buf[256];
        int len = llama_token_to_piece(ctx->vocab, id, buf, sizeof(buf), 0, true);
        snap.top_k[i] = {id, std::string(buf, std::max(0, len)), probs[idx]};
    }

    ctx->snapshots->push_back(std::move(snap));
}

static void observer_free(struct llama_sampler* smpl) {
    delete static_cast<ObserverCtx*>(smpl->ctx);
}

static llama_sampler_i observer_iface = {
    /* .name   = */ observer_name,
    /* .accept = */ nullptr,
    /* .apply  = */ observer_apply,
    /* .reset  = */ nullptr,
    /* .clone  = */ nullptr,
    /* .free   = */ observer_free,
};

// Create an observer sampler that records the distribution state after `stage_name`.
static llama_sampler* make_observer(const std::string& stage_name,
                                     std::vector<SamplerStageSnapshot>* snapshots,
                                     const llama_vocab* vocab) {
    auto* ctx = new ObserverCtx{stage_name, snapshots, vocab};
    return llama_sampler_init(&observer_iface, ctx);
}

// Build the full sampler chain with observers between each stage.
// Returns the chain sampler. Caller must llama_sampler_free() it.
// `snapshots` is cleared and will be populated per-token (clear before each sample call).
static llama_sampler* build_sampler_chain(
    float temperature,
    const SamplerConfig& cfg,
    const llama_vocab* vocab,
    std::vector<SamplerStageSnapshot>* snapshots) {

    auto chain = llama_sampler_chain_init(llama_sampler_chain_default_params());

    // Observer: raw logits (before any sampling)
    llama_sampler_chain_add(chain, make_observer("logits", snapshots, vocab));

    // Penalties (must come before top-k/top-p per llama.cpp docs)
    if (cfg.penalty_last_n != 0) {
        llama_sampler_chain_add(chain,
            llama_sampler_init_penalties(cfg.penalty_last_n,
                                         cfg.penalty_repeat,
                                         cfg.penalty_freq,
                                         cfg.penalty_present));
        llama_sampler_chain_add(chain, make_observer("penalties", snapshots, vocab));
    }

    // Top-K
    if (cfg.top_k > 0) {
        llama_sampler_chain_add(chain, llama_sampler_init_top_k(cfg.top_k));
        llama_sampler_chain_add(chain, make_observer("top_k", snapshots, vocab));
    }

    // Top-P (nucleus)
    if (cfg.top_p < 1.0f) {
        llama_sampler_chain_add(chain, llama_sampler_init_top_p(cfg.top_p, 1));
        llama_sampler_chain_add(chain, make_observer("top_p", snapshots, vocab));
    }

    // Min-P
    if (cfg.min_p > 0.0f) {
        llama_sampler_chain_add(chain, llama_sampler_init_min_p(cfg.min_p, 1));
        llama_sampler_chain_add(chain, make_observer("min_p", snapshots, vocab));
    }

    // Typical
    if (cfg.typical_p < 1.0f) {
        llama_sampler_chain_add(chain, llama_sampler_init_typical(cfg.typical_p, 1));
        llama_sampler_chain_add(chain, make_observer("typical", snapshots, vocab));
    }

    // Temperature
    llama_sampler_chain_add(chain, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(chain, make_observer("temp", snapshots, vocab));

    // Final: categorical distribution sampling
    llama_sampler_chain_add(chain, llama_sampler_init_dist(0));

    return chain;
}

// Softmax in-place over n floats, returns max for numerical stability
static void softmax(float* data, int n) {
    float max_val = *std::max_element(data, data + n);
    float sum = 0.0f;
    for (int i = 0; i < n; i++) {
        data[i] = std::exp(data[i] - max_val);
        sum += data[i];
    }
    for (int i = 0; i < n; i++) {
        data[i] /= sum;
    }
}

// Capture pre-sampler signal from raw logits at the current position.
//
// Zero-cost signals not yet extracted (data exists in this window):
// TODO(TMD): Token metadata — llama_token_get_score(vocab, id) and
//   llama_token_get_attr(vocab, id) per top-k candidate. O(1) lookups.
// TODO(CWU): Context window usage — llama_get_seq_pos(ctx, -1) / n_ctx.
//   Three integers per token, zero compute.
// TODO(TMP): Temperature sensitivity — softmax at 5 temperatures to show
//   how much temperature reshapes the distribution. ~0.5ms for 5 extra passes.
// TODO(CPX): Cumulative perplexity — exp(mean(-log(confidence))) across all
//   tokens so far. Running scalar, negligible compute.
//
// Moderate-cost signals (Tier 2):
// TODO(HSE): Hidden state embedding — llama_get_embeddings(ctx) returns 4096
//   floats, pointer dereference. Semantic trajectory through token space.
//
// Sampler visibility: implemented via observer sampler (see build_sampler_chain above).
// Observer snapshots distribution between each stage — data flows through TokenSignal.sampler_stages.
static TokenSignal capture_signal(llama_context* ctx, const llama_vocab* vocab, int top_k) {
    int n_vocab = llama_vocab_n_tokens(vocab);
    float* logits = llama_get_logits_ith(ctx, -1);

    // Copy logits — we need to softmax without mutating the originals
    // that the sampler will read
    std::vector<float> probs(logits, logits + n_vocab);
    softmax(probs.data(), n_vocab);

    // Build index array, partial sort for top-k
    std::vector<int> indices(n_vocab);
    for (int i = 0; i < n_vocab; i++) indices[i] = i;
    int k = std::min(top_k, n_vocab);
    std::partial_sort(indices.begin(), indices.begin() + k, indices.end(),
                      [&probs](int a, int b) { return probs[a] > probs[b]; });

    TokenSignal sig;
    sig.confidence = probs[indices[0]];
    sig.top_gap = (k >= 2) ? probs[indices[0]] - probs[indices[1]] : sig.confidence;

    // Shannon entropy over top-k
    float h = 0.0f;
    for (int i = 0; i < k; i++) {
        float p = probs[indices[i]];
        if (p > 0.0f) {
            h -= p * std::log2(p);
        }
    }
    sig.entropy = h;

    // Top-k candidates
    sig.top_k.resize(k);
    for (int i = 0; i < k; i++) {
        int id = indices[i];
        char buf[256];
        int len = llama_token_to_piece(vocab, id, buf, sizeof(buf), 0, true);
        sig.top_k[i] = {id, std::string(buf, std::max(0, len)), probs[id]};
    }

    // Keep the full softmax distribution for visualization
    sig.full_distribution = std::move(probs);

    return sig;
}

InferenceEngine::InferenceEngine() {}

InferenceEngine::~InferenceEngine() {
    unload();
    if (backend_initialized_) {
        llama_backend_free();
    }
}

bool InferenceEngine::load_model(const std::string& model_path, int n_ctx) {
    std::lock_guard<std::mutex> lock(mutex_);

    unload();

    // Initialize backend on first model load
    if (!backend_initialized_) {
        llama_backend_init();
        backend_initialized_ = true;
    }

    // Model parameters
    auto model_params = llama_model_default_params();
    model_ = llama_model_load_from_file(model_path.c_str(), model_params);
    if (!model_) {
        std::cout << "[llama-cpp] Failed to load model from " << model_path << std::endl;
        return false;
    }

    // Context parameters
    auto ctx_params = llama_context_default_params();
    ctx_params.n_ctx = n_ctx;
    ctx_ = llama_init_from_model(model_, ctx_params);
    if (!ctx_) {
        std::cout << "[llama-cpp] Failed to create context for " << model_path << std::endl;
        llama_model_free(model_);
        model_ = nullptr;
        return false;
    }

    model_path_ = model_path;

    // Read model name from GGUF metadata
    char name_buf[256];
    int n = llama_model_meta_val_str(model_, "general.name", name_buf, sizeof(name_buf));
    if (n > 0) {
        model_name_ = std::string(name_buf, n);
    } else {
        // Fallback: derive from filename
        auto pos = model_path.find_last_of('/');
        model_name_ = (pos != std::string::npos) ? model_path.substr(pos + 1) : model_path;
        auto dot = model_name_.find_last_of('.');
        if (dot != std::string::npos) {
            model_name_ = model_name_.substr(0, dot);
        }
    }

    std::cout << "[llama-cpp] Model loaded: " << model_name_
              << " (ctx=" << n_ctx << ")" << std::endl;
    return true;
}

void InferenceEngine::unload() {
    if (ctx_) {
        llama_free(ctx_);
        ctx_ = nullptr;
    }
    if (model_) {
        llama_model_free(model_);
        model_ = nullptr;
    }
}

bool InferenceEngine::is_loaded() const {
    return model_ != nullptr && ctx_ != nullptr;
}

// Prepare prompt: build chat template, tokenize, decode prompt into KV cache.
// Returns prompt token count, or -1 on error (with result.content set).
int InferenceEngine::prepare_prompt(
    const std::vector<Message>& messages,
    ChatResult& result) {

    // Build llama_chat_message array from our Message structs
    std::vector<llama_chat_message> chat_msgs;
    chat_msgs.reserve(messages.size());
    for (const auto& m : messages) {
        chat_msgs.push_back({m.role.c_str(), m.content.c_str()});
    }

    // Apply the model's own chat template
    const char* tmpl = llama_model_chat_template(model_, nullptr);
    size_t total_content = 0;
    for (const auto& m : messages) total_content += m.content.size();
    int alloc = 2 * total_content + 256;
    std::vector<char> buf(alloc);
    int n_written = llama_chat_apply_template(tmpl, chat_msgs.data(), chat_msgs.size(), true, buf.data(), buf.size());
    if (n_written > (int)buf.size()) {
        buf.resize(n_written + 1);
        n_written = llama_chat_apply_template(tmpl, chat_msgs.data(), chat_msgs.size(), true, buf.data(), buf.size());
    }
    if (n_written < 0) {
        result.content = "error: chat template failed";
        return -1;
    }
    std::string prompt(buf.data(), n_written);

    // Tokenize
    const llama_vocab* vocab = llama_model_get_vocab(model_);
    int n_prompt_max = prompt.size() + 32;
    std::vector<llama_token> tokens(n_prompt_max);
    int n_tokens = llama_tokenize(vocab, prompt.c_str(), prompt.size(),
                                   tokens.data(), n_prompt_max, true, true);
    if (n_tokens < 0) {
        result.content = "error: tokenization failed";
        return -1;
    }
    tokens.resize(n_tokens);

    // Clear KV cache
    llama_memory_clear(llama_get_memory(ctx_), true);

    // Decode prompt
    llama_batch batch = llama_batch_get_one(tokens.data(), n_tokens);
    if (llama_decode(ctx_, batch) != 0) {
        result.content = "error: prompt decode failed";
        return -1;
    }

    return n_tokens;
}

InferenceEngine::ChatResult InferenceEngine::chat(
    const std::string& system_prompt,
    const std::string& user_prompt,
    float temperature,
    int max_tokens,
    const SamplerConfig& sampler_cfg) {

    std::vector<Message> messages;
    if (!system_prompt.empty()) {
        messages.push_back({"system", system_prompt});
    }
    messages.push_back({"user", user_prompt});
    return chat(messages, temperature, max_tokens, sampler_cfg);
}

InferenceEngine::ChatResult InferenceEngine::chat(
    const std::vector<Message>& messages,
    float temperature,
    int max_tokens,
    const SamplerConfig& sampler_cfg) {

    std::lock_guard<std::mutex> lock(mutex_);

    ChatResult result;
    if (!model_ || !ctx_) {
        result.content = "error: no model loaded";
        return result;
    }

    int n_tokens = prepare_prompt(messages, result);
    if (n_tokens < 0) return result;
    result.prompt_tokens = n_tokens;

    const llama_vocab* vocab = llama_model_get_vocab(model_);
    std::vector<SamplerStageSnapshot> stage_snapshots;
    auto sampler = build_sampler_chain(temperature, sampler_cfg, vocab, &stage_snapshots);

    std::ostringstream output;
    int n_generated = 0;

    for (int i = 0; i < max_tokens; i++) {
        TokenSignal sig = capture_signal(ctx_, vocab, SIGNAL_TOP_K);
        stage_snapshots.clear();
        llama_token new_token = llama_sampler_sample(sampler, ctx_, -1);

        if (llama_vocab_is_eog(vocab, new_token)) break;

        char buf[256];
        int n = llama_token_to_piece(vocab, new_token, buf, sizeof(buf), 0, true);
        sig.token_id = new_token;
        if (n > 0) {
            sig.token_text = std::string(buf, n);
            output.write(buf, n);
        }
        sig.sampler_stages = stage_snapshots;
        result.signals.push_back(std::move(sig));

        llama_batch next = llama_batch_get_one(&new_token, 1);
        if (llama_decode(ctx_, next) != 0) break;
        n_generated++;
    }

    llama_sampler_free(sampler);
    result.content = output.str();
    result.completion_tokens = n_generated;
    return result;
}

InferenceEngine::ChatResult InferenceEngine::stream_chat(
    const std::string& system_prompt,
    const std::string& user_prompt,
    float temperature,
    int max_tokens,
    TokenCallback on_token,
    const SamplerConfig& sampler_cfg) {

    std::vector<Message> messages;
    if (!system_prompt.empty()) {
        messages.push_back({"system", system_prompt});
    }
    messages.push_back({"user", user_prompt});
    return stream_chat(messages, temperature, max_tokens, on_token, sampler_cfg);
}

InferenceEngine::ChatResult InferenceEngine::stream_chat(
    const std::vector<Message>& messages,
    float temperature,
    int max_tokens,
    TokenCallback on_token,
    const SamplerConfig& sampler_cfg) {

    std::lock_guard<std::mutex> lock(mutex_);

    ChatResult result;
    if (!model_ || !ctx_) {
        result.content = "error: no model loaded";
        return result;
    }

    int n_tokens = prepare_prompt(messages, result);
    if (n_tokens < 0) return result;
    result.prompt_tokens = n_tokens;

    const llama_vocab* vocab = llama_model_get_vocab(model_);
    std::vector<SamplerStageSnapshot> stage_snapshots;
    auto sampler = build_sampler_chain(temperature, sampler_cfg, vocab, &stage_snapshots);

    std::ostringstream output;
    int n_generated = 0;

    for (int i = 0; i < max_tokens; i++) {
        TokenSignal sig = capture_signal(ctx_, vocab, SIGNAL_TOP_K);
        stage_snapshots.clear();
        llama_token new_token = llama_sampler_sample(sampler, ctx_, -1);

        if (llama_vocab_is_eog(vocab, new_token)) break;

        char buf[256];
        int n = llama_token_to_piece(vocab, new_token, buf, sizeof(buf), 0, true);
        sig.token_id = new_token;
        if (n > 0) {
            sig.token_text = std::string(buf, n);
            output.write(buf, n);
        }
        sig.sampler_stages = stage_snapshots;
        result.signals.push_back(sig);

        // Stream the token to the caller
        if (on_token && !on_token(sig.token_text, sig)) {
            break; // Caller requested abort
        }

        llama_batch next = llama_batch_get_one(&new_token, 1);
        if (llama_decode(ctx_, next) != 0) break;
        n_generated++;
    }

    llama_sampler_free(sampler);
    result.content = output.str();
    result.completion_tokens = n_generated;
    return result;
}
