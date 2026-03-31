#include "plugin.h"
#include "base64.h"
#include "log_capture.h"
#include "metal_renderer.h"
#include "pdf_extract.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <iostream>
#include <thread>
#include <vector>

// BPE tokens for multi-byte scripts (Hindi, CJK, emoji) can be partial UTF-8
// sequences. Protobuf string fields reject invalid UTF-8, so replace bad bytes
// with U+FFFD before serializing.
std::string sanitize_utf8(const std::string& s) {
    std::string out;
    out.reserve(s.size());
    size_t i = 0;
    while (i < s.size()) {
        unsigned char c = s[i];
        int len = 0;
        if (c < 0x80) len = 1;
        else if ((c >> 5) == 0x06) len = 2;
        else if ((c >> 4) == 0x0E) len = 3;
        else if ((c >> 3) == 0x1E) len = 4;

        if (len == 0 || i + len > s.size()) {
            out += "\xEF\xBF\xBD";  // U+FFFD
            i++;
            continue;
        }
        // Validate continuation bytes
        bool valid = true;
        for (int j = 1; j < len; j++) {
            if ((s[i + j] & 0xC0) != 0x80) { valid = false; break; }
        }
        if (valid) {
            out.append(s, i, len);
            i += len;
        } else {
            out += "\xEF\xBF\xBD";
            i++;
        }
    }
    return out;
}

// --- LlamaCppPlugin (DomainPluginService) ---

LlamaCppPlugin::LlamaCppPlugin() : renderer_(std::make_unique<MetalRenderer>()) {
    renderer_->setup();
}

LlamaCppPlugin::~LlamaCppPlugin() {
    if (pca_thread_.joinable()) pca_thread_.join();
}

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

    // Parse sampler config from plugin config
    auto parse_int = [&config](const char* key, int fallback) -> int {
        auto it = config.find(key);
        if (it != config.end()) { try { return std::stoi(it->second); } catch (...) {} }
        return fallback;
    };
    auto parse_float = [&config](const char* key, float fallback) -> float {
        auto it = config.find(key);
        if (it != config.end()) { try { return std::stof(it->second); } catch (...) {} }
        return fallback;
    };
    sampler_cfg_.top_k = parse_int("top_k", 0);
    sampler_cfg_.top_p = parse_float("top_p", 1.0f);
    sampler_cfg_.min_p = parse_float("min_p", 0.0f);
    sampler_cfg_.typical_p = parse_float("typical_p", 1.0f);
    sampler_cfg_.penalty_last_n = parse_int("penalty_last_n", 0);
    sampler_cfg_.penalty_repeat = parse_float("repeat_penalty", 1.0f);
    sampler_cfg_.penalty_freq = parse_float("freq_penalty", 0.0f);
    sampler_cfg_.penalty_present = parse_float("presence_penalty", 0.0f);

    std::cout << "[llama-cpp] Sampler config: top_k=" << sampler_cfg_.top_k
              << " top_p=" << sampler_cfg_.top_p
              << " min_p=" << sampler_cfg_.min_p
              << " typical_p=" << sampler_cfg_.typical_p
              << " penalty_last_n=" << sampler_cfg_.penalty_last_n
              << " repeat_penalty=" << sampler_cfg_.penalty_repeat << std::endl;

    // Retry renderer setup if it failed during construction (restart timing)
    if (!renderer_->is_ready()) {
        std::cout << "[metal-llama] Renderer not ready, retrying setup..." << std::endl;
        renderer_->setup();
    }

    // Set PCA positions on the renderer if model loaded, start render loop.
    // Positions load from disk cache (~1ms) or compute via PCA (~16s first run).
    if (engine_.is_loaded() && renderer_->is_ready()) {
        const auto& pos = engine_.vocab_positions_3d();
        if (!pos.empty()) {
            renderer_->set_vocab_positions(pos.data(), pos.size() / 3);
            renderer_->start_render_loop(800, 600);
            std::cout << "[metal-llama] Loaded " << pos.size() / 3
                      << " vocab positions into renderer, render loop started" << std::endl;
        }
    }
    pca_ready_.store(true, std::memory_order_release);

    // Configure ATS client for writing attestations
    if (!req->ats_store_endpoint().empty()) {
        ats_client_.configure(req->ats_store_endpoint(), req->auth_token());
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
    // Wait for background PCA to finish before unloading model
    if (pca_thread_.joinable()) pca_thread_.join();
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

    // Sampler chain configuration
    protocol::ConfigFieldSchema top_k_field;
    top_k_field.set_type("number");
    top_k_field.set_description("Top-K sampling: keep only the top K tokens (0 = disabled)");
    top_k_field.set_default_value("0");
    top_k_field.set_min_value("0");
    top_k_field.set_max_value("500");
    (*fields)["top_k"] = top_k_field;

    protocol::ConfigFieldSchema top_p_field;
    top_p_field.set_type("number");
    top_p_field.set_description("Top-P (nucleus) sampling: cumulative probability cutoff (1.0 = disabled)");
    top_p_field.set_default_value("1.0");
    top_p_field.set_min_value("0.0");
    top_p_field.set_max_value("1.0");
    (*fields)["top_p"] = top_p_field;

    protocol::ConfigFieldSchema min_p_field;
    min_p_field.set_type("number");
    min_p_field.set_description("Min-P sampling: drop tokens below this fraction of top token (0.0 = disabled)");
    min_p_field.set_default_value("0.0");
    min_p_field.set_min_value("0.0");
    min_p_field.set_max_value("1.0");
    (*fields)["min_p"] = min_p_field;

    protocol::ConfigFieldSchema typical_p_field;
    typical_p_field.set_type("number");
    typical_p_field.set_description("Typical-P sampling: locally typical sampling threshold (1.0 = disabled)");
    typical_p_field.set_default_value("1.0");
    typical_p_field.set_min_value("0.0");
    typical_p_field.set_max_value("1.0");
    (*fields)["typical_p"] = typical_p_field;

    protocol::ConfigFieldSchema repeat_penalty_field;
    repeat_penalty_field.set_type("number");
    repeat_penalty_field.set_description("Repetition penalty (1.0 = disabled)");
    repeat_penalty_field.set_default_value("1.0");
    repeat_penalty_field.set_min_value("0.0");
    repeat_penalty_field.set_max_value("3.0");
    (*fields)["repeat_penalty"] = repeat_penalty_field;

    protocol::ConfigFieldSchema penalty_last_n_field;
    penalty_last_n_field.set_type("number");
    penalty_last_n_field.set_description("Penalty window: number of recent tokens to penalize (0 = disabled)");
    penalty_last_n_field.set_default_value("0");
    penalty_last_n_field.set_min_value("0");
    penalty_last_n_field.set_max_value("2048");
    (*fields)["penalty_last_n"] = penalty_last_n_field;

    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::RegisterGlyphs(grpc::ServerContext* ctx,
                                              const protocol::Empty* req,
                                              protocol::GlyphDefResponse* resp) {
    // Nebula view is now part of the response glyph — no separate glyph needed.
    // The response glyph connects directly to this plugin's WebSocket for frames.
    return grpc::Status::OK;
}

grpc::Status LlamaCppPlugin::HandleHTTP(grpc::ServerContext* ctx,
                                         const protocol::HTTPRequest* req,
                                         protocol::HTTPResponse* resp) {
    if (req->method() == "GET" && req->path() == "/render-latest") {
        int w = 0, h = 0;
        auto png = renderer_->get_latest_frame(w, h);
        if (png.empty()) {
            resp->set_status_code(404);
            resp->set_body("{\"error\":\"no frame rendered yet\"}");
            auto* hdr = resp->add_headers();
            hdr->set_name("Content-Type");
            hdr->add_values("application/json");
            return grpc::Status::OK;
        }
        resp->set_status_code(200);
        resp->set_body(png.data(), png.size());
        auto* ct = resp->add_headers();
        ct->set_name("Content-Type");
        ct->add_values("image/png");
        return grpc::Status::OK;
    }

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
        ct->add_values("image/png");
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

    if (req->method() == "GET" && req->path() == "/version") {
        resp->set_status_code(200);
        resp->set_body(PLUGIN_VERSION);
        return grpc::Status::OK;
    }

    if (req->method() == "GET" && req->path() == "/status") {
        std::string state;
        if (!engine_.is_loaded()) {
            state = "no_model";
        } else if (!pca_ready()) {
            state = "computing_positions";
        } else {
            state = "ready";
        }
        std::string act = activity();
        std::string body = "{\"state\":\"" + state + "\",\"version\":\"" + PLUGIN_VERSION + "\"";
        if (!act.empty()) body += ",\"activity\":\"" + act + "\"";
        body += "}";
        resp->set_status_code(200);
        resp->set_body(body);
        auto* ct = resp->add_headers();
        ct->set_name("Content-Type");
        ct->add_values("application/json");
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
    // gRPC ServerReaderWriter does not allow concurrent Write() calls.
    // The reader thread queues pong responses; the main loop drains them
    // alongside frame pushes, serializing all writes on one thread.
    std::atomic<bool> closed{false};
    auto* renderer = renderer_.get();

    std::mutex pong_mutex;
    std::vector<protocol::WebSocketMessage> pong_queue;

    std::thread reader([stream, &closed, renderer, &pong_mutex, &pong_queue]() {
        protocol::WebSocketMessage in_msg;
        while (stream->Read(&in_msg)) {
            if (in_msg.type() == protocol::WebSocketMessage::PING) {
                protocol::WebSocketMessage pong;
                pong.set_type(protocol::WebSocketMessage::PONG);
                pong.set_timestamp(in_msg.timestamp());
                std::lock_guard<std::mutex> lock(pong_mutex);
                pong_queue.push_back(std::move(pong));
            } else if (in_msg.type() == protocol::WebSocketMessage::CLOSE) {
                closed.store(true);
                return;
            } else if (in_msg.type() == protocol::WebSocketMessage::DATA) {
                // Parse scrub commands: "scrub:N" where N is keyframe index (-1 = live)
                const auto& data = in_msg.data();
                if (data.size() > 6 && data.substr(0, 6) == "scrub:") {
                    try {
                        int idx = std::stoi(data.substr(6));
                        renderer->set_scrub_index(idx);
                    } catch (...) {}
                } else if (data == "cam:r") {
                    renderer->reset_camera();
                } else if (data.size() > 4 && data.substr(0, 4) == "cam:") {
                    // cam:dx,dy,dz,dyaw,dpitch
                    auto rest = data.substr(4);
                    // Split on commas
                    float vals[5] = {0, 0, 1, 0, 0};
                    int vi = 0;
                    size_t pos = 0;
                    while (vi < 5 && pos < rest.size()) {
                        auto next = rest.find(',', pos);
                        try {
                            vals[vi] = std::stof(rest.substr(pos, next - pos));
                        } catch (...) {}
                        vi++;
                        if (next == std::string::npos) break;
                        pos = next + 1;
                    }
                    renderer->apply_camera(vals[0], vals[1], vals[2], vals[3], vals[4]);
                } else if (data.size() > 6 && data.substr(0, 6) == "param:") {
                    // param:key:value — adjust renderer parameters at runtime
                    auto rest = data.substr(6);
                    auto sep = rest.find(':');
                    if (sep != std::string::npos) {
                        try {
                            auto key = rest.substr(0, sep);
                            float val = std::stof(rest.substr(sep + 1));
                            renderer->set_param(key, val);
                        } catch (...) {}
                    }
                }
            }
        }
        closed.store(true);
    });

    // Push nebula frames and drain pong queue — all writes on this thread
    while (!ctx->IsCancelled() && !closed.load()) {
        // Drain queued pongs
        {
            std::lock_guard<std::mutex> lock(pong_mutex);
            for (auto& pong : pong_queue) {
                if (!stream->Write(pong)) { closed.store(true); break; }
            }
            pong_queue.clear();
        }
        if (closed.load()) break;

        auto png = renderer_->wait_for_frame(100);
        if (png.empty()) continue;

        protocol::WebSocketMessage msg;
        msg.set_type(protocol::WebSocketMessage::DATA);
        msg.set_data(png.data(), png.size());
        if (!stream->Write(msg)) break;
    }

    if (reader.joinable()) reader.join();
    return grpc::Status::OK;
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

    // Build message history from proto
    std::vector<InferenceEngine::Message> messages;
    if (req->messages_size() > 0) {
        // Multi-turn: use messages array from proto
        for (const auto& m : req->messages()) {
            messages.push_back({m.role(), m.content()});
        }
        // Prepend attachment context to the last user message
        if (!context.empty()) {
            for (int i = messages.size() - 1; i >= 0; i--) {
                if (messages[i].role == "user") {
                    messages[i].content = context + messages[i].content;
                    break;
                }
            }
        }
    } else {
        // Single-turn fallback (deprecated fields)
        std::string user_prompt = context.empty()
            ? req->user_prompt()
            : context + req->user_prompt();
        if (!req->system_prompt().empty()) {
            messages.push_back({"system", req->system_prompt()});
        }
        messages.push_back({"user", user_prompt});
    }

    auto result = engine.chat(messages, temperature, max_tokens, plugin_->sampler_config());

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

        // Submit the last token's distribution for interpolated rendering
        const auto& last_sig = result.signals.back();
        if (plugin_->renderer().is_ready() && !last_sig.full_distribution.empty()) {
            plugin_->renderer().submit_distribution(
                last_sig.full_distribution.data(), last_sig.full_distribution.size());
        }

        // Write weave attestation to ATS (token signals packed in attributes)
        auto& ats = plugin_->ats_client();
        if (ats.is_configured()) {
            std::string context_id = "chat:" + std::to_string(
                std::chrono::system_clock::now().time_since_epoch().count());

            // Extract last user message for weave attestation
            std::string prompt_text;
            for (auto it = messages.rbegin(); it != messages.rend(); ++it) {
                if (it->role == "user") { prompt_text = it->content; break; }
            }
            GenerationPerf perf;
            perf.prompt_eval_ms = result.prompt_eval_ms;
            perf.generation_ms = result.generation_ms;
            perf.decode_ms = result.decode_ms;
            perf.signal_ms = result.signal_ms;
            perf.callback_ms = result.callback_ms;
            perf.completion_tokens = result.completion_tokens;
            ats.create_weave(engine.model_name(), prompt_text,
                             result.content, context_id, n,
                             conf_sum / n, ent_sum / n, result.signals, perf);
        }
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

    // Build message history from proto
    std::vector<InferenceEngine::Message> messages;
    if (req->messages_size() > 0) {
        for (const auto& m : req->messages()) {
            messages.push_back({m.role(), m.content()});
        }
        if (!context.empty()) {
            for (int i = messages.size() - 1; i >= 0; i--) {
                if (messages[i].role == "user") {
                    messages[i].content = context + messages[i].content;
                    break;
                }
            }
        }
    } else {
        std::string user_prompt = context.empty()
            ? req->user_prompt()
            : context + req->user_prompt();
        if (!req->system_prompt().empty()) {
            messages.push_back({"system", req->system_prompt()});
        }
        messages.push_back({"user", user_prompt});
    }

    // Clear trail and keyframe history for new generation
    if (plugin_->renderer().is_ready()) {
        plugin_->renderer().clear_trail();
        plugin_->renderer().set_scrub_index(-1);
    }

    plugin_->set_activity("evaluating prompt");

    // Stream tokens as they're generated
    auto result = engine.stream_chat(
        messages, temperature, max_tokens,
        [&](const std::string& token_text, const TokenSignal& sig) -> bool {
            plugin_->set_activity("generating");

            protocol::LLMChatChunk chunk;
            chunk.set_token(sanitize_utf8(token_text));
            chunk.set_done(false);
            chunk.set_model(engine.model_name());

            auto* signal = chunk.mutable_signal();
            signal->set_confidence(sig.confidence);
            signal->set_entropy(sig.entropy);
            signal->set_top_gap(sig.top_gap);
            for (const auto& cand : sig.top_k) {
                auto* tc = signal->add_top_k();
                tc->set_id(cand.id);
                tc->set_text(sanitize_utf8(cand.text));
                tc->set_prob(cand.prob);
            }
            for (const auto& stage : sig.sampler_stages) {
                auto* sp = signal->add_sampler_stages();
                sp->set_name(stage.stage_name);
                sp->set_active_count(stage.active_count);
                sp->set_top1_prob(stage.top1_prob);
                sp->set_entropy(stage.entropy);
                for (const auto& cand : stage.top_k) {
                    auto* tc = sp->add_top_k();
                    tc->set_id(cand.id);
                    tc->set_text(sanitize_utf8(cand.text));
                    tc->set_prob(cand.prob);
                }
            }

            // Submit distribution for interpolated rendering + record trail + store keyframe
            if (plugin_->renderer().is_ready() && !sig.full_distribution.empty()) {
                plugin_->renderer().submit_distribution(
                    sig.full_distribution.data(), sig.full_distribution.size());
                plugin_->renderer().store_keyframe(
                    sig.full_distribution.data(), sig.full_distribution.size());
                plugin_->renderer().add_trail_point(sig.token_id);

                if (sig.top_k.size() > 1) {
                    std::vector<std::pair<int,float>> runners;
                    for (size_t i = 0; i < sig.top_k.size() && runners.size() < 10; i++) {
                        if (sig.top_k[i].id != sig.token_id) {
                            runners.emplace_back(sig.top_k[i].id, sig.top_k[i].prob);
                        }
                    }
                    plugin_->renderer().add_ghost_branches(sig.token_id, runners);
                }
            }

            return writer->Write(chunk);
        },
        plugin_->sampler_config());

    // Final chunk with totals
    protocol::LLMChatChunk final_chunk;
    final_chunk.set_done(true);
    final_chunk.set_model(engine.model_name());
    final_chunk.set_prompt_tokens(result.prompt_tokens);
    final_chunk.set_completion_tokens(result.completion_tokens);
    final_chunk.set_total_tokens(result.prompt_tokens + result.completion_tokens);
    writer->Write(final_chunk);
    plugin_->set_activity("");

    // Write weave attestation to ATS after stream completes (token signals packed in attributes)
    auto& ats = plugin_->ats_client();
    if (ats.is_configured() && !result.signals.empty()) {
        int n = result.signals.size();
        float ent_sum = 0, conf_sum = 0;
        for (const auto& sig : result.signals) {
            ent_sum += sig.entropy;
            conf_sum += sig.confidence;
        }

        std::string context_id = "stream:" + std::to_string(
            std::chrono::system_clock::now().time_since_epoch().count());

        std::string prompt_text;
        for (auto it = messages.rbegin(); it != messages.rend(); ++it) {
            if (it->role == "user") { prompt_text = it->content; break; }
        }
        GenerationPerf perf;
        perf.prompt_eval_ms = result.prompt_eval_ms;
        perf.generation_ms = result.generation_ms;
        perf.decode_ms = result.decode_ms;
        perf.signal_ms = result.signal_ms;
        perf.callback_ms = result.callback_ms;
        perf.completion_tokens = result.completion_tokens;
        ats.create_weave(engine.model_name(), prompt_text,
                         result.content, context_id, n,
                         conf_sum / n, ent_sum / n, result.signals, perf);
    }

    return grpc::Status::OK;
}
