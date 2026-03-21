/// ix-otlp plugin implementation.
///
/// Receives OTLP/HTTP JSON trace exports via HandleHTTP, decodes spans,
/// and writes each as an OTLPSpan attestation to ATS.
///
/// HTTP endpoint: POST /v1/traces (routed from /api/ix-otlp/v1/traces)
/// OTLP exporters configure: OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:877/api/ix-otlp
module ixotlp.plugin;

import ixotlp.proto;
import ixotlp.grpc;
import ixotlp.ats;
import ixotlp.otlp;

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
    bool initialized;
    bool paused;
}

private __gshared PluginState state;

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

import ixotlp.version_ : PLUGIN_NAME, PLUGIN_VERSION;

MetadataResponse metadata() {
    MetadataResponse resp;
    resp.name        = PLUGIN_NAME;
    resp.version_    = PLUGIN_VERSION;
    resp.qntxVersion = ">= 0.1.0";
    resp.description = "OpenTelemetry trace ingestion — receives OTLP/HTTP JSON exports, persists spans as OTLPSpan attestations";
    resp.author      = "QNTX";
    resp.license     = "MIT";
    return resp;
}

// ---------------------------------------------------------------------------
// Initialize / Shutdown
// ---------------------------------------------------------------------------

InitializeResponse initialize(ref const InitializeRequest req) {
    import ixotlp.log;

    state.authToken       = req.authToken;
    state.atsEndpoint     = req.atsStoreEndpoint;
    state.queueEndpoint   = req.queueEndpoint;
    state.scheduleEndpoint = req.scheduleEndpoint;
    state.config          = cast(string[string])req.config;

    logInfo("[ix-otlp] Initialize: ats=%s queue=%s schedule=%s",
        state.atsEndpoint, state.queueEndpoint, state.scheduleEndpoint);

    if (state.atsEndpoint.length > 0) {
        if (!state.atsClient.connect(state.atsEndpoint, state.authToken)) {
            import ixotlp.log;
            logWarn("[ix-otlp] Initialize: ATSStore connection failed, attestation writes disabled");
        }
    } else {
        logWarn("[ix-otlp] Initialize: no ATSStore endpoint provided");
    }

    state.initialized = true;

    InitializeResponse resp;
    resp.handlerNames = ["ix-otlp.ingest"];
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
    resp.message = state.paused ? "ix-otlp plugin paused" : "ix-otlp plugin operational";
    resp.details["version"]     = PLUGIN_VERSION;
    resp.details["initialized"] = state.initialized ? "true" : "false";
    resp.details["ats_connected"] = state.atsClient.connected ? "true" : "false";
    return resp;
}

// ---------------------------------------------------------------------------
// Glyph definition (none for this plugin)
// ---------------------------------------------------------------------------

GlyphDefResponse registerGlyphs() {
    GlyphDefResponse resp;
    return resp;
}

// ---------------------------------------------------------------------------
// HTTP request handling
// ---------------------------------------------------------------------------

HTTPResponse handleHTTP(ref const HTTPRequest req) {
    auto path = req.path;
    auto method = req.method;

    if (method == "POST" && path == "/v1/traces") {
        return handleTraces(req);
    } else if (method == "OPTIONS") {
        return jsonResponse(200, "{}");
    } else if (method == "GET" && path == "/status") {
        return handleStatus();
    } else if (method == "GET" && (path == "/" || path == "")) {
        return jsonResponse(200, `{"name":"` ~ PLUGIN_NAME ~ `","version":"` ~ PLUGIN_VERSION ~ `"}`);
    }

    HTTPResponse resp;
    resp.statusCode = 404;
    resp.body_ = cast(ubyte[])("{\"error\":\"not found: " ~ path ~ "\"}");
    resp.headers = [httpHeader("Content-Type", "application/json")];
    return resp;
}

/// POST /v1/traces — receive OTLP ExportTraceServiceRequest JSON.
private HTTPResponse handleTraces(ref const HTTPRequest req) {
    import ixotlp.log;

    if (req.body_.length == 0) {
        return jsonResponse(400, `{"error":"empty body"}`);
    }

    auto body_ = cast(string)req.body_;
    logInfo("[ix-otlp] Trace export received (%d bytes)", req.body_.length);

    auto result = ingestOTLP(body_);

    if (result.lastError.length > 0 && result.spanCount == 0) {
        return jsonResponse(400, `{"error":"` ~ escapeJsonString(result.lastError) ~ `"}`);
    }

    // Write attestations to ATS
    int created = 0;
    string lastError;
    if (state.atsClient.connected) {
        foreach (ref cmd; result.attestations) {
            string err;
            if (state.atsClient.createAttestation(cmd, err)) {
                created++;
            } else {
                lastError = err;
            }
        }
    }

    logInfo("[ix-otlp] Processed: %d spans, %d traces, %d attestations created",
        result.spanCount, result.traceCount, created);

    // OTLP ExportTraceServiceResponse — partialSuccess if some failed
    if (created == result.spanCount) {
        return jsonResponse(200, `{"partialSuccess":null}`);
    } else {
        auto rejected = result.spanCount - created;
        auto json = `{"partialSuccess":{"rejectedSpans":` ~ to!string(rejected);
        if (lastError.length > 0) {
            json ~= `,"errorMessage":"` ~ escapeJsonString(lastError) ~ `"`;
        }
        json ~= `}}`;
        return jsonResponse(200, json);
    }
}

/// GET /status
private HTTPResponse handleStatus() {
    auto json = `{"name":"` ~ PLUGIN_NAME ~
                `","version":"` ~ PLUGIN_VERSION ~
                `","initialized":` ~ (state.initialized ? "true" : "false") ~
                `,"paused":` ~ (state.paused ? "true" : "false") ~
                `,"ats_connected":` ~ (state.atsClient.connected ? "true" : "false") ~
                `}`;
    return jsonResponse(200, json);
}

// ---------------------------------------------------------------------------
// Job execution
// ---------------------------------------------------------------------------

ExecuteJobResponse executeJob(ref const ExecuteJobRequest req) {
    ExecuteJobResponse resp;
    resp.pluginVersion = PLUGIN_VERSION;

    if (req.handlerName != "ix-otlp.ingest") {
        resp.success = false;
        resp.error = "unknown handler: " ~ req.handlerName;
        return resp;
    }

    if (state.paused) {
        resp.success = false;
        resp.error = "plugin is paused";
        return resp;
    }

    auto body_ = cast(string)req.payload;
    auto result = ingestOTLP(body_);

    int created = 0;
    if (state.atsClient.connected) {
        foreach (ref cmd; result.attestations) {
            string err;
            if (state.atsClient.createAttestation(cmd, err)) {
                created++;
            }
        }
    }

    resp.success = true;
    resp.progressCurrent = created;
    resp.progressTotal = cast(int)result.attestations.length;
    resp.result = cast(ubyte[])(`{"spans":` ~ to!string(result.spanCount) ~
                                `,"traces":` ~ to!string(result.traceCount) ~
                                `,"attestations_created":` ~ to!string(created) ~ `}`);
    return resp;
}

// ---------------------------------------------------------------------------
// RPC dispatcher
// ---------------------------------------------------------------------------

void registerHandlers(ref GrpcServer server) {
    server.registerHandler("/protocol.DomainPluginService/Metadata", (const ubyte[] _) {
        auto resp = metadata();
        return encode(resp);
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
        auto resp = health();
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/ConfigSchema", (const ubyte[] _) {
        ConfigSchemaResponse resp;
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/RegisterGlyphs", (const ubyte[] _) {
        auto resp = registerGlyphs();
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/ExecuteJob", (const ubyte[] data) {
        auto req = decode!ExecuteJobRequest(data);
        auto resp = executeJob(req);
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

private string escapeJsonString(string s) {
    char[] result;
    foreach (c; s) {
        switch (c) {
            case '"':  result ~= `\"`; break;
            case '\\': result ~= `\\`; break;
            case '\n': result ~= `\n`; break;
            case '\r': result ~= `\r`; break;
            case '\t': result ~= `\t`; break;
            default:   result ~= c; break;
        }
    }
    return cast(string)result.idup;
}
