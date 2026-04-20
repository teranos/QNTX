#include "inference_internal.h"

#include <chrono>
#include <iostream>
#include <sstream>
#include <vector>

// Build a sampler chain without observer instrumentation.
llama_sampler* build_sampler_chain(float temperature, const SamplerConfig& cfg) {
    auto chain = llama_sampler_chain_init(llama_sampler_chain_default_params());

    // Penalties (must come before top-k/top-p per llama.cpp docs)
    if (cfg.penalty_last_n != 0) {
        llama_sampler_chain_add(chain,
            llama_sampler_init_penalties(cfg.penalty_last_n,
                                         cfg.penalty_repeat,
                                         cfg.penalty_freq,
                                         cfg.penalty_present));
    }

    if (cfg.top_k > 0) {
        llama_sampler_chain_add(chain, llama_sampler_init_top_k(cfg.top_k));
    }

    if (cfg.top_p < 1.0f) {
        llama_sampler_chain_add(chain, llama_sampler_init_top_p(cfg.top_p, 1));
    }

    if (cfg.min_p > 0.0f) {
        llama_sampler_chain_add(chain, llama_sampler_init_min_p(cfg.min_p, 1));
    }

    if (cfg.typical_p < 1.0f) {
        llama_sampler_chain_add(chain, llama_sampler_init_typical(cfg.typical_p, 1));
    }

    llama_sampler_chain_add(chain, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(chain, llama_sampler_init_dist(0));

    return chain;
}

static bool g_backend_initialized = false;

static void ensure_backend() {
    if (!g_backend_initialized) {
        llama_backend_init();
        g_backend_initialized = true;
    }
}

InferenceEngine::InferenceEngine() {}

InferenceEngine::~InferenceEngine() {
    unload();
}

bool InferenceEngine::load_model(const std::string& model_path, int n_ctx) {
    std::lock_guard<std::mutex> lock(mutex_);

    unload();
    ensure_backend();

    auto model_params = llama_model_default_params();
    model_params.n_gpu_layers = -1;
    model_params.progress_callback = [](float, void*) -> bool { return true; };
    model_ = llama_model_load_from_file(model_path.c_str(), model_params);
    if (!model_) {
        std::cout << "[model] Failed to load model from " << model_path << std::endl;
        return false;
    }

    // Use model's native context length, capped by config n_ctx
    int model_ctx = llama_model_n_ctx_train(model_);
    int effective_ctx = (model_ctx > 0 && model_ctx < n_ctx) ? model_ctx : n_ctx;

    auto ctx_params = llama_context_default_params();
    ctx_params.n_ctx = effective_ctx;
    ctx_params.n_batch = effective_ctx;
    ctx_ = llama_init_from_model(model_, ctx_params);
    if (!ctx_) {
        std::cout << "[model] Failed to create context for " << model_path << std::endl;
        llama_model_free(model_);
        model_ = nullptr;
        return false;
    }

    model_path_ = model_path;
    effective_ctx_ = effective_ctx;

    char name_buf[256];
    int n = llama_model_meta_val_str(model_, "general.name", name_buf, sizeof(name_buf));
    if (n > 0) {
        model_name_ = std::string(name_buf, n);
    } else {
        auto pos = model_path.find_last_of('/');
        model_name_ = (pos != std::string::npos) ? model_path.substr(pos + 1) : model_path;
        auto dot = model_name_.find_last_of('.');
        if (dot != std::string::npos) {
            model_name_ = model_name_.substr(0, dot);
        }
    }

    // Model details are reported via Health RPC details map

    init_vision(model_path);

    return true;
}

void InferenceEngine::unload() {
    cleanup_vision();
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

int InferenceEngine::vocab_size() const {
    if (!model_) return 0;
    const auto* vocab = llama_model_get_vocab(model_);
    return vocab ? llama_vocab_n_tokens(vocab) : 0;
}

std::string InferenceEngine::token_text(int token_id) const {
    if (!model_ || token_id < 0) return "";
    const auto* vocab = llama_model_get_vocab(model_);
    if (!vocab) return "";
    char buf[256];
    int len = llama_token_to_piece(vocab, token_id, buf, sizeof(buf), 0, true);
    if (len < 0) return "";
    return std::string(buf, len);
}

std::string InferenceEngine::model_desc() const {
    if (!model_) return "";
    char buf[256];
    int n = llama_model_desc(model_, buf, sizeof(buf));
    if (n > 0) return std::string(buf, n);
    return "";
}

uint64_t InferenceEngine::model_size_bytes() const {
    if (!model_) return 0;
    return llama_model_size(model_);
}

uint64_t InferenceEngine::model_n_params() const {
    if (!model_) return 0;
    return llama_model_n_params(model_);
}

int InferenceEngine::context_length() const {
    return effective_ctx_;
}

int InferenceEngine::prepare_prompt(
    const std::vector<Message>& messages,
    ChatResult& result) {

    const llama_vocab* vocab = llama_model_get_vocab(model_);
    const char* tmpl = llama_model_chat_template(model_, nullptr);
    int ctx_size = llama_n_ctx(ctx_);
    int max_prompt = ctx_size * 3 / 4;

    auto template_and_tokenize = [&](const std::vector<Message>& msgs,
                                      std::vector<llama_token>& out) -> int {
        std::vector<llama_chat_message> chat_msgs;
        chat_msgs.reserve(msgs.size());
        for (const auto& m : msgs) {
            chat_msgs.push_back({m.role.c_str(), m.content.c_str()});
        }
        size_t total = 0;
        for (const auto& m : msgs) total += m.content.size();
        int alloc = 2 * total + 256;
        std::vector<char> buf(alloc);
        int n = llama_chat_apply_template(tmpl, chat_msgs.data(), chat_msgs.size(),
                                          true, buf.data(), buf.size());
        if (n > (int)buf.size()) {
            buf.resize(n + 1);
            n = llama_chat_apply_template(tmpl, chat_msgs.data(), chat_msgs.size(),
                                          true, buf.data(), buf.size());
        }
        if (n < 0) return -1;
        std::string prompt(buf.data(), n);
        int tok_max = prompt.size() + 32;
        out.resize(tok_max);
        int nt = llama_tokenize(vocab, prompt.c_str(), prompt.size(),
                                out.data(), tok_max, true, true);
        if (nt < 0) return -1;
        out.resize(nt);
        return nt;
    };

    std::vector<llama_token> tokens;
    int n_tokens = template_and_tokenize(messages, tokens);
    if (n_tokens < 0) {
        result.content = "error: tokenization failed";
        return -1;
    }

    if (n_tokens > max_prompt) {
        int original = n_tokens;

        std::vector<Message> reduced;
        for (const auto& m : messages) {
            if (m.role == "system") { reduced.push_back(m); break; }
        }
        int last_user = -1;
        for (int i = messages.size() - 1; i >= 0; i--) {
            if (messages[i].role == "user") { last_user = i; break; }
        }
        if (last_user < 0) {
            result.content = "error: no user message found";
            return -1;
        }
        reduced.push_back(messages[last_user]);

        n_tokens = template_and_tokenize(reduced, tokens);
        if (n_tokens < 0) {
            result.content = "error: tokenization failed";
            return -1;
        }

        if (n_tokens > max_prompt) {
            int excess = n_tokens - max_prompt;
            int chars_to_cut = excess * 5;
            auto& content = reduced.back().content;
            if (chars_to_cut < (int)content.size()) {
                content = "… " + content.substr(chars_to_cut);
            } else {
                int keep = (int)content.size() / 2;
                content = "… " + content.substr(content.size() - keep);
            }
            n_tokens = template_and_tokenize(reduced, tokens);
            if (n_tokens < 0) {
                result.content = "error: tokenization failed after truncation";
                return -1;
            }
            if (n_tokens > max_prompt) {
                n_tokens = max_prompt;
                tokens.resize(n_tokens);
            }
        }

        result.warning = "Prompt truncated from " + std::to_string(original)
            + " to " + std::to_string(n_tokens) + " tokens (context window: "
            + std::to_string(ctx_size) + ")";
        std::cerr << "[model] WARNING: " << result.warning << std::endl;
    }

    llama_memory_clear(llama_get_memory(ctx_), true);

    auto t0 = std::chrono::steady_clock::now();
    llama_batch batch = llama_batch_get_one(tokens.data(), n_tokens);
    if (llama_decode(ctx_, batch) != 0) {
        result.content = "error: prompt decode failed";
        return -1;
    }
    auto t1 = std::chrono::steady_clock::now();
    result.prompt_eval_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t1 - t0).count();
    return n_tokens;
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
    auto sampler = build_sampler_chain(temperature, sampler_cfg);

    std::ostringstream output;
    int n_generated = 0;
    auto gen_start = std::chrono::steady_clock::now();
    long decode_us = 0, callback_us = 0;

    for (int i = 0; i < max_tokens; i++) {
        llama_token new_token = llama_sampler_sample(sampler, ctx_, -1);

        if (llama_vocab_is_eog(vocab, new_token)) break;

        char buf[256];
        int n = llama_token_to_piece(vocab, new_token, buf, sizeof(buf), 0, true);
        std::string token_text;
        if (n > 0) {
            token_text = std::string(buf, n);
            output.write(buf, n);
        }

        auto t2 = std::chrono::steady_clock::now();
        if (on_token && !on_token(token_text)) {
            break;
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
    std::cout << "[inference] " << n_generated << " tokens in "
              << total_ms << "ms (" << (total_ms > 0 ? (n_generated * 1000 / total_ms) : 0)
              << " tok/s)" << std::endl;
    llama_sampler_free(sampler);
    result.content = output.str();
    result.completion_tokens = n_generated;
    result.generation_ms = total_ms;
    result.decode_ms = decode_us / 1000;
    result.callback_ms = callback_us / 1000;
    return result;
}
