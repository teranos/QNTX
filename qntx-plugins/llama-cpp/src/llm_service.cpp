#include "plugin.h"
#include "base64.h"
#include "metal_renderer.h"
#include "pdf_extract.h"

#include <algorithm>
#include <chrono>
#include <iostream>
#include <vector>

// Defined in plugin.cpp
std::string sanitize_utf8(const std::string& s);

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

    // Process attachments: separate images (for vision) from documents (text extraction).
    std::string context;
    std::vector<InferenceEngine::ImageAttachment> image_attachments;

    for (const auto& att : req->attachments()) {
        const auto& data = att.data();
        const auto& mime = att.mime_type();
        if (data.empty()) continue;

        // Check if this is an image attachment
        bool is_image = false;
        std::string payload;

        if (mime.find("image") != std::string::npos) {
            // Direct image: mime_type is "image/png" etc, data is raw base64
            is_image = true;
            // Data might be a data URI or raw base64
            auto comma = data.find(',');
            payload = (comma != std::string::npos) ? data.substr(comma + 1) : data;
        } else if (data.find("data:image") == 0) {
            // Data URI with image mime
            is_image = true;
            auto comma = data.find(',');
            if (comma != std::string::npos) payload = data.substr(comma + 1);
        }

        if (is_image) {
            if (!engine.has_vision()) {
                return grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                    "image attachment received but model has no vision support");
            }
            auto bytes = base64_decode(payload);
            image_attachments.push_back({std::move(bytes)});
            continue;
        }

        // Text/document extraction (existing path)
        auto comma = data.find(',');
        if (comma == std::string::npos) continue;
        auto header = data.substr(0, comma);
        auto doc_payload = data.substr(comma + 1);

        if (header.find("application/pdf") != std::string::npos) {
            auto bytes = base64_decode(doc_payload);
            auto text = extract_pdf_text(bytes.data(), bytes.size());
            if (!text.empty()) {
                context += "[Document: " + att.filename() + "]\n" + text + "\n\n";
            }
        } else if (header.find("text/plain") != std::string::npos) {
            auto bytes = base64_decode(doc_payload);
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

    // Route to vision or text path
    InferenceEngine::ChatResult result;
    if (!image_attachments.empty()) {
        result = engine.chat_vision(messages, image_attachments,
                                     temperature, max_tokens, plugin_->sampler_config());
    } else {
        result = engine.chat(messages, temperature, max_tokens, plugin_->sampler_config());
    }

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

    // Process attachments: separate images (for vision) from documents (text extraction)
    std::string context;
    std::vector<InferenceEngine::ImageAttachment> image_attachments;

    for (const auto& att : req->attachments()) {
        const auto& data = att.data();
        const auto& mime = att.mime_type();
        if (data.empty()) continue;

        bool is_image = false;
        std::string payload;

        if (mime.find("image") != std::string::npos) {
            is_image = true;
            auto comma = data.find(',');
            payload = (comma != std::string::npos) ? data.substr(comma + 1) : data;
        } else if (data.find("data:image") == 0) {
            is_image = true;
            auto comma = data.find(',');
            if (comma != std::string::npos) payload = data.substr(comma + 1);
        }

        if (is_image) {
            if (!engine.has_vision()) {
                return grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                    "image attachment received but model has no vision support");
            }
            auto bytes = base64_decode(payload);
            image_attachments.push_back({std::move(bytes)});
            continue;
        }

        auto comma = data.find(',');
        if (comma == std::string::npos) continue;
        auto header = data.substr(0, comma);
        auto doc_payload = data.substr(comma + 1);

        if (header.find("application/pdf") != std::string::npos) {
            auto bytes = base64_decode(doc_payload);
            auto text = extract_pdf_text(bytes.data(), bytes.size());
            if (!text.empty()) {
                context += "[Document: " + att.filename() + "]\n" + text + "\n\n";
            }
        } else if (header.find("text/plain") != std::string::npos) {
            auto bytes = base64_decode(doc_payload);
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

    // Route to vision or text streaming path
    auto token_callback = [&](const std::string& token_text, const TokenSignal& sig) -> bool {
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
    };

    InferenceEngine::ChatResult result;
    if (!image_attachments.empty()) {
        result = engine.stream_chat_vision(
            messages, image_attachments, temperature, max_tokens,
            token_callback, plugin_->sampler_config());
    } else {
        result = engine.stream_chat(
            messages, temperature, max_tokens,
            token_callback, plugin_->sampler_config());
    }

    // Send truncation warning as a visible token so the UI shows it
    if (!result.warning.empty()) {
        protocol::LLMChatChunk warn_chunk;
        warn_chunk.set_token("\n\n⚠ " + result.warning);
        warn_chunk.set_done(false);
        warn_chunk.set_model(engine.model_name());
        writer->Write(warn_chunk);
    }

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
