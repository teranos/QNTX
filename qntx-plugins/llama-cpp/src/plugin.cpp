#include "plugin.h"
#include "log_capture.h"

#include <iostream>

// --- LlamaCppPlugin (DomainPluginService) ---

LlamaCppPlugin::LlamaCppPlugin() {}

grpc::Status LlamaCppPlugin::Metadata(grpc::ServerContext* ctx,
                                       const protocol::Empty* req,
                                       protocol::MetadataResponse* resp) {
    resp->set_name("llama-cpp");
    resp->set_version(PLUGIN_VERSION);
    resp->set_description("Local LLM inference via llama.cpp with Metal acceleration");
    resp->set_author("teranos");
    resp->set_license("MIT");
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::Initialize(grpc::ServerContext* ctx,
                                         const protocol::InitializeRequest* req,
                                         protocol::InitializeResponse* resp) {
    // Load model from config — gather all params first, load once
    auto config = req->config();
    auto it = config.find("model_path");
    if (it != config.end() && !it->second.empty()) {
        int n_ctx = 2048;
        auto ctx_it = config.find("n_ctx");
        if (ctx_it != config.end()) {
            try { n_ctx = std::stoi(ctx_it->second); } catch (...) {}
            if (n_ctx <= 0) n_ctx = 2048;
        }
        if (!engine_.load_model(it->second, n_ctx)) {
            std::cout << "[llama-cpp] Failed to load model: " << it->second << std::endl;
        }
    }

    // Flush condensed log summary — replaces 1000+ lines of stderr with a few stdout lines
    auto log_it = config.find("log_level");
    std::string log_level = (log_it != config.end()) ? log_it->second : "info";
    LogCapture::instance().flush_summary(log_level);

    // This plugin is an LLM provider
    resp->set_llm_provider(true);

    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::Shutdown(grpc::ServerContext* ctx,
                                       const protocol::Empty* req,
                                       protocol::Empty* resp) {
    engine_.unload();
    std::cout << "[llama-cpp] Shutdown complete" << std::endl;
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::Health(grpc::ServerContext* ctx,
                                     const protocol::Empty* req,
                                     protocol::HealthResponse* resp) {
    resp->set_healthy(true);
    if (engine_.is_loaded()) {
        resp->set_message("model loaded: " + engine_.model_name());
    } else {
        resp->set_message("no model loaded");
    }
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::ConfigSchema(grpc::ServerContext* ctx,
                                           const protocol::Empty* req,
                                           protocol::ConfigSchemaResponse* resp) {
    auto* fields = resp->mutable_fields();

    protocol::ConfigFieldSchema model_field;
    model_field.set_type("string");
    model_field.set_description("Path to GGUF model file");
    model_field.set_required(true);
    (*fields)["model_path"] = model_field;

    protocol::ConfigFieldSchema ctx_field;
    ctx_field.set_type("number");
    ctx_field.set_description("Context window size in tokens");
    ctx_field.set_default_value("2048");
    ctx_field.set_min_value("512");
    ctx_field.set_max_value("32768");
    (*fields)["n_ctx"] = ctx_field;

    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::RegisterGlyphs(grpc::ServerContext* ctx,
                                              const protocol::Empty* req,
                                              protocol::GlyphDefResponse* resp) {
    // No custom glyphs — chat goes through the prompt glyph
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::HandleHTTP(grpc::ServerContext* ctx,
                                         const protocol::HTTPRequest* req,
                                         protocol::HTTPResponse* resp) {
    resp->set_status_code(404);
    resp->set_body("not found");
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::HandleWebSocket(
    grpc::ServerContext* ctx,
    grpc::ServerReaderWriter<protocol::WebSocketMessage,
                             protocol::WebSocketMessage>* stream) {
    return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "no websocket handlers");
}

grpc::Status LlamaCppPlugin::ExecuteJob(grpc::ServerContext* ctx,
                                         const protocol::ExecuteJobRequest* req,
                                         protocol::ExecuteJobResponse* resp) {
    resp->set_success(false);
    resp->set_error("llama-cpp does not handle async jobs");
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::ParseAxQuery(grpc::ServerContext* ctx,
                                           const protocol::ParseAxQueryRequest* req,
                                           protocol::ParseAxQueryResponse* resp) {
    resp->set_error("llama-cpp does not parse Ax queries");
    return grpc::Status::OK;
}

// --- LlamaCppLLMService ---

LlamaCppLLMService::LlamaCppLLMService(LlamaCppPlugin* plugin)
    : plugin_(plugin) {}

grpc::Status LlamaCppLLMService::Chat(grpc::ServerContext* ctx,
                                       const protocol::LLMChatRequest* req,
                                       protocol::LLMChatResponse* resp) {
    auto& engine = plugin_->engine();

    if (!engine.is_loaded()) {
        return grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                            "no model loaded — set model_path in plugin config");
    }

    float temperature = req->temperature() > 0 ? req->temperature() : 0.7;
    int max_tokens = req->max_tokens() > 0 ? req->max_tokens() : 512;

    auto result = engine.chat(req->system_prompt(),
                              req->user_prompt(),
                              temperature,
                              max_tokens);

    resp->set_content(result.content);
    resp->set_model(engine.model_name());
    resp->set_prompt_tokens(result.prompt_tokens);
    resp->set_completion_tokens(result.completion_tokens);
    resp->set_total_tokens(result.prompt_tokens + result.completion_tokens);

    return grpc::Status::OK;
}
