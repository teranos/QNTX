#include "plugin.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <iostream>
#include <sstream>
#include <vector>

#include "llama.h"

static constexpr int SIGNAL_TOP_K = 10;

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
// Sampler visibility (requires observer sampler, not a capture_signal change):
// TODO(SCO): Sampler chain observations — custom llama_sampler_i between each
//   sampler stage to snapshot distribution before/after. See inference-internals.md.
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

    // Model parameters — offload all layers to Metal GPU
    auto model_params = llama_model_default_params();
    model_params.n_gpu_layers = -1;
    model_params.progress_callback = [](float progress, void*) -> bool {
        int pct = (int)(progress * 100);
        if (pct % 10 == 0) {
            std::cout << "[llama-cpp] Loading model: " << pct << "%" << std::endl;
        }
        return true;
    };
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
    auto t0 = std::chrono::steady_clock::now();
    llama_batch batch = llama_batch_get_one(tokens.data(), n_tokens);
    if (llama_decode(ctx_, batch) != 0) {
        result.content = "error: prompt decode failed";
        return -1;
    }
    auto t1 = std::chrono::steady_clock::now();
    auto prompt_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t1 - t0).count();
    std::cout << "[llama-cpp] Prompt eval: " << n_tokens << " tokens in "
              << prompt_ms << "ms (" << (prompt_ms > 0 ? (n_tokens * 1000 / prompt_ms) : 0)
              << " tok/s)" << std::endl;

    return n_tokens;
}

InferenceEngine::ChatResult InferenceEngine::chat(
    const std::string& system_prompt,
    const std::string& user_prompt,
    float temperature,
    int max_tokens) {

    std::vector<Message> messages;
    if (!system_prompt.empty()) {
        messages.push_back({"system", system_prompt});
    }
    messages.push_back({"user", user_prompt});
    return chat(messages, temperature, max_tokens);
}

InferenceEngine::ChatResult InferenceEngine::chat(
    const std::vector<Message>& messages,
    float temperature,
    int max_tokens) {

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
    auto sampler = llama_sampler_chain_init(llama_sampler_chain_default_params());
    llama_sampler_chain_add(sampler, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(sampler, llama_sampler_init_dist(0));

    std::ostringstream output;
    int n_generated = 0;

    for (int i = 0; i < max_tokens; i++) {
        TokenSignal sig = capture_signal(ctx_, vocab, SIGNAL_TOP_K);
        llama_token new_token = llama_sampler_sample(sampler, ctx_, -1);

        if (llama_vocab_is_eog(vocab, new_token)) break;

        char buf[256];
        int n = llama_token_to_piece(vocab, new_token, buf, sizeof(buf), 0, true);
        sig.token_id = new_token;
        if (n > 0) {
            sig.token_text = std::string(buf, n);
            output.write(buf, n);
        }
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
    TokenCallback on_token) {

    std::vector<Message> messages;
    if (!system_prompt.empty()) {
        messages.push_back({"system", system_prompt});
    }
    messages.push_back({"user", user_prompt});
    return stream_chat(messages, temperature, max_tokens, on_token);
}

InferenceEngine::ChatResult InferenceEngine::stream_chat(
    const std::vector<Message>& messages,
    float temperature,
    int max_tokens,
    TokenCallback on_token) {

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
    // TODO(SCO): Insert observer samplers between stages to capture distribution
    //   before/after each sampler. Currently the chain is a black box.
    // TODO(SUI): Accept top_k, top_p, min_p, repetition_penalty from request.
    //   Only temperature is wired. See inference-internals.md checklist.
    auto sampler = llama_sampler_chain_init(llama_sampler_chain_default_params());
    llama_sampler_chain_add(sampler, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(sampler, llama_sampler_init_dist(0));

    std::ostringstream output;
    int n_generated = 0;
    auto gen_start = std::chrono::steady_clock::now();
    long signal_us = 0, decode_us = 0, callback_us = 0;

    for (int i = 0; i < max_tokens; i++) {
        auto t0 = std::chrono::steady_clock::now();
        TokenSignal sig = capture_signal(ctx_, vocab, SIGNAL_TOP_K);
        auto t1 = std::chrono::steady_clock::now();
        signal_us += std::chrono::duration_cast<std::chrono::microseconds>(t1 - t0).count();

        llama_token new_token = llama_sampler_sample(sampler, ctx_, -1);

        if (llama_vocab_is_eog(vocab, new_token)) break;

        char buf[256];
        int n = llama_token_to_piece(vocab, new_token, buf, sizeof(buf), 0, true);
        sig.token_id = new_token;
        if (n > 0) {
            sig.token_text = std::string(buf, n);
            output.write(buf, n);
        }
        result.signals.push_back(sig);

        // Stream the token to the caller
        auto t2 = std::chrono::steady_clock::now();
        if (on_token && !on_token(sig.token_text, sig)) {
            break; // Caller requested abort
        }
        auto t3 = std::chrono::steady_clock::now();
        callback_us += std::chrono::duration_cast<std::chrono::microseconds>(t3 - t2).count();

        auto t4 = std::chrono::steady_clock::now();
        llama_batch next = llama_batch_get_one(&new_token, 1);
        if (llama_decode(ctx_, next) != 0) break;
        auto t5 = std::chrono::steady_clock::now();
        decode_us += std::chrono::duration_cast<std::chrono::microseconds>(t5 - t4).count();
        n_generated++;
    }

    auto gen_end = std::chrono::steady_clock::now();
    auto total_ms = std::chrono::duration_cast<std::chrono::milliseconds>(gen_end - gen_start).count();
    std::cout << "[llama-cpp] Generation: " << n_generated << " tokens in "
              << total_ms << "ms (" << (total_ms > 0 ? (n_generated * 1000 / total_ms) : 0) << " tok/s)"
              << " | decode=" << decode_us / 1000 << "ms"
              << " signal=" << signal_us / 1000 << "ms"
              << " callback=" << callback_us / 1000 << "ms" << std::endl;

    llama_sampler_free(sampler);
    result.content = output.str();
    result.completion_tokens = n_generated;
    return result;
}
