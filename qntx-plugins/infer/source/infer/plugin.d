/// Infer plugin — LLM provider that proxies llama-server with probability
/// capture, computes entropy/confidence signals, and writes inference
/// attestations to the QNTX attestation store.
///
/// Architecture:
///   QNTX prompt → gRPC LLMService/Chat → this plugin
///     → HTTP POST /completion (llama-server, n_probs=20, post_sampling_probs=true)
///     → parse probabilities → compute signals → write attestation → return response
module infer.plugin;

import infer.proto;
import infer.grpc;
import infer.ats;
import infer.http;
import infer.signals;
import infer.log;

import std.conv : to;

// ---------------------------------------------------------------------------
// Plugin state
// ---------------------------------------------------------------------------

struct PluginState {
    string authToken;
    string atsEndpoint;
    string queueEndpoint;
    string scheduleEndpoint;
    string[string] config;
    ATSClient atsClient;
    LlamaServerClient llamaClient;
    bool initialized;
    int nProbs;          // Number of top-N probabilities to request (default 20)
    double confThreshold; // Low-confidence threshold (default 0.3)
    uint generationCounter; // Monotonic counter for generation IDs
}

private __gshared PluginState state;

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

import infer.version_ : PLUGIN_NAME, PLUGIN_VERSION;

MetadataResponse metadata() {
    MetadataResponse resp;
    resp.name        = PLUGIN_NAME;
    resp.version_    = PLUGIN_VERSION;
    resp.qntxVersion = ">= 0.1.0";
    resp.description = "Inference attestation — entropy and confidence signals from llama-server probabilities";
    resp.author      = "QNTX";
    resp.license     = "MIT";
    return resp;
}

// ---------------------------------------------------------------------------
// Initialize / Shutdown
// ---------------------------------------------------------------------------

InitializeResponse initialize(ref const InitializeRequest req) {
    state.authToken        = req.authToken;
    state.atsEndpoint      = req.atsStoreEndpoint;
    state.queueEndpoint    = req.queueEndpoint;
    state.scheduleEndpoint = req.scheduleEndpoint;
    state.config           = cast(string[string])req.config;

    logInfo("[infer] Initialize: ats=%s queue=%s schedule=%s",
        state.atsEndpoint, state.queueEndpoint, state.scheduleEndpoint);

    // Parse config
    auto llamaUrl = "llama_server_url" in state.config;
    if (llamaUrl is null || (*llamaUrl).length == 0) {
        logError("[infer] Initialize: llama_server_url not configured — LLM requests will fail");
    } else {
        if (!state.llamaClient.configure(*llamaUrl)) {
            logError("[infer] Initialize: invalid llama_server_url: %s", *llamaUrl);
        } else {
            logInfo("[infer] Initialize: llama-server at %s:%d", state.llamaClient.host, state.llamaClient.port);
        }
    }

    // n_probs (default 20)
    state.nProbs = 20;
    if (auto np = "n_probs" in state.config) {
        try { state.nProbs = (*np).to!int; } catch (Exception) {}
    }
    if (state.nProbs < 1) state.nProbs = 20;

    // confidence_threshold (default 0.3)
    state.confThreshold = 0.3;
    if (auto ct = "confidence_threshold" in state.config) {
        try { state.confThreshold = (*ct).to!double; } catch (Exception) {}
    }

    logInfo("[infer] Initialize: n_probs=%d confidence_threshold=%.2f", state.nProbs, state.confThreshold);

    // Connect to ATSStore
    if (state.atsEndpoint.length > 0) {
        if (!state.atsClient.connect(state.atsEndpoint, state.authToken)) {
            logWarn("[infer] Initialize: ATSStore connection failed, attestation writes disabled");
        }
    } else {
        logWarn("[infer] Initialize: no ATSStore endpoint provided");
    }

    state.initialized = true;

    InitializeResponse resp;
    resp.handlerNames = ["infer.attest"];
    resp.llmProvider = true; // Register as LLM provider
    return resp;
}

void shutdown() {
    state.atsClient.close();
    state.initialized = false;
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

HealthResponse health() {
    HealthResponse resp;
    resp.healthy = true;
    resp.message = "infer plugin operational";
    resp.details["version"]       = PLUGIN_VERSION;
    resp.details["initialized"]   = state.initialized ? "true" : "false";
    resp.details["ats_connected"] = state.atsClient.connected ? "true" : "false";
    resp.details["llama_server"]  = state.llamaClient.host ~ ":" ~ state.llamaClient.port.to!string;
    resp.details["n_probs"]       = state.nProbs.to!string;
    return resp;
}

// ---------------------------------------------------------------------------
// Config schema
// ---------------------------------------------------------------------------

ConfigSchemaResponse configSchema() {
    ConfigSchemaResponse resp;
    resp.fields["llama_server_url"] = "string|required|URL of the running llama-server (e.g., http://127.0.0.1:8080)";
    resp.fields["n_probs"] = "number|optional|Top-N probabilities to capture per token (default 20)";
    resp.fields["confidence_threshold"] = "number|optional|Confidence threshold for low-confidence span detection (default 0.3)";
    return resp;
}

// ---------------------------------------------------------------------------
// LLM Chat — the core inference + attestation pipeline
// ---------------------------------------------------------------------------

LLMChatResponse handleChat(ref const LLMChatRequest req) {
    LLMChatResponse resp;

    if (state.llamaClient.host.length == 0) {
        resp.content = "Error: llama_server_url not configured for infer plugin";
        return resp;
    }

    // Build prompt — combine system prompt + user prompt as llama-server expects a flat prompt
    string prompt;
    if (req.systemPrompt.length > 0) {
        prompt = req.systemPrompt ~ "\n\n" ~ req.userPrompt;
    } else {
        prompt = req.userPrompt;
    }

    auto maxTokens = req.maxTokens > 0 ? req.maxTokens : 512;
    auto temperature = req.temperature > 0.0 ? req.temperature : 0.7;

    logInfo("[infer] Chat: prompt_len=%d max_tokens=%d n_probs=%d temperature=%.2f",
        prompt.length, maxTokens, state.nProbs, temperature);

    // Forward to llama-server with probability capture
    auto completion = state.llamaClient.complete(prompt, maxTokens, state.nProbs, temperature);

    if (completion.error.length > 0) {
        logError("[infer] Chat: llama-server error: %s", completion.error);
        resp.content = "Error: " ~ completion.error;
        return resp;
    }

    resp.content = completion.content;
    resp.model = completion.model;
    resp.completionTokens = cast(int)completion.probs.length;

    // Compute signals from probability data
    if (completion.probs.length > 0) {
        auto sigs = computeSignals(completion.probs, state.confThreshold);

        logInfo("[infer] Signals: tokens=%d mean_entropy=%.3f max_entropy=%.3f mean_conf=%.3f min_conf=%.3f spikes=%d low_spans=%d",
            sigs.tokenCount, sigs.meanEntropy, sigs.maxEntropy,
            sigs.meanConfidence, sigs.minConfidence,
            cast(int)sigs.entropySpikes.length, cast(int)sigs.lowConfSpans.length);

        // Write attestation
        writeInferenceAttestation(prompt, completion, sigs);
    } else {
        logWarn("[infer] Chat: no probability data returned — llama-server may not support n_probs");
    }

    return resp;
}

// ---------------------------------------------------------------------------
// Attestation writing
// ---------------------------------------------------------------------------

private void writeInferenceAttestation(
    string prompt,
    ref const CompletionResponse completion,
    ref const GenerationSignals sigs
) {
    if (!state.atsClient.connected) {
        logWarn("[infer] attestation skipped — ATSStore not connected");
        return;
    }

    state.generationCounter++;
    auto genId = state.generationCounter.to!string;

    // Build attributes
    auto sigAttrs = signalsToAttributes(sigs);

    // Add generation metadata
    sigAttrs["prompt_length"] = prompt.length.to!string;
    sigAttrs["completion_tokens"] = (cast(int)completion.probs.length).to!string;
    sigAttrs["n_probs"] = state.nProbs.to!string;
    sigAttrs["truncated"] = completion.truncated ? "true" : "false";
    if (completion.model.length > 0) {
        sigAttrs["model"] = completion.model;
    }

    auto attrBytes = encodeStructFromStringMap(sigAttrs);

    AttestationCommand cmd;
    cmd.subjects    = ["inference:" ~ genId];
    cmd.predicates  = ["generated"];
    cmd.contexts    = completion.model.length > 0
                    ? ["model:" ~ completion.model]
                    : ["model:unknown"];
    cmd.actors      = ["llama-server:" ~ state.llamaClient.host ~ ":" ~ state.llamaClient.port.to!string];
    cmd.attributes  = attrBytes;
    cmd.source      = "infer";
    cmd.sourceVersion = PLUGIN_VERSION;

    string err;
    if (state.atsClient.createAttestation(cmd, err)) {
        logInfo("[infer] attestation created for inference:%s — mean_entropy=%.3f min_conf=%.3f",
            genId, sigs.meanEntropy, sigs.minConfidence);
    } else {
        logError("[infer] attestation failed for inference:%s — %s", genId, err);
    }
}

// ---------------------------------------------------------------------------
// HTTP handler (status endpoint)
// ---------------------------------------------------------------------------

HTTPResponse handleHTTP(ref const HTTPRequest req) {
    auto path = req.path;
    auto method = req.method;

    if (method == "GET" && path == "/status") {
        auto json = `{"name":"` ~ PLUGIN_NAME ~
                    `","version":"` ~ PLUGIN_VERSION ~
                    `","initialized":` ~ (state.initialized ? "true" : "false") ~
                    `,"ats_connected":` ~ (state.atsClient.connected ? "true" : "false") ~
                    `,"llama_server":"` ~ state.llamaClient.host ~ ":" ~ state.llamaClient.port.to!string ~
                    `","n_probs":` ~ state.nProbs.to!string ~
                    `,"generation_count":` ~ state.generationCounter.to!string ~
                    `}`;
        return jsonResponse(200, json);
    }

    HTTPResponse resp;
    resp.statusCode = 404;
    resp.body_ = cast(ubyte[])(`{"error":"not found: ` ~ path ~ `"}`);
    resp.headers = [httpHeader("Content-Type", "application/json")];
    return resp;
}

// ---------------------------------------------------------------------------
// Glyph definition — placeholder for future inference browser glyph
// ---------------------------------------------------------------------------

GlyphDefResponse registerGlyphs() {
    GlyphDefResponse resp;
    return resp;
}

// ---------------------------------------------------------------------------
// Job execution
// ---------------------------------------------------------------------------

ExecuteJobResponse executeJob(ref const ExecuteJobRequest req) {
    ExecuteJobResponse resp;
    resp.pluginVersion = PLUGIN_VERSION;
    resp.success = false;
    resp.error = "infer plugin does not handle async jobs";
    return resp;
}

// ---------------------------------------------------------------------------
// RPC dispatcher
// ---------------------------------------------------------------------------

/// Register all RPC handlers on the gRPC server.
void registerHandlers(ref GrpcServer server) {
    server.registerHandler("/protocol.DomainPluginService/Metadata", (const ubyte[] _) {
        return encode(metadata());
    });

    server.registerHandler("/protocol.DomainPluginService/Initialize", (const ubyte[] data) {
        auto req = decode!InitializeRequest(data);
        auto resp = initialize(req);
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/Shutdown", (const ubyte[] _) {
        shutdown();
        return encode(Empty());
    });

    server.registerHandler("/protocol.DomainPluginService/HandleHTTP", (const ubyte[] data) {
        auto req = decode!HTTPRequest(data);
        auto resp = handleHTTP(req);
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/Health", (const ubyte[] _) {
        return encode(health());
    });

    server.registerHandler("/protocol.DomainPluginService/ConfigSchema", (const ubyte[] _) {
        return encode(configSchema());
    });

    server.registerHandler("/protocol.DomainPluginService/RegisterGlyphs", (const ubyte[] _) {
        return encode(registerGlyphs());
    });

    server.registerHandler("/protocol.DomainPluginService/ExecuteJob", (const ubyte[] data) {
        auto req = decode!ExecuteJobRequest(data);
        auto resp = executeJob(req);
        return encode(resp);
    });

    // LLM service — this is what makes the plugin an LLM provider
    server.registerHandler("/protocol.LLMService/Chat", (const ubyte[] data) {
        auto req = decode!LLMChatRequest(data);
        auto resp = handleChat(req);
        return encode(resp);
    });
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

private HTTPHeader httpHeader(string name, string value) {
    HTTPHeader h;
    h.name = name;
    h.values = [value];
    return h;
}

private HTTPResponse jsonResponse(int status, string body_) {
    HTTPResponse resp;
    resp.statusCode = status;
    resp.body_ = cast(ubyte[])body_;
    resp.headers = [httpHeader("Content-Type", "application/json")];
    return resp;
}
