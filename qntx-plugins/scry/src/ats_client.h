#pragma once

#include <memory>
#include <string>
#include <vector>

#include <grpcpp/grpcpp.h>

#include "atsstore.grpc.pb.h"

struct TokenSignal;

struct GenerationPerf {
    long prompt_eval_ms = 0;
    long generation_ms = 0;
    long decode_ms = 0;
    long signal_ms = 0;
    long callback_ms = 0;
    int completion_tokens = 0;
};

// gRPC client for ATSStoreService — writes attestations after inference
class AtsClient {
public:
    void configure(const std::string& endpoint, const std::string& auth_token);
    bool is_configured() const { return stub_ != nullptr; }

    // Write a Weave attestation for a completed generation.
    // Token signals are packed into the attributes — no separate Token attestations.
    // Performance breakdown is embedded so regressions are detectable from attestation history.
    bool create_weave(const std::string& model_name,
                      const std::string& prompt,
                      const std::string& response_text,
                      const std::string& context_id,
                      int token_count,
                      float mean_confidence,
                      float mean_entropy,
                      const std::vector<TokenSignal>& signals,
                      const GenerationPerf& perf);

private:
    std::unique_ptr<protocol::ATSStoreService::Stub> stub_;
    std::string auth_token_;
};
