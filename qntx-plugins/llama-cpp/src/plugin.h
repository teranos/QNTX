#pragma once

#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <vector>

#include <grpcpp/grpcpp.h>

#include "domain.grpc.pb.h"
#include "llm.grpc.pb.h"

#define PLUGIN_VERSION "0.12.0"

// Forward declarations
struct llama_model;
struct llama_context;
class MetalRenderer;

// A candidate token and its probability from the pre-sampler logit distribution
struct TokenCandidate {
    int id;
    std::string text;
    float prob;
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

    ChatResult chat(const std::string& system_prompt,
                    const std::string& user_prompt,
                    float temperature,
                    int max_tokens);

    // Callback receives token text + signal per step. Return false to abort.
    using TokenCallback = std::function<bool(const std::string& token_text, const TokenSignal& signal)>;

    // Streaming chat — calls on_token for each generated token
    ChatResult stream_chat(const std::string& system_prompt,
                           const std::string& user_prompt,
                           float temperature,
                           int max_tokens,
                           TokenCallback on_token);

    std::string model_name() const { return model_name_; }

    // Get vocab token positions projected to 3D via PCA.
    // Computed once at model load, cached. Returns vocab_size × 3 floats.
    const std::vector<float>& vocab_positions_3d();

private:
    void compute_vocab_positions();
    int prepare_prompt(const std::string& system_prompt,
                       const std::string& user_prompt,
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

private:
    InferenceEngine engine_;
    std::unique_ptr<MetalRenderer> renderer_;
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
