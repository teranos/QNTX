#pragma once

#include "plugin.h"
#include "llama.h"

static constexpr int SIGNAL_TOP_K = 10;

// Shared between inference.cpp and vision.cpp

llama_sampler* build_sampler_chain(
    float temperature,
    const SamplerConfig& cfg,
    const llama_vocab* vocab,
    std::vector<SamplerStageSnapshot>* snapshots);

void capture_signal(llama_context* ctx, const llama_vocab* vocab, int top_k,
                    TokenSignal& sig,
                    std::vector<float>& probs_buf,
                    std::vector<int>& indices_buf);
