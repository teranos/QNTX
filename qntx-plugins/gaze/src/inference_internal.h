#pragma once

#include "plugin.h"
#include "llama.h"

// Build a sampler chain (no observer instrumentation).
// Caller must llama_sampler_free() the returned chain.
llama_sampler* build_sampler_chain(
    float temperature,
    const SamplerConfig& cfg);
