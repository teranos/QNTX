#include "inference_internal.h"

#include <chrono>
#include <dirent.h>
#include <iostream>
#include <sstream>
#include <fstream>
#include <unistd.h>

#include "mtmd.h"
#include "mtmd-helper.h"

// Redirect stderr to a temp file, call fn(), restore stderr, return captured output.
// Uses a file instead of a pipe to avoid deadlock when output exceeds pipe buffer.
static std::string capture_stderr(std::function<void()> fn) {
    char tmpl[] = "/tmp/scry-clip-XXXXXX";
    int tmpfd = mkstemp(tmpl);
    if (tmpfd < 0) {
        fn();
        return "";
    }

    fflush(stderr);
    int saved = dup(STDERR_FILENO);
    dup2(tmpfd, STDERR_FILENO);
    close(tmpfd);

    fn();

    fflush(stderr);
    dup2(saved, STDERR_FILENO);
    close(saved);

    // Read captured output from temp file
    std::ifstream f(tmpl);
    std::string captured((std::istreambuf_iterator<char>(f)),
                          std::istreambuf_iterator<char>());
    f.close();
    unlink(tmpl);
    return captured;
}

// Count lines in a string.
static int count_lines(const std::string& s) {
    int n = 0;
    for (char c : s) {
        if (c == '\n') n++;
    }
    return n;
}

// Auto-detect mmproj in the same directory as the model file.
// Tries mmproj-*.gguf first, then the model file itself (bundled tensors).
void InferenceEngine::init_vision(const std::string& model_path) {
    auto mparams = mtmd_context_params_default();
    mparams.use_gpu = true;
    mparams.print_timings = true;
    mparams.warmup = true;

    std::string mmproj_path;
    auto slash = model_path.find_last_of('/');
    if (slash != std::string::npos) {
        std::string dir = model_path.substr(0, slash + 1);
        DIR* d = opendir(dir.c_str());
        if (d) {
            struct dirent* entry;
            while ((entry = readdir(d)) != nullptr) {
                std::string name(entry->d_name);
                if (name.find("mmproj") == 0 && name.find(".gguf") != std::string::npos) {
                    mmproj_path = dir + name;
                    break;
                }
            }
            closedir(d);
        }
    }

    // mtmd_init_from_file dumps clip_model_loader tensor info directly to stderr,
    // bypassing llama_log_set. Capture and condense into a single summary line.
    std::string clip_stderr = capture_stderr([&]() {
        if (!mmproj_path.empty()) {
            mtmd_ctx_ = mtmd_init_from_file(mmproj_path.c_str(), model_, mparams);
        } else {
            mtmd_ctx_ = mtmd_init_from_file(model_path.c_str(), model_, mparams);
        }
    });

    if (mtmd_ctx_ && mtmd_support_vision(mtmd_ctx_)) {
        int n_lines = count_lines(clip_stderr);
        std::cout << "[scry] Vision: yes"
                  << (mmproj_path.empty() ? " (bundled)" : " (" + mmproj_path + ")");
        if (n_lines > 0) {
            std::cout << ", " << n_lines << " clip loader lines condensed";
        }
        std::cout << std::endl;
    } else {
        if (mtmd_ctx_) {
            mtmd_free(mtmd_ctx_);
            mtmd_ctx_ = nullptr;
        }
    }
}

void InferenceEngine::cleanup_vision() {
    if (mtmd_ctx_) {
        mtmd_free(mtmd_ctx_);
        mtmd_ctx_ = nullptr;
    }
}

// Prepare prompt with vision: tokenize text + images through mtmd, decode into KV cache.
// Returns total token count (text + image tokens), or -1 on error.
int InferenceEngine::prepare_prompt_vision(
    const std::vector<Message>& messages,
    const std::vector<ImageAttachment>& images,
    ChatResult& result) {

    if (!mtmd_ctx_) {
        result.content = "error: no mmproj loaded — set mmproj_path in plugin config";
        return -1;
    }

    // Build prompt using the model's chat template, with media markers for images.
    const char* marker = mtmd_default_marker();
    const char* tmpl = llama_model_chat_template(model_, nullptr);

    // Insert image markers into the last user message content
    std::vector<Message> patched_messages = messages;
    for (int i = (int)patched_messages.size() - 1; i >= 0; i--) {
        if (patched_messages[i].role == "user") {
            std::string prefix;
            for (size_t j = 0; j < images.size(); j++) {
                prefix += marker;
                prefix += "\n";
            }
            patched_messages[i].content = prefix + patched_messages[i].content;
            break;
        }
    }

    // Apply chat template
    std::vector<llama_chat_message> chat_msgs;
    chat_msgs.reserve(patched_messages.size());
    for (const auto& m : patched_messages) {
        chat_msgs.push_back({m.role.c_str(), m.content.c_str()});
    }
    size_t total = 0;
    for (const auto& m : patched_messages) total += m.content.size();
    int alloc = 2 * total + 256;
    std::vector<char> buf(alloc);
    int n = llama_chat_apply_template(tmpl, chat_msgs.data(), chat_msgs.size(),
                                      true, buf.data(), buf.size());
    if (n > (int)buf.size()) {
        buf.resize(n + 1);
        n = llama_chat_apply_template(tmpl, chat_msgs.data(), chat_msgs.size(),
                                      true, buf.data(), buf.size());
    }
    if (n < 0) {
        result.content = "error: failed to apply chat template";
        return -1;
    }
    std::string prompt(buf.data(), n);

    // Create bitmaps from raw image bytes
    mtmd::bitmaps bitmaps;
    for (size_t i = 0; i < images.size(); i++) {
        auto* bmp = mtmd_helper_bitmap_init_from_buf(
            mtmd_ctx_, images[i].data.data(), images[i].data.size());
        if (!bmp) {
            result.content = "error: failed to decode image attachment " + std::to_string(i);
            return -1;
        }
        bitmaps.entries.emplace_back(bmp);
    }

    // Tokenize text + images
    auto chunks = mtmd::input_chunks(mtmd_input_chunks_init());
    auto bitmaps_c = bitmaps.c_ptr();
    mtmd_input_text text_input;
    text_input.text = prompt.c_str();
    text_input.add_special = true;
    text_input.parse_special = true;

    int32_t tok_result = mtmd_tokenize(mtmd_ctx_, chunks.ptr.get(),
                                        &text_input,
                                        bitmaps_c.data(), bitmaps_c.size());
    if (tok_result != 0) {
        result.content = "error: mtmd_tokenize failed (code " + std::to_string(tok_result) + ")";
        return -1;
    }

    size_t n_tokens = mtmd_helper_get_n_tokens(chunks.ptr.get());
    std::cout << "[scry] Vision prompt: " << n_tokens << " tokens ("
              << images.size() << " images, " << chunks.size() << " chunks)" << std::endl;

    // Clear KV cache
    llama_memory_clear(llama_get_memory(ctx_), true);

    // Eval all chunks (text + image embeddings)
    auto t0 = std::chrono::steady_clock::now();
    llama_pos new_n_past = 0;
    int32_t eval_result = mtmd_helper_eval_chunks(
        mtmd_ctx_, ctx_, chunks.ptr.get(),
        0,     // n_past
        0,     // seq_id
        2048,  // n_batch
        true,  // logits_last
        &new_n_past);

    if (eval_result != 0) {
        result.content = "error: mtmd_helper_eval_chunks failed (code " + std::to_string(eval_result) + ")";
        return -1;
    }

    auto t1 = std::chrono::steady_clock::now();
    result.prompt_eval_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t1 - t0).count();

    return static_cast<int>(n_tokens);
}

InferenceEngine::ChatResult InferenceEngine::chat_vision(
    const std::vector<Message>& messages,
    const std::vector<ImageAttachment>& images,
    float temperature,
    int max_tokens,
    const SamplerConfig& sampler_cfg) {

    std::lock_guard<std::mutex> lock(mutex_);

    ChatResult result;
    if (!model_ || !ctx_) {
        result.content = "error: no model loaded";
        return result;
    }

    int n_tokens = prepare_prompt_vision(messages, images, result);
    if (n_tokens < 0) return result;
    result.prompt_tokens = n_tokens;

    const llama_vocab* vocab = llama_model_get_vocab(model_);
    std::vector<SamplerStageSnapshot> stage_snapshots;
    auto sampler = build_sampler_chain(temperature, sampler_cfg, vocab, &stage_snapshots);

    std::vector<float> probs_buf;
    std::vector<int> indices_buf;

    std::ostringstream output;
    int n_generated = 0;

    for (int i = 0; i < max_tokens; i++) {
        TokenSignal sig;
        capture_signal(ctx_, vocab, SIGNAL_TOP_K, sig, probs_buf, indices_buf);

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
        sig.full_distribution.clear();
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

InferenceEngine::ChatResult InferenceEngine::stream_chat_vision(
    const std::vector<Message>& messages,
    const std::vector<ImageAttachment>& images,
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

    int n_tokens = prepare_prompt_vision(messages, images, result);
    if (n_tokens < 0) return result;
    result.prompt_tokens = n_tokens;

    const llama_vocab* vocab = llama_model_get_vocab(model_);
    std::vector<SamplerStageSnapshot> stage_snapshots;
    auto sampler = build_sampler_chain(temperature, sampler_cfg, vocab, &stage_snapshots);

    std::vector<float> probs_buf;
    std::vector<int> indices_buf;

    std::ostringstream output;
    int n_generated = 0;
    auto gen_start = std::chrono::steady_clock::now();
    long signal_us = 0, decode_us = 0, callback_us = 0;

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

        auto t4 = std::chrono::steady_clock::now();
        llama_batch next = llama_batch_get_one(&new_token, 1);
        if (llama_decode(ctx_, next) != 0) break;
        auto t5 = std::chrono::steady_clock::now();
        decode_us += std::chrono::duration_cast<std::chrono::microseconds>(t5 - t4).count();
        n_generated++;
    }

    auto gen_end = std::chrono::steady_clock::now();
    auto total_ms = std::chrono::duration_cast<std::chrono::milliseconds>(gen_end - gen_start).count();
    std::cout << "[scry] vision: " << n_generated << " tokens in "
              << total_ms << "ms (" << (total_ms > 0 ? (n_generated * 1000 / total_ms) : 0)
              << " tok/s)" << std::endl;
    llama_sampler_free(sampler);
    result.content = output.str();
    result.completion_tokens = n_generated;
    result.generation_ms = total_ms;
    result.decode_ms = decode_us / 1000;
    result.signal_ms = signal_us / 1000;
    result.callback_ms = callback_us / 1000;
    return result;
}
