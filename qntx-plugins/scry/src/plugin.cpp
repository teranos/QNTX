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

// --- ScryPlugin (DomainPluginService) ---

ScryPlugin::ScryPlugin() : renderer_(std::make_unique<MetalRenderer>()) {
    renderer_->setup();
}

ScryPlugin::~ScryPlugin() {
    if (pca_thread_.joinable()) pca_thread_.join();
}

grpc::Status ScryPlugin::Metadata(grpc::ServerContext* ctx,
                                       const protocol::Empty* req,
                                       protocol::MetadataResponse* resp) {
    resp->set_name("scry");
    resp->set_version(PLUGIN_VERSION);
    resp->set_description("Local LLM inference via llama.cpp with Metal acceleration");
    resp->set_author("teranos");
    resp->set_license("MIT");
    return grpc::Status::OK;
}

grpc::Status ScryPlugin::Initialize(grpc::ServerContext* ctx,
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
            std::cout << "[scry] Failed to load model: " << it->second << std::endl;
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

    std::cout << "[scry] Sampler config: top_k=" << sampler_cfg_.top_k
              << " top_p=" << sampler_cfg_.top_p
              << " min_p=" << sampler_cfg_.min_p
              << " typical_p=" << sampler_cfg_.typical_p
              << " penalty_last_n=" << sampler_cfg_.penalty_last_n
              << " repeat_penalty=" << sampler_cfg_.penalty_repeat << std::endl;

    // Retry renderer setup if it failed during construction (restart timing)
    if (!renderer_->is_ready()) {
        std::cout << "[metal-scry] Renderer not ready, retrying setup..." << std::endl;
        renderer_->setup();
    }

    // Set PCA positions on the renderer if model loaded, start render loop.
    // Positions load from disk cache (~1ms) or compute via PCA (~16s first run).
    if (engine_.is_loaded() && renderer_->is_ready()) {
        const auto& pos = engine_.vocab_positions_3d();
        if (!pos.empty()) {
            renderer_->set_vocab_positions(pos.data(), pos.size() / 6);
            renderer_->start_render_loop(800, 600);
            std::cout << "[metal-scry] Loaded " << pos.size() / 6
                      << " vocab positions+colors into renderer, render loop started" << std::endl;
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

grpc::Status ScryPlugin::Shutdown(grpc::ServerContext* ctx,
                                       const protocol::Empty* req,
                                       protocol::Empty* resp) {
    // Wait for background PCA to finish before unloading model
    if (pca_thread_.joinable()) pca_thread_.join();
    engine_.unload();
    std::cout << "[scry] Shutdown complete" << std::endl;
    return grpc::Status::OK;
}

grpc::Status ScryPlugin::Health(grpc::ServerContext* ctx,
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

grpc::Status ScryPlugin::ConfigSchema(grpc::ServerContext* ctx,
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

grpc::Status ScryPlugin::RegisterGlyphs(grpc::ServerContext* ctx,
                                              const protocol::Empty* req,
                                              protocol::GlyphDefResponse* resp) {
    // Nebula view is now part of the response glyph — no separate glyph needed.
    // The response glyph connects directly to this plugin's WebSocket for frames.
    return grpc::Status::OK;
}

grpc::Status ScryPlugin::HandleHTTP(grpc::ServerContext* ctx,
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
                renderer_->set_vocab_positions(pos.data(), pos.size() / 6);
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

grpc::Status ScryPlugin::HandleWebSocket(
    grpc::ServerContext* ctx,
    grpc::ServerReaderWriter<protocol::WebSocketMessage,
                             protocol::WebSocketMessage>* stream) {
    // gRPC ServerReaderWriter does not allow concurrent Write() calls.
    // The reader thread queues pong responses; the main loop drains them
    // alongside frame pushes, serializing all writes on one thread.
    std::atomic<bool> closed{false};
    auto* renderer = renderer_.get();
    auto* engine = &engine_;

    std::mutex pong_mutex;
    std::vector<protocol::WebSocketMessage> pong_queue;

    auto* plugin = this;
    std::thread reader([stream, &closed, renderer, engine, plugin, &pong_mutex, &pong_queue]() {
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
                } else if (data.size() > 6 && data.substr(0, 6) == "mouse:") {
                    // mouse:x,y — update cursor position for GPU pick
                    auto rest = data.substr(6);
                    auto comma = rest.find(',');
                    if (comma != std::string::npos) {
                        try {
                            int mx = std::stoi(rest.substr(0, comma));
                            int my = std::stoi(rest.substr(comma + 1));
                            renderer->set_mouse(mx, my);
                        } catch (...) {}
                    }
                } else if (data.size() > 8 && data.substr(0, 8) == "examine:") {
                    renderer->set_token_examine(data.substr(8) == "1");
                } else if (data.size() > 4 && data.substr(0, 4) == "nav:") {
                    auto cmd = data.substr(4);
                    int dir = 0;
                    if (cmd == ".") dir = 1;
                    else if (cmd == ",") dir = -1;
                    if (dir != 0) {
                        int token_id = renderer->step_candidate(dir);
                        if (token_id >= 0) {
                            auto text = engine->token_text(token_id);
                            renderer->set_hover_label(text);
                            // Queue picked response for JS span highlight
                            std::string pick_resp = "picked:" + std::to_string(token_id) + "," + text;
                            protocol::WebSocketMessage pick_msg;
                            pick_msg.set_type(protocol::WebSocketMessage::DATA);
                            pick_msg.set_data(pick_resp);
                            std::lock_guard<std::mutex> lock(pong_mutex);
                            pong_queue.push_back(pick_msg);
                        }
                    }
                } else if (data.size() > 5 && data.substr(0, 5) == "fork:") {
                    // Fork: pick an alternative token and generate from that point.
                    // Format: fork:token_id
                    // Uses current scrub_index as fork position, active branch as parent.
                    try {
                        int fork_token_id = std::stoi(data.substr(5));

                        // Detach fork generation to avoid blocking the WS reader
                        std::thread([plugin, renderer, engine, fork_token_id,
                                     &pong_mutex, &pong_queue, &closed]() {
                            std::lock_guard<std::mutex> flock(plugin->fork_mutex());
                            auto& tree = plugin->fork_tree();

                            // Determine fork position from scrub index
                            int fork_pos = renderer->branch_count() > 0
                                ? (int)(renderer->token_probability(fork_token_id) >= 0 ? 0 : 0)
                                : 0;
                            // The scrub index tells us where in the sequence to fork
                            // We need it from the renderer's scrub state
                            // For now, use the active branch's token count as fork position
                            int active_id = tree.active_branch;
                            if (active_id >= 0 && active_id < (int)tree.branches.size()) {
                                fork_pos = tree.branches[active_id].tokens.size();
                            }

                            // Allocate new sequence and branch
                            int new_seq = tree.next_seq_id++;
                            int parent_seq = (active_id >= 0 && active_id < (int)tree.branches.size())
                                ? tree.branches[active_id].seq_id : 0;

                            // Create renderer branch (copies parent trail up to fork point)
                            int render_branch_id = renderer->add_fork_branch(active_id, fork_pos);
                            renderer->set_active_branch(render_branch_id);

                            // Create data model branch
                            ForkBranch branch;
                            branch.id = (int)tree.branches.size();
                            branch.parent_id = active_id;
                            branch.fork_position = fork_pos;
                            branch.seq_id = new_seq;
                            branch.fork_token = fork_token_id;
                            branch.orbit_phase = (active_id >= 0 && active_id < (int)tree.branches.size())
                                ? tree.branches[active_id].orbit_phase + (float)M_PI / 2.0f : (float)M_PI / 2.0f;
                            branch.active = true;
                            tree.branches.push_back(branch);

                            int branch_id = branch.id;
                            tree.active_branch = branch_id;

                            // Absolute position = prompt tokens + fork position
                            int fork_pos_absolute = tree.prompt_token_count + fork_pos;

                            // Send forked: response
                            {
                                std::string resp = "forked:" + std::to_string(branch_id);
                                protocol::WebSocketMessage msg;
                                msg.set_type(protocol::WebSocketMessage::DATA);
                                msg.set_data(resp);
                                std::lock_guard<std::mutex> lock(pong_mutex);
                                pong_queue.push_back(msg);
                            }

                            plugin->set_activity("forking (branch " + std::to_string(branch_id) + ")");

                            // Generate on the fork
                            auto result = engine->fork_and_generate(
                                parent_seq, fork_pos_absolute,
                                fork_token_id, new_seq,
                                0.7f, 256,
                                [&](const std::string& token_text, const TokenSignal& sig) -> bool {
                                    if (closed.load()) return false;

                                    // Feed renderer with fork branch data
                                    if (renderer->is_ready() && !sig.full_distribution.empty()) {
                                        renderer->submit_distribution(
                                            sig.full_distribution.data(), sig.full_distribution.size());
                                        renderer->store_keyframe(
                                            sig.full_distribution.data(), sig.full_distribution.size(),
                                            render_branch_id);
                                        renderer->add_trail_point(sig.token_id, render_branch_id);

                                        if (sig.top_k.size() > 1) {
                                            std::vector<std::pair<int,float>> runners;
                                            for (size_t i = 0; i < sig.top_k.size() && runners.size() < 10; i++) {
                                                if (sig.top_k[i].id != sig.token_id) {
                                                    runners.emplace_back(sig.top_k[i].id, sig.top_k[i].prob);
                                                }
                                            }
                                            renderer->add_ghost_branches(sig.token_id, runners, render_branch_id);
                                        }
                                    }

                                    // Stream fork token to frontend
                                    std::string tok_resp = "fork_token:" + sanitize_utf8(token_text);
                                    protocol::WebSocketMessage msg;
                                    msg.set_type(protocol::WebSocketMessage::DATA);
                                    msg.set_data(tok_resp);
                                    std::lock_guard<std::mutex> lock(pong_mutex);
                                    pong_queue.push_back(msg);

                                    // Record token in branch
                                    {
                                        std::lock_guard<std::mutex> flock2(plugin->fork_mutex());
                                        auto& b = plugin->fork_tree().branches[branch_id];
                                        b.tokens.push_back((int32_t)sig.token_id);
                                    }

                                    return !closed.load();
                                },
                                plugin->sampler_config());

                            plugin->set_activity("");

                            std::cout << "[scry] Fork branch " << branch_id
                                      << " complete: " << result.completion_tokens
                                      << " tokens" << std::endl;

                            // Send fork_done to frontend
                            {
                                std::string done_resp = "fork_done:" + std::to_string(branch_id);
                                protocol::WebSocketMessage msg;
                                msg.set_type(protocol::WebSocketMessage::DATA);
                                msg.set_data(done_resp);
                                std::lock_guard<std::mutex> lock(pong_mutex);
                                pong_queue.push_back(msg);
                            }
                        }).detach();
                    } catch (...) {}
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

        // Check for debounced pick result (400ms idle)
        int pick_id = renderer_->consume_pick_result();
        if (pick_id >= 0) {
            auto token_text = engine_.token_text(pick_id);

            // Send to JS for span highlighting
            std::string pick_resp = "picked:" + std::to_string(pick_id) + "," + token_text;
            protocol::WebSocketMessage pick_msg;
            pick_msg.set_type(protocol::WebSocketMessage::DATA);
            pick_msg.set_data(pick_resp);
            if (!stream->Write(pick_msg)) break;

            // Set Metal-rendered label
            renderer_->set_hover_label(token_text);
        }

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

grpc::Status ScryPlugin::ExecuteJob(grpc::ServerContext* ctx,
                                         const protocol::ExecuteJobRequest* req,
                                         protocol::ExecuteJobResponse* resp) {
    resp->set_success(false);
    resp->set_error("scry does not handle async jobs");
    return grpc::Status::OK;
}

grpc::Status ScryPlugin::ParseAxQuery(grpc::ServerContext* ctx,
                                           const protocol::ParseAxQueryRequest* req,
                                           protocol::ParseAxQueryResponse* resp) {
    resp->set_error("scry does not parse Ax queries");
    return grpc::Status::OK;
}

