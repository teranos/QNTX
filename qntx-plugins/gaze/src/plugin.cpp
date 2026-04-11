#include "plugin.h"
#include "log_capture.h"

#include <iostream>

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

// --- GazePlugin (DomainPluginService) ---

grpc::Status GazePlugin::Metadata(grpc::ServerContext* ctx,
                                   const protocol::Empty* req,
                                   protocol::MetadataResponse* resp) {
    resp->set_name("gaze");
    resp->set_version(PLUGIN_VERSION);
    resp->set_description("Production LLM inference via llama.cpp with Metal acceleration");
    resp->set_author("teranos");
    resp->set_license("MIT");
    return grpc::Status::OK;
}

grpc::Status GazePlugin::Initialize(grpc::ServerContext* ctx,
                                     const protocol::InitializeRequest* req,
                                     protocol::InitializeResponse* resp) {
    auto config = req->config();

    int n_ctx = 2048;
    auto ctx_it = config.find("n_ctx");
    if (ctx_it != config.end()) {
        try { n_ctx = std::stoi(ctx_it->second); } catch (...) {}
        if (n_ctx <= 0) n_ctx = 2048;
    }

    // Collect model paths: "models" JSON array or legacy "model_path" single entry
    std::vector<std::string> model_paths;
    auto models_it = config.find("models");
    if (models_it != config.end() && !models_it->second.empty()) {
        // Core serializes TOML arrays as JSON: ["/path/a.gguf","/path/b.gguf"]
        const std::string& json = models_it->second;
        // Minimal JSON array parser — extract quoted strings
        size_t pos = 0;
        while ((pos = json.find('"', pos)) != std::string::npos) {
            size_t start = pos + 1;
            size_t end = json.find('"', start);
            if (end == std::string::npos) break;
            std::string path = json.substr(start, end - start);
            if (!path.empty()) model_paths.push_back(path);
            pos = end + 1;
        }
    }
    if (model_paths.empty()) {
        auto it = config.find("model_path");
        if (it != config.end() && !it->second.empty()) {
            model_paths.push_back(it->second);
        }
    }

    for (const auto& path : model_paths) {
        auto engine = std::make_unique<InferenceEngine>();
        if (engine->load_model(path, n_ctx)) {
            std::string name = engine->model_name();
            engines_[name] = std::move(engine);
        } else {
            std::cout << "[gaze] Failed to load model: " << path << std::endl;
        }
    }

    // Parse sampler config
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

    std::cout << "[gaze] Sampler config: top_k=" << sampler_cfg_.top_k
              << " top_p=" << sampler_cfg_.top_p
              << " min_p=" << sampler_cfg_.min_p
              << " typical_p=" << sampler_cfg_.typical_p
              << " penalty_last_n=" << sampler_cfg_.penalty_last_n
              << " repeat_penalty=" << sampler_cfg_.penalty_repeat << std::endl;

    // Flush condensed log summary
    auto log_it = config.find("log_level");
    std::string log_level = (log_it != config.end()) ? log_it->second : "info";
    LogCapture::instance().flush_summary(log_level);

    resp->set_llm_provider(true);

    return grpc::Status::OK;
}

grpc::Status GazePlugin::Shutdown(grpc::ServerContext* ctx,
                                   const protocol::Empty* req,
                                   protocol::Empty* resp) {
    engines_.clear();
    std::cout << "[gaze] Shutdown complete" << std::endl;
    return grpc::Status::OK;
}

InferenceEngine* GazePlugin::get_engine(const std::string& model_name) {
    auto it = engines_.find(model_name);
    if (it != engines_.end()) return it->second.get();
    return nullptr;
}

InferenceEngine* GazePlugin::default_engine() {
    if (engines_.empty()) return nullptr;
    return engines_.begin()->second.get();
}

std::vector<std::string> GazePlugin::model_names() const {
    std::vector<std::string> names;
    names.reserve(engines_.size());
    for (const auto& [name, _] : engines_) {
        names.push_back(name);
    }
    return names;
}

grpc::Status GazePlugin::Health(grpc::ServerContext* ctx,
                                 const protocol::Empty* req,
                                 protocol::HealthResponse* resp) {
    resp->set_healthy(true);
    if (engines_.empty()) {
        resp->set_message("no models loaded");
    } else {
        std::string msg = std::to_string(engines_.size()) + " model(s): ";
        bool first = true;
        for (const auto& [name, _] : engines_) {
            if (!first) msg += ", ";
            msg += name;
            first = false;
        }
        resp->set_message(msg);
    }
    return grpc::Status::OK;
}

grpc::Status GazePlugin::ConfigSchema(grpc::ServerContext* ctx,
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

grpc::Status GazePlugin::RegisterGlyphs(grpc::ServerContext* ctx,
                                          const protocol::Empty* req,
                                          protocol::GlyphDefResponse* resp) {
    return grpc::Status::OK;
}

grpc::Status GazePlugin::HandleHTTP(grpc::ServerContext* ctx,
                                     const protocol::HTTPRequest* req,
                                     protocol::HTTPResponse* resp) {
    if (req->method() == "GET" && req->path() == "/version") {
        resp->set_status_code(200);
        resp->set_body(PLUGIN_VERSION);
        return grpc::Status::OK;
    }

    if (req->method() == "GET" && req->path() == "/status") {
        std::string state = engines_.empty() ? "no_model" : "ready";
        std::string act = activity();
        std::string body = "{\"state\":\"" + state + "\",\"version\":\"" + PLUGIN_VERSION + "\"";
        body += ",\"models\":" + std::to_string(engines_.size());
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

grpc::Status GazePlugin::HandleWebSocket(
    grpc::ServerContext* ctx,
    grpc::ServerReaderWriter<protocol::WebSocketMessage,
                             protocol::WebSocketMessage>* stream) {
    return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "gaze has no WebSocket interface");
}

grpc::Status GazePlugin::ExecuteJob(grpc::ServerContext* ctx,
                                     const protocol::ExecuteJobRequest* req,
                                     protocol::ExecuteJobResponse* resp) {
    resp->set_success(false);
    resp->set_error("gaze does not handle async jobs");
    return grpc::Status::OK;
}

grpc::Status GazePlugin::ParseAxQuery(grpc::ServerContext* ctx,
                                       const protocol::ParseAxQueryRequest* req,
                                       protocol::ParseAxQueryResponse* resp) {
    resp->set_error("gaze does not parse Ax queries");
    return grpc::Status::OK;
}
