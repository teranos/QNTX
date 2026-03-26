#include "plugin.h"
#include "base64.h"
#include "log_capture.h"
#include "metal_renderer.h"
#include "pdf_extract.h"

#include <algorithm>
#include <iostream>
#include <vector>

// --- LlamaCppPlugin (DomainPluginService) ---

LlamaCppPlugin::LlamaCppPlugin() : renderer_(std::make_unique<MetalRenderer>()) {
    renderer_->setup();
}

LlamaCppPlugin::~LlamaCppPlugin() = default;

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
    if (req->method() == "POST" && req->path() == "/render-test") {
        if (!renderer_->is_ready()) {
            resp->set_status_code(503);
            resp->set_body("{\"error\":\"Metal not available\"}");
            auto* h = resp->add_headers();
            h->set_name("Content-Type");
            h->add_values("application/json");
            return grpc::Status::OK;
        }

        // Use real PCA positions if model is loaded
        if (engine_.is_loaded()) {
            const auto& pos = engine_.vocab_positions_3d();
            if (!pos.empty()) {
                renderer_->set_vocab_positions(pos.data(), pos.size() / 3);
            }
        }

        int w = 800, h = 600;
        auto pixels = renderer_->render_test(w, h);
        if (pixels.empty()) {
            resp->set_status_code(500);
            resp->set_body("{\"error\":\"render failed\"}");
            auto* hdr = resp->add_headers();
            hdr->set_name("Content-Type");
            hdr->add_values("application/json");
            return grpc::Status::OK;
        }

        resp->set_status_code(200);
        resp->set_body(pixels.data(), pixels.size());
        auto* ct = resp->add_headers();
        ct->set_name("Content-Type");
        ct->add_values("application/octet-stream");
        auto* wh = resp->add_headers();
        wh->set_name("X-Width");
        wh->add_values(std::to_string(w));
        auto* hh = resp->add_headers();
        hh->set_name("X-Height");
        hh->add_values(std::to_string(h));
        return grpc::Status::OK;
    }

    if (req->method() == "GET" && req->path() == "/vocab-positions") {
        if (!engine_.is_loaded()) {
            resp->set_status_code(503);
            resp->set_body("{\"error\":\"no model loaded\"}");
            auto* h = resp->add_headers();
            h->set_name("Content-Type");
            h->add_values("application/json");
            return grpc::Status::OK;
        }

        const auto& positions = engine_.vocab_positions_3d();
        if (positions.empty()) {
            resp->set_status_code(500);
            resp->set_body("{\"error\":\"failed to compute vocab positions\"}");
            auto* h = resp->add_headers();
            h->set_name("Content-Type");
            h->add_values("application/json");
            return grpc::Status::OK;
        }

        // Return as raw float32 array (n_vocab × 3), binary
        int n_vocab = positions.size() / 3;
        resp->set_status_code(200);
        resp->set_body(positions.data(), positions.size() * sizeof(float));
        auto* h = resp->add_headers();
        h->set_name("Content-Type");
        h->add_values("application/octet-stream");
        auto* h2 = resp->add_headers();
        h2->set_name("X-Vocab-Size");
        h2->add_values(std::to_string(n_vocab));
        return grpc::Status::OK;
    }

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

    // Log signal summary for runtime verification
    if (!result.signals.empty()) {
        float ent_sum = 0, ent_max = 0, conf_sum = 0, conf_min = 1.0;
        for (const auto& sig : result.signals) {
            ent_sum += sig.entropy;
            if (sig.entropy > ent_max) ent_max = sig.entropy;
            conf_sum += sig.confidence;
            if (sig.confidence < conf_min) conf_min = sig.confidence;
        }
        int n = result.signals.size();
        std::cout << "[llama-cpp] signals: " << n << " tokens"
                  << " | entropy avg=" << (ent_sum / n)
                  << " max=" << ent_max
                  << " | confidence avg=" << (conf_sum / n)
                  << " min=" << conf_min << std::endl;

        // Show the 3 lowest-confidence tokens
        std::vector<size_t> idx(n);
        for (size_t i = 0; i < idx.size(); i++) idx[i] = i;
        std::partial_sort(idx.begin(), idx.begin() + std::min(3, n), idx.end(),
                          [&result](size_t a, size_t b) {
                              return result.signals[a].confidence < result.signals[b].confidence;
                          });
        std::cout << "[llama-cpp] least confident:";
        for (int i = 0; i < std::min(3, n); i++) {
            const auto& s = result.signals[idx[i]];
            std::cout << " \"" << s.token_text << "\"(p=" << s.confidence
                      << " H=" << s.entropy << ")";
        }
        std::cout << std::endl;
    }

    return grpc::Status::OK;
}

grpc::Status LlamaCppLLMService::StreamChat(grpc::ServerContext* ctx,
                                             const protocol::LLMChatRequest* req,
                                             grpc::ServerWriter<protocol::LLMChatChunk>* writer) {
    auto& engine = plugin_->engine();

    if (!engine.is_loaded()) {
        return grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                            "no model loaded — set model_path in plugin config");
    }

    float temperature = req->temperature() > 0 ? req->temperature() : 0.7;
    int max_tokens = req->max_tokens() > 0 ? req->max_tokens() : 512;

    // Extract attachment context (same as Chat)
    std::string context;
    for (const auto& att : req->attachments()) {
        const auto& uri = att.data();
        if (uri.empty()) continue;
        auto comma = uri.find(',');
        if (comma == std::string::npos) continue;
        auto header = uri.substr(0, comma);
        auto payload = uri.substr(comma + 1);

        if (header.find("application/pdf") != std::string::npos) {
            auto bytes = base64_decode(payload);
            auto text = extract_pdf_text(bytes.data(), bytes.size());
            if (!text.empty()) {
                context += "[Document: " + att.filename() + "]\n" + text + "\n\n";
            }
        } else if (header.find("text/plain") != std::string::npos) {
            auto bytes = base64_decode(payload);
            std::string text(bytes.begin(), bytes.end());
            if (!text.empty()) {
                context += "[Document: " + att.filename() + "]\n" + text + "\n\n";
            }
        }
    }

    std::string user_prompt = context.empty()
        ? req->user_prompt()
        : context + req->user_prompt();

    // Stream tokens as they're generated
    auto result = engine.stream_chat(
        req->system_prompt(), user_prompt, temperature, max_tokens,
        [&](const std::string& token_text, const TokenSignal& sig) -> bool {
            protocol::LLMChatChunk chunk;
            chunk.set_token(token_text);
            chunk.set_done(false);
            chunk.set_model(engine.model_name());

            auto* signal = chunk.mutable_signal();
            signal->set_confidence(sig.confidence);
            signal->set_entropy(sig.entropy);
            signal->set_top_gap(sig.top_gap);
            for (const auto& cand : sig.top_k) {
                auto* tc = signal->add_top_k();
                tc->set_id(cand.id);
                tc->set_text(cand.text);
                tc->set_prob(cand.prob);
            }
            for (float p : sig.full_distribution) {
                signal->add_full_distribution(p);
            }

            return writer->Write(chunk);
        });

    // Final chunk with totals
    protocol::LLMChatChunk final_chunk;
    final_chunk.set_done(true);
    final_chunk.set_model(engine.model_name());
    final_chunk.set_prompt_tokens(result.prompt_tokens);
    final_chunk.set_completion_tokens(result.completion_tokens);
    final_chunk.set_total_tokens(result.prompt_tokens + result.completion_tokens);
    writer->Write(final_chunk);

    return grpc::Status::OK;
}
