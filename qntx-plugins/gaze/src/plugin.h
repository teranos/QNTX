#pragma once

#include <functional>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <vector>

#include <grpcpp/grpcpp.h>

#include "domain.grpc.pb.h"
#include "llm.grpc.pb.h"

#define PLUGIN_VERSION "0.2.0"

// Forward declarations
struct llama_model;
struct llama_context;
struct mtmd_context;

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

// Inference engine wrapping llama.cpp
class InferenceEngine {
public:
    InferenceEngine();
    ~InferenceEngine();

    bool load_model(const std::string& model_path, int n_ctx = 2048);
    void unload();
    bool is_loaded() const;
    bool has_vision() const { return mtmd_ctx_ != nullptr; }

    struct ChatResult {
        std::string content;
        int prompt_tokens;
        int completion_tokens;
        std::string warning;  // non-empty if prompt was truncated to fit n_ctx

        // Performance breakdown (milliseconds)
        long prompt_eval_ms = 0;
        long generation_ms = 0;
        long decode_ms = 0;
        long callback_ms = 0;
    };

    struct Message {
        std::string role;    // "system", "user", "assistant"
        std::string content;
    };

    // Callback receives token text per step. Return false to abort.
    using TokenCallback = std::function<bool(const std::string& token_text)>;

    // Multi-turn streaming
    ChatResult stream_chat(const std::vector<Message>& messages,
                           float temperature,
                           int max_tokens,
                           TokenCallback on_token,
                           const SamplerConfig& sampler_cfg = {});

    // Image data for vision: raw bytes (decoded PNG/JPG)
    struct ImageAttachment {
        std::vector<uint8_t> data;
    };

    // Multi-turn streaming with image attachments (vision)
    ChatResult stream_chat_vision(const std::vector<Message>& messages,
                                   const std::vector<ImageAttachment>& images,
                                   float temperature,
                                   int max_tokens,
                                   TokenCallback on_token,
                                   const SamplerConfig& sampler_cfg = {});

    std::string model_name() const { return model_name_; }
    int vocab_size() const;
    std::string token_text(int token_id) const;

private:
    int prepare_prompt(const std::vector<Message>& messages,
                       ChatResult& result);
    void init_vision(const std::string& model_path);
    void cleanup_vision();
    int prepare_prompt_vision(const std::vector<Message>& messages,
                              const std::vector<ImageAttachment>& images,
                              ChatResult& result);
    llama_model* model_ = nullptr;
    llama_context* ctx_ = nullptr;
    mtmd_context* mtmd_ctx_ = nullptr;
    std::string model_path_;
    std::string model_name_;
    std::mutex mutex_;
};

// DomainPluginService implementation
class GazePlugin final : public protocol::DomainPluginService::Service {
public:
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

    // Get engine by model name. Returns nullptr if not found.
    InferenceEngine* get_engine(const std::string& model_name);

    // Get the first (or only) engine. Returns nullptr if none loaded.
    InferenceEngine* default_engine();

    // List all advertised model names.
    std::vector<std::string> model_names() const;

    // Activity status for UI feedback
    void set_activity(const std::string& a) { std::lock_guard<std::mutex> l(activity_mu_); activity_ = a; }
    std::string activity() { std::lock_guard<std::mutex> l(activity_mu_); return activity_; }

    const SamplerConfig& sampler_config() const { return sampler_cfg_; }

private:
    std::map<std::string, std::unique_ptr<InferenceEngine>> engines_;
    SamplerConfig sampler_cfg_;

    // Activity status
    std::mutex activity_mu_;
    std::string activity_;
};

// LLMService implementation
class GazeLLMService final : public protocol::LLMService::Service {
public:
    explicit GazeLLMService(GazePlugin* plugin);

    grpc::Status Chat(grpc::ServerContext* ctx,
                      const protocol::LLMChatRequest* req,
                      protocol::LLMChatResponse* resp) override;

    grpc::Status StreamChat(grpc::ServerContext* ctx,
                            const protocol::LLMChatRequest* req,
                            grpc::ServerWriter<protocol::LLMChatChunk>* writer) override;

private:
    GazePlugin* plugin_;
};
