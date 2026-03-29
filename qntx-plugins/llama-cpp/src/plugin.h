#pragma once

#include <atomic>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

#include <grpcpp/grpcpp.h>

#include "domain.grpc.pb.h"
#include "llm.grpc.pb.h"
#include "ats_client.h"

#define PLUGIN_VERSION "0.21.1"

// Forward declarations
struct llama_model;
struct llama_context;
class MetalRenderer;

// Sampler chain configuration — controls which samplers are active and their params.
// Defaults produce the same behavior as the previous temp-only chain.
struct SamplerConfig {
    int top_k = 0;                   // 0 = disabled
    float top_p = 1.0f;             // 1.0 = disabled (nucleus sampling)
    float min_p = 0.0f;             // 0.0 = disabled
    float typical_p = 1.0f;         // 1.0 = disabled
    int penalty_last_n = 0;         // 0 = disabled
    float penalty_repeat = 1.0f;    // 1.0 = disabled
    float penalty_freq = 0.0f;      // 0.0 = disabled
    float penalty_present = 0.0f;   // 0.0 = disabled
};

// A candidate token and its probability from the pre-sampler logit distribution
struct TokenCandidate {
    int id;
    std::string text;
    float prob;
};

// Snapshot of the token distribution at a point in the sampler chain.
// Captured by observer samplers inserted between each stage.
struct SamplerStageSnapshot {
    std::string stage_name;          // e.g. "penalties", "top_k", "top_p", "temp"
    int active_count;                // tokens remaining with nonzero probability
    float top1_prob;                 // probability of top token after this stage
    float entropy;                   // Shannon entropy after this stage
    std::vector<TokenCandidate> top_k;  // top-5 candidates after this stage
};

// Per-token signal captured before sampling
struct TokenSignal {
    int token_id;
    std::string token_text;
    float confidence;                      // P(chosen) from raw distribution
    float entropy;                         // Shannon entropy in bits
    float top_gap;                         // P(top1) - P(top2)
    std::vector<TokenCandidate> top_k;     // top-k candidates with probabilities
    std::vector<float> full_distribution;  // Full softmax distribution (vocab_size floats)
    std::vector<SamplerStageSnapshot> sampler_stages;  // per-stage snapshots through chain
};

// Inference engine wrapping llama.cpp
class InferenceEngine {
public:
    InferenceEngine();
    ~InferenceEngine();

    bool load_model(const std::string& model_path, int n_ctx = 2048);
    void unload();
    bool is_loaded() const;

    struct ChatResult {
        std::string content;
        int prompt_tokens;
        int completion_tokens;
        std::vector<TokenSignal> signals;
    };

    // Single-turn (deprecated, wraps multi-turn)
    ChatResult chat(const std::string& system_prompt,
                    const std::string& user_prompt,
                    float temperature,
                    int max_tokens,
                    const SamplerConfig& sampler_cfg = {});

    // Multi-turn: messages is a vector of {role, content} pairs
    struct Message {
        std::string role;    // "system", "user", "assistant"
        std::string content;
    };

    ChatResult chat(const std::vector<Message>& messages,
                    float temperature,
                    int max_tokens,
                    const SamplerConfig& sampler_cfg = {});

    // Callback receives token text + signal per step. Return false to abort.
    using TokenCallback = std::function<bool(const std::string& token_text, const TokenSignal& signal)>;

    // Single-turn streaming (deprecated, wraps multi-turn)
    ChatResult stream_chat(const std::string& system_prompt,
                           const std::string& user_prompt,
                           float temperature,
                           int max_tokens,
                           TokenCallback on_token,
                           const SamplerConfig& sampler_cfg = {});

    // Multi-turn streaming
    ChatResult stream_chat(const std::vector<Message>& messages,
                           float temperature,
                           int max_tokens,
                           TokenCallback on_token,
                           const SamplerConfig& sampler_cfg = {});

    std::string model_name() const { return model_name_; }

    // Get vocab token positions projected to 3D via PCA.
    // Computed once at model load, cached. Returns vocab_size × 3 floats.
    const std::vector<float>& vocab_positions_3d();

private:
    void compute_vocab_positions();
    bool load_vocab_cache();
    void write_vocab_cache();
    int prepare_prompt(const std::vector<Message>& messages,
                       ChatResult& result);
    llama_model* model_ = nullptr;
    llama_context* ctx_ = nullptr;
    std::string model_path_;
    std::string model_name_;
    bool backend_initialized_ = false;
    std::mutex mutex_;
    std::vector<float> vocab_positions_;  // cached 3D positions (n_vocab × 3)
};

// DomainPluginService implementation
class LlamaCppPlugin final : public protocol::DomainPluginService::Service {
public:
    LlamaCppPlugin();
    ~LlamaCppPlugin();

    grpc::Status Metadata(grpc::ServerContext* ctx,
                          const protocol::Empty* req,
                          protocol::MetadataResponse* resp) override;

    grpc::Status Initialize(grpc::ServerContext* ctx,
                            const protocol::InitializeRequest* req,
                            protocol::InitializeResponse* resp) override;

    grpc::Status Shutdown(grpc::ServerContext* ctx,
                          const protocol::Empty* req,
                          protocol::Empty* resp) override;

    grpc::Status Health(grpc::ServerContext* ctx,
                        const protocol::Empty* req,
                        protocol::HealthResponse* resp) override;

    grpc::Status ConfigSchema(grpc::ServerContext* ctx,
                              const protocol::Empty* req,
                              protocol::ConfigSchemaResponse* resp) override;

    grpc::Status RegisterGlyphs(grpc::ServerContext* ctx,
                                const protocol::Empty* req,
                                protocol::GlyphDefResponse* resp) override;

    grpc::Status HandleHTTP(grpc::ServerContext* ctx,
                            const protocol::HTTPRequest* req,
                            protocol::HTTPResponse* resp) override;

    grpc::Status HandleWebSocket(grpc::ServerContext* ctx,
                                 grpc::ServerReaderWriter<protocol::WebSocketMessage,
                                                          protocol::WebSocketMessage>* stream) override;

    grpc::Status ExecuteJob(grpc::ServerContext* ctx,
                            const protocol::ExecuteJobRequest* req,
                            protocol::ExecuteJobResponse* resp) override;

    grpc::Status ParseAxQuery(grpc::ServerContext* ctx,
                              const protocol::ParseAxQueryRequest* req,
                              protocol::ParseAxQueryResponse* resp) override;

    InferenceEngine& engine() { return engine_; }
    MetalRenderer& renderer() { return *renderer_; }
    AtsClient& ats_client() { return ats_client_; }

    // PCA readiness — set to true once background thread finishes
    bool pca_ready() const { return pca_ready_.load(std::memory_order_acquire); }

    const SamplerConfig& sampler_config() const { return sampler_cfg_; }

private:
    InferenceEngine engine_;
    std::unique_ptr<MetalRenderer> renderer_;
    AtsClient ats_client_;
    SamplerConfig sampler_cfg_;

    // Background PCA computation thread
    std::thread pca_thread_;
    std::atomic<bool> pca_ready_{false};
};

// LLMService implementation
class LlamaCppLLMService final : public protocol::LLMService::Service {
public:
    explicit LlamaCppLLMService(LlamaCppPlugin* plugin);

    grpc::Status Chat(grpc::ServerContext* ctx,
                      const protocol::LLMChatRequest* req,
                      protocol::LLMChatResponse* resp) override;

    grpc::Status StreamChat(grpc::ServerContext* ctx,
                            const protocol::LLMChatRequest* req,
                            grpc::ServerWriter<protocol::LLMChatChunk>* writer) override;

private:
    LlamaCppPlugin* plugin_;
};
