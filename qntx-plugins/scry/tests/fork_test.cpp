// Integration test for fork_and_generate.
// Loads a real model, generates tokens, forks, and verifies the fork path works.
// Requires SCRY_TEST_MODEL env var pointing to a GGUF file.

#include "../src/plugin.h"

#include <cassert>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>

static void test_fork_basic() {
    const char* model_path = getenv("SCRY_TEST_MODEL");
    if (!model_path) {
        printf("  SKIP fork_basic: set SCRY_TEST_MODEL to a .gguf path\n");
        return;
    }

    InferenceEngine engine;
    bool loaded = engine.load_model(model_path, 2048);
    assert(loaded && "model must load");
    printf("  model loaded: %s\n", engine.model_name().c_str());

    // Generate a short response to populate KV cache on seq 0
    std::vector<InferenceEngine::Message> messages = {
        {"user", "Say hello in exactly five words."}
    };

    std::vector<TokenSignal> root_signals;
    int root_token_count = 0;

    auto result = engine.stream_chat(messages, 0.7f, 30,
        [&](const std::string& text, const TokenSignal& sig) -> bool {
            root_signals.push_back(sig);
            root_token_count++;
            return true;
        },
        SamplerConfig{});

    printf("  root generation: %d tokens, prompt=%d\n",
           result.completion_tokens, result.prompt_tokens);
    assert(result.completion_tokens > 0 && "must generate tokens");
    assert(result.prompt_tokens > 0 && "must have prompt tokens");

    // Pick fork point at midway through generation
    int fork_pos = result.prompt_tokens + result.completion_tokens / 2;
    printf("  fork_pos_absolute: %d (prompt=%d + half_completion=%d)\n",
           fork_pos, result.prompt_tokens, result.completion_tokens / 2);

    // Pick an alternative token from the signals at the fork point
    int fork_signal_idx = result.completion_tokens / 2;
    int fork_token_id = -1;
    if (fork_signal_idx < (int)root_signals.size()) {
        const auto& sig = root_signals[fork_signal_idx];
        // Use top-k candidate that isn't the chosen token
        for (const auto& cand : sig.top_k) {
            if (cand.id != sig.token_id) {
                fork_token_id = cand.id;
                break;
            }
        }
    }

    if (fork_token_id < 0) {
        printf("  SKIP: no alternative candidate found at fork point\n");
        return;
    }

    printf("  forking: token_id=%d at pos=%d on new_seq=1\n",
           fork_token_id, fork_pos);

    // Fork: copy KV cache, decode alternative token, generate onward
    int fork_tokens = 0;
    std::string fork_text;

    auto fork_result = engine.fork_and_generate(
        0,              // parent_seq
        fork_pos,       // fork_pos_absolute
        fork_token_id,  // alternative token
        1,              // new_seq
        0.7f,           // temperature
        20,             // max_tokens
        [&](const std::string& text, const TokenSignal& sig) -> bool {
            fork_tokens++;
            fork_text += text;
            return true;
        },
        SamplerConfig{});

    printf("  fork result: completion_tokens=%d, callback_count=%d\n",
           fork_result.completion_tokens, fork_tokens);
    printf("  fork text: \"%s\"\n", fork_text.c_str());

    // Assertions
    assert(fork_result.completion_tokens >= 0 &&
           "completion_tokens must not be garbage");
    assert(fork_result.completion_tokens <= 20 &&
           "completion_tokens must respect max_tokens cap");
    assert(fork_result.completion_tokens == fork_tokens &&
           "completion_tokens must match callback count");
    assert(fork_result.completion_tokens > 0 &&
           "fork must generate at least one token");
    assert(!fork_text.empty() && "fork must produce text");

    printf("  fork_basic: OK\n");
}

static void test_fork_completion_tokens_initialized() {
    // Verify ChatResult fields are zero-initialized even when model isn't loaded
    InferenceEngine engine;
    // Don't load model — fork should return cleanly with 0 tokens

    auto result = engine.fork_and_generate(
        0, 100, 1, 1, 0.7f, 10,
        [](const std::string&, const TokenSignal&) { return true; },
        SamplerConfig{});

    printf("  no-model fork: completion_tokens=%d content=\"%s\"\n",
           result.completion_tokens, result.content.c_str());

    assert(result.completion_tokens == 0 &&
           "failed fork must return 0 tokens, not garbage");

    printf("  fork_completion_tokens_initialized: OK\n");
}

int main() {
    printf("fork_test:\n");
    test_fork_basic();
    test_fork_completion_tokens_initialized();
    printf("all fork tests passed\n");
    return 0;
}
