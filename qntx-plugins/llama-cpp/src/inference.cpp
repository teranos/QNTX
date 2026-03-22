#include "plugin.h"

#include <iostream>
#include <sstream>
#include <vector>

#include "llama.h"

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

InferenceEngine::ChatResult InferenceEngine::chat(
    const std::string& system_prompt,
    const std::string& user_prompt,
    float temperature,
    int max_tokens) {

    std::lock_guard<std::mutex> lock(mutex_);

    ChatResult result;
    if (!model_ || !ctx_) {
        result.content = "error: no model loaded";
        return result;
    }

    // TODO: make chat template configurable via plugin config instead of hardcoding
    // Format prompt in Llama 3 format
    std::string prompt = "<|begin_of_text|>";
    if (!system_prompt.empty()) {
        prompt += "<|start_header_id|>system<|end_header_id|>\n\n"
                + system_prompt + "<|eot_id|>";
    }
    prompt += "<|start_header_id|>user<|end_header_id|>\n\n"
            + user_prompt + "<|eot_id|>"
            + "<|start_header_id|>assistant<|end_header_id|>\n\n";

    // Tokenize
    const llama_vocab* vocab = llama_model_get_vocab(model_);
    int n_prompt_max = prompt.size() + 32;
    std::vector<llama_token> tokens(n_prompt_max);
    int n_tokens = llama_tokenize(vocab, prompt.c_str(), prompt.size(),
                                   tokens.data(), n_prompt_max, true, true);
    if (n_tokens < 0) {
        result.content = "error: tokenization failed";
        return result;
    }
    tokens.resize(n_tokens);
    result.prompt_tokens = n_tokens;

    // Clear KV cache
    llama_memory_clear(llama_get_memory(ctx_), true);

    // Decode prompt
    llama_batch batch = llama_batch_get_one(tokens.data(), n_tokens);
    if (llama_decode(ctx_, batch) != 0) {
        result.content = "error: prompt decode failed";
        return result;
    }

    // Sample tokens
    auto sampler = llama_sampler_chain_init(llama_sampler_chain_default_params());
    llama_sampler_chain_add(sampler, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(sampler, llama_sampler_init_dist(0));

    std::ostringstream output;
    int n_generated = 0;

    for (int i = 0; i < max_tokens; i++) {
        llama_token new_token = llama_sampler_sample(sampler, ctx_, -1);

        if (llama_vocab_is_eog(vocab, new_token)) {
            break;
        }

        // Convert token to text
        char buf[256];
        int n = llama_token_to_piece(vocab, new_token, buf, sizeof(buf), 0, true);
        if (n > 0) {
            output.write(buf, n);
        }

        // Decode next token
        llama_batch next = llama_batch_get_one(&new_token, 1);
        if (llama_decode(ctx_, next) != 0) {
            break;
        }

        n_generated++;
    }

    llama_sampler_free(sampler);

    result.content = output.str();
    result.completion_tokens = n_generated;
    return result;
}
