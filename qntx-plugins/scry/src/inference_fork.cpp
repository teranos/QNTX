#include "inference_internal.h"

#include <chrono>
#include <iostream>
#include <sstream>
#include <vector>

InferenceEngine::ChatResult InferenceEngine::fork_and_generate(
    int32_t parent_seq, int fork_pos_absolute,
    int32_t fork_token, int32_t new_seq,
    float temperature, int max_tokens,
    TokenCallback on_token, const SamplerConfig& sampler_cfg) {

    std::lock_guard<std::mutex> lock(mutex_);

    std::cout << "[scry] fork_and_generate: parent_seq=" << parent_seq
              << " fork_pos=" << fork_pos_absolute
              << " fork_token=" << fork_token
              << " new_seq=" << new_seq
              << " max_tokens=" << max_tokens << std::endl;

    // Hard safety cap
    if (max_tokens > 1024) max_tokens = 1024;

    ChatResult result;
    result.prompt_tokens = 0;
    result.completion_tokens = 0;

    if (!model_ || !ctx_) {
        result.content = "error: no model loaded";
        return result;
    }

    auto mem = llama_get_memory(ctx_);

    // Check parent sequence state before fork
    auto parent_min = llama_memory_seq_pos_min(mem, parent_seq);
    auto parent_max = llama_memory_seq_pos_max(mem, parent_seq);
    std::cout << "[scry] fork: parent seq " << parent_seq
              << " pos range [" << parent_min << "," << parent_max << "]"
              << " fork_pos=" << fork_pos_absolute << std::endl;

    // Copy full parent KV to new sequence (partial copy not supported by llama.cpp)
    // then trim the new sequence past the fork point
    llama_memory_seq_cp(mem, parent_seq, new_seq, 0, -1);
    llama_memory_seq_rm(mem, new_seq, fork_pos_absolute, -1);

    auto new_min = llama_memory_seq_pos_min(mem, new_seq);
    auto new_max = llama_memory_seq_pos_max(mem, new_seq);
    std::cout << "[scry] fork: new seq " << new_seq
              << " pos range [" << new_min << "," << new_max << "]" << std::endl;

    // Decode the fork token on the new sequence at the fork position
    auto batch = llama_batch_init(1, 0, 1);
    batch.n_tokens = 1;
    batch.token[0] = fork_token;
    batch.pos[0] = fork_pos_absolute;
    batch.n_seq_id[0] = 1;
    batch.seq_id[0][0] = new_seq;
    batch.logits[0] = 1;  // need logits for next prediction

    int decode_rc = llama_decode(ctx_, batch);
    llama_batch_free(batch);
    if (decode_rc != 0) {
        std::cout << "[scry] fork: decode failed (rc=" << decode_rc
                  << " fork_pos=" << fork_pos_absolute
                  << " token=" << fork_token
                  << " n_ctx=" << llama_n_ctx(ctx_) << ")" << std::endl;
        result.content = "error: fork token decode failed";
        return result;
    }

    result.prompt_tokens = fork_pos_absolute;

    const llama_vocab* vocab = llama_model_get_vocab(model_);
    std::vector<SamplerStageSnapshot> stage_snapshots;
    auto sampler = build_sampler_chain(temperature, sampler_cfg, vocab, &stage_snapshots);

    std::vector<float> probs_buf;
    std::vector<int> indices_buf;

    std::ostringstream output;
    int n_generated = 0;
    auto gen_start = std::chrono::steady_clock::now();
    long signal_us = 0, decode_us = 0, callback_us = 0;

    // First iteration: capture signal from the fork token decode, then generate
    llama_token current_token = fork_token;

    for (int i = 0; i < max_tokens; i++) {
        auto t0 = std::chrono::steady_clock::now();
        TokenSignal sig;
        capture_signal(ctx_, vocab, SIGNAL_TOP_K, sig, probs_buf, indices_buf);
        auto t1 = std::chrono::steady_clock::now();
        signal_us += std::chrono::duration_cast<std::chrono::microseconds>(t1 - t0).count();

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
        sig.sampler_stages = std::move(stage_snapshots);

        auto t2 = std::chrono::steady_clock::now();
        if (on_token && !on_token(sig.token_text, sig)) {
            break;
        }

        sig.full_distribution.clear();
        sig.sampler_stages.clear();
        result.signals.push_back(std::move(sig));
        auto t3 = std::chrono::steady_clock::now();
        callback_us += std::chrono::duration_cast<std::chrono::microseconds>(t3 - t2).count();

        // Decode next token on the fork sequence
        auto t4 = std::chrono::steady_clock::now();
        auto next_batch = llama_batch_init(1, 0, 1);
        next_batch.n_tokens = 1;
        next_batch.token[0] = new_token;
        next_batch.pos[0] = fork_pos_absolute + 1 + i;
        next_batch.n_seq_id[0] = 1;
        next_batch.seq_id[0][0] = new_seq;
        next_batch.logits[0] = 1;

        int decode_status = llama_decode(ctx_, next_batch);
        llama_batch_free(next_batch);
        if (decode_status != 0) break;

        auto t5 = std::chrono::steady_clock::now();
        decode_us += std::chrono::duration_cast<std::chrono::microseconds>(t5 - t4).count();
        n_generated++;
    }

    auto gen_end = std::chrono::steady_clock::now();
    auto total_ms = std::chrono::duration_cast<std::chrono::milliseconds>(gen_end - gen_start).count();
    std::cout << "[scry] fork: " << n_generated << " tokens in "
              << total_ms << "ms on seq " << new_seq << std::endl;

    llama_sampler_free(sampler);
    result.content = output.str();
    result.completion_tokens = n_generated;
    result.generation_ms = total_ms;
    result.decode_ms = decode_us / 1000;
    result.signal_ms = signal_us / 1000;
    result.callback_ms = callback_us / 1000;
    return result;
}
