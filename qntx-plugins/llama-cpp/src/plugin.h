#pragma once

#include <memory>
#include <mutex>
#include <string>

#include <grpcpp/grpcpp.h>

#include "domain.grpc.pb.h"
#include "llm.grpc.pb.h"

#define PLUGIN_VERSION "0.4.1"

// Forward declaration
struct llama_model;
struct llama_context;

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
    };

    ChatResult chat(const std::string& system_prompt,
                    const std::string& user_prompt,
                    float temperature,
                    int max_tokens);

    std::string model_name() const { return model_name_; }

private:
    llama_model* model_ = nullptr;
    llama_context* ctx_ = nullptr;
    std::string model_path_;
    std::string model_name_;
    bool backend_initialized_ = false;
    std::mutex mutex_;
};

// DomainPluginService implementation
class LlamaCppPlugin final : public protocol::DomainPluginService::Service {
public:
    LlamaCppPlugin();

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

private:
    InferenceEngine engine_;
};

// LLMService implementation
class LlamaCppLLMService final : public protocol::LLMService::Service {
public:
    explicit LlamaCppLLMService(LlamaCppPlugin* plugin);

    grpc::Status Chat(grpc::ServerContext* ctx,
                      const protocol::LLMChatRequest* req,
                      protocol::LLMChatResponse* resp) override;

private:
    LlamaCppPlugin* plugin_;
};
