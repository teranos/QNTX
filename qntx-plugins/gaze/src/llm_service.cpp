#include "plugin.h"
#include "base64.h"
#include "pdf_extract.h"

#include <iostream>
#include <vector>

// Defined in plugin.cpp
std::string sanitize_utf8(const std::string& s);

// --- GazeLLMService ---

GazeLLMService::GazeLLMService(GazePlugin* plugin)
    : plugin_(plugin) {}

// Parse attachments from a request into document context text and image attachments.
static void parse_attachments(
    const protocol::LLMChatRequest& req,
    InferenceEngine& engine,
    std::string& context,
    std::vector<InferenceEngine::ImageAttachment>& image_attachments,
    grpc::Status& err) {

    for (const auto& att : req.attachments()) {
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
                err = grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                    "image attachment received but model has no vision support");
                return;
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
}

// Build message history from a request, prepending document context to the last user message.
static std::vector<InferenceEngine::Message> build_messages(
    const protocol::LLMChatRequest& req,
    const std::string& context) {

    std::vector<InferenceEngine::Message> messages;
    if (req.messages_size() > 0) {
        for (const auto& m : req.messages()) {
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
            ? req.user_prompt()
            : context + req.user_prompt();
        if (!req.system_prompt().empty()) {
            messages.push_back({"system", req.system_prompt()});
        }
        messages.push_back({"user", user_prompt});
    }
    return messages;
}

grpc::Status GazeLLMService::Chat(grpc::ServerContext* ctx,
                                   const protocol::LLMChatRequest* req,
                                   protocol::LLMChatResponse* resp) {
    auto& engine = plugin_->engine();

    if (!engine.is_loaded()) {
        return grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                            "no model loaded — set model_path in plugin config");
    }

    float temperature = req->temperature() > 0 ? req->temperature() : 0.7;
    int max_tokens = req->max_tokens() > 0 ? req->max_tokens() : 512;

    std::string context;
    std::vector<InferenceEngine::ImageAttachment> image_attachments;
    grpc::Status att_err;
    parse_attachments(*req, engine, context, image_attachments, att_err);
    if (!att_err.ok()) return att_err;

    auto messages = build_messages(*req, context);

    // Use streaming path with a no-op callback (simplest way to get a complete response)
    std::string full_response;
    auto collect = [&full_response](const std::string& token) -> bool {
        full_response += token;
        return true;
    };

    InferenceEngine::ChatResult result;
    if (!image_attachments.empty()) {
        result = engine.stream_chat_vision(
            messages, image_attachments, temperature, max_tokens,
            collect, plugin_->sampler_config());
    } else {
        result = engine.stream_chat(
            messages, temperature, max_tokens,
            collect, plugin_->sampler_config());
    }

    resp->set_content(result.content);
    resp->set_model(engine.model_name());
    resp->set_prompt_tokens(result.prompt_tokens);
    resp->set_completion_tokens(result.completion_tokens);
    resp->set_total_tokens(result.prompt_tokens + result.completion_tokens);

    return grpc::Status::OK;
}

grpc::Status GazeLLMService::StreamChat(grpc::ServerContext* ctx,
                                         const protocol::LLMChatRequest* req,
                                         grpc::ServerWriter<protocol::LLMChatChunk>* writer) {
    auto& engine = plugin_->engine();

    if (!engine.is_loaded()) {
        return grpc::Status(grpc::StatusCode::FAILED_PRECONDITION,
                            "no model loaded — set model_path in plugin config");
    }

    float temperature = req->temperature() > 0 ? req->temperature() : 0.7;
    int max_tokens = req->max_tokens() > 0 ? req->max_tokens() : 512;

    std::string context;
    std::vector<InferenceEngine::ImageAttachment> image_attachments;
    grpc::Status att_err;
    parse_attachments(*req, engine, context, image_attachments, att_err);
    if (!att_err.ok()) return att_err;

    auto messages = build_messages(*req, context);

    plugin_->set_activity("evaluating prompt");

    auto token_callback = [&](const std::string& token_text) -> bool {
        plugin_->set_activity("generating");

        protocol::LLMChatChunk chunk;
        chunk.set_token(sanitize_utf8(token_text));
        chunk.set_done(false);
        chunk.set_model(engine.model_name());

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

    // Send truncation warning as a visible token
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

    return grpc::Status::OK;
}
