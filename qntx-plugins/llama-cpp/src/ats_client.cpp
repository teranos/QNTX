#include "ats_client.h"
#include "plugin.h"

#include <iostream>

#include <google/protobuf/struct.pb.h>

// Defined in plugin.cpp — replaces invalid UTF-8 bytes with U+FFFD
extern std::string sanitize_utf8(const std::string& s);

static google::protobuf::Value make_string(const std::string& s) {
    google::protobuf::Value v;
    v.set_string_value(s);
    return v;
}

static google::protobuf::Value make_number(double n) {
    google::protobuf::Value v;
    v.set_number_value(n);
    return v;
}

void AtsClient::configure(const std::string& endpoint, const std::string& auth_token) {
    auth_token_ = auth_token;
    auto channel = grpc::CreateChannel(endpoint, grpc::InsecureChannelCredentials());
    stub_ = protocol::ATSStoreService::NewStub(channel);
    std::cout << "[llama-cpp] ATS client configured: " << endpoint << std::endl;
}

bool AtsClient::create_weave(const std::string& model_name,
                              const std::string& prompt,
                              const std::string& response_text,
                              const std::string& context_id,
                              int token_count,
                              float mean_confidence,
                              float mean_entropy,
                              const std::vector<TokenSignal>& signals) {
    if (!stub_) return false;

    protocol::GenerateAttestationRequest req;
    req.set_auth_token(auth_token_);

    auto* cmd = req.mutable_command();
    cmd->add_subjects("model:" + model_name);
    cmd->add_predicates("Weave");
    cmd->add_contexts(context_id);
    cmd->add_actors("llama-cpp");
    cmd->set_source("llama-cpp");
    cmd->set_source_version(PLUGIN_VERSION);

    auto* attrs = cmd->mutable_attributes();
    auto* fields = attrs->mutable_fields();
    (*fields)["prompt"] = make_string(sanitize_utf8(prompt));
    (*fields)["text"] = make_string(sanitize_utf8(response_text));
    (*fields)["model"] = make_string(model_name);
    (*fields)["token_count"] = make_number(token_count);
    (*fields)["mean_confidence"] = make_number(mean_confidence);
    (*fields)["mean_entropy"] = make_number(mean_entropy);
    (*fields)["weave_source"] = make_string("llama-cpp");

    // Pack token signals into attributes — one weave, no eviction
    if (!signals.empty()) {
        google::protobuf::Value tokens_val;
        auto* list = tokens_val.mutable_list_value();
        for (int i = 0; i < (int)signals.size(); i++) {
            const auto& sig = signals[i];
            google::protobuf::Value item;
            auto* f = item.mutable_struct_value()->mutable_fields();
            (*f)["text"] = make_string(sanitize_utf8(sig.token_text));
            (*f)["position"] = make_number(i);
            (*f)["confidence"] = make_number(sig.confidence);
            (*f)["entropy"] = make_number(sig.entropy);
            (*f)["top_gap"] = make_number(sig.top_gap);

            if (!sig.top_k.empty()) {
                google::protobuf::Value top_k_val;
                auto* top_k_list = top_k_val.mutable_list_value();
                for (const auto& cand : sig.top_k) {
                    google::protobuf::Value c;
                    auto* cf = c.mutable_struct_value()->mutable_fields();
                    (*cf)["text"] = make_string(sanitize_utf8(cand.text));
                    (*cf)["prob"] = make_number(cand.prob);
                    *top_k_list->add_values() = c;
                }
                (*f)["top_k"] = top_k_val;
            }

            *list->add_values() = item;
        }
        (*fields)["tokens"] = tokens_val;
    }

    protocol::GenerateAttestationResponse resp;
    grpc::ClientContext ctx;
    auto status = stub_->GenerateAndCreateAttestation(&ctx, req, &resp);

    if (!status.ok()) {
        std::cerr << "[llama-cpp] ATS weave write failed: " << status.error_message() << std::endl;
        return false;
    }
    if (!resp.success()) {
        std::cerr << "[llama-cpp] ATS weave rejected: " << resp.error() << std::endl;
        return false;
    }

    std::cout << "[llama-cpp] Weave attestation created: " << resp.attestation().id()
              << " (" << signals.size() << " tokens embedded)" << std::endl;
    return true;
}
