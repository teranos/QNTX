#include "plugin.h"
#include "base64.h"
#include "log_capture.h"
#include "pdf_extract.h"

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

    // Extract text from attachments and prepend as context.
    //
    // The Go core sends file attachments as data URIs:
    //   data:application/pdf;base64,JVBERi0xLjQg...
    //
    // We parse the URI with string methods (find/substr), decode the
    // base64 payload, then extract text depending on the MIME type.
    // PDF goes through MuPDF; plain text is used directly.
    std::string context;
    for (const auto& att : req->attachments()) {
        const auto& uri = att.data();
        if (uri.empty()) continue;

        // Data URI format: "data:<mime>;base64,<payload>"
        // Find the comma that separates header from payload.
        auto comma = uri.find(',');
        if (comma == std::string::npos) continue;

        // The header is everything before the comma: "data:application/pdf;base64"
        // We check for known MIME types using find() — no regex needed.
        auto header = uri.substr(0, comma);
        auto payload = uri.substr(comma + 1);

        if (header.find("application/pdf") != std::string::npos) {
            // base64_decode returns vector<uint8_t> — contiguous memory,
            // so .data() gives us the raw byte pointer MuPDF needs.
            auto bytes = base64_decode(payload);
            auto text = extract_pdf_text(bytes.data(), bytes.size());
            if (!text.empty()) {
                context += "[Document: " + att.filename() + "]\n" + text + "\n\n";
            }
        } else if (header.find("text/plain") != std::string::npos) {
            auto bytes = base64_decode(payload);
            // std::string constructor from iterators — works because
            // uint8_t is just unsigned char, which char can hold.
            std::string text(bytes.begin(), bytes.end());
            if (!text.empty()) {
                context += "[Document: " + att.filename() + "]\n" + text + "\n\n";
            }
        }
    }

    // If we extracted context, prepend it to the user prompt so the
    // model sees the document content before the user's question.
    std::string user_prompt = context.empty()
        ? req->user_prompt()
        : context + req->user_prompt();

    // TODO: support streaming — return partial tokens as they're sampled
    // TODO: support multi-turn — gRPC protocol currently carries no message history
    auto result = engine.chat(req->system_prompt(),
                              user_prompt,
                              temperature,
                              max_tokens);

    resp->set_content(result.content);
    resp->set_model(engine.model_name());
    resp->set_prompt_tokens(result.prompt_tokens);
    resp->set_completion_tokens(result.completion_tokens);
    resp->set_total_tokens(result.prompt_tokens + result.completion_tokens);

    return grpc::Status::OK;
}
