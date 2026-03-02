/// ix-bin plugin implementation.
///
/// Metadata, lifecycle, HTTP handlers, glyph definition, and job execution
/// for binary/structured data ingestion.
module ixbin.plugin;

import ixbin.proto;
import ixbin.grpc;
import ixbin.ats;
import ixbin.ingest;

import std.conv : to;
import std.json;

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

    // Cached ingestion results per glyph
    IngestResult[string] glyphResults;
}

private __gshared PluginState state;

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

enum PLUGIN_NAME    = "ix-bin";
enum PLUGIN_VERSION = "0.1.0";

MetadataResponse metadata() {
    MetadataResponse resp;
    resp.name        = PLUGIN_NAME;
    resp.version_    = PLUGIN_VERSION;
    resp.qntxVersion = ">= 0.1.0";
    resp.description = "Binary/structured data ingestion — CTFE-generated parsers, SIMD scanning, zero-copy decoding";
    resp.author      = "QNTX";
    resp.license     = "MIT";
    return resp;
}

// ---------------------------------------------------------------------------
// Initialize / Shutdown
// ---------------------------------------------------------------------------

InitializeResponse initialize(ref const InitializeRequest req) {
    state.authToken       = req.authToken;
    state.atsEndpoint     = req.atsStoreEndpoint;
    state.queueEndpoint   = req.queueEndpoint;
    state.scheduleEndpoint = req.scheduleEndpoint;
    state.config          = cast(string[string])req.config;

    // Connect to ATSStore
    if (state.atsEndpoint.length > 0) {
        state.atsClient.connect(state.atsEndpoint, state.authToken);
    }

    state.initialized = true;

    InitializeResponse resp;
    resp.handlerNames = ["ix-bin.ingest"];
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
    resp.message = state.paused ? "ix-bin plugin paused" : "ix-bin plugin operational";
    resp.details["version"]     = PLUGIN_VERSION;
    resp.details["initialized"] = state.initialized ? "true" : "false";
    resp.details["ats_connected"] = state.atsClient.connected ? "true" : "false";
    return resp;
}

// ---------------------------------------------------------------------------
// Glyph definition
// ---------------------------------------------------------------------------

GlyphDefResponse registerGlyphs() {
    GlyphDefResponse resp;
    GlyphDef hexGlyph;
    hexGlyph.symbol       = "\u2B22"; // hexagon: ⬢
    hexGlyph.title        = "Binary Inspector";
    hexGlyph.label        = "ix-bin";
    hexGlyph.modulePath   = "/hex-viewer-module.js";
    hexGlyph.defaultWidth = 700;
    hexGlyph.defaultHeight = 500;
    resp.glyphs = [hexGlyph];
    return resp;
}

// ---------------------------------------------------------------------------
// HTTP request handling
// ---------------------------------------------------------------------------

HTTPResponse handleHTTP(ref const HTTPRequest req) {
    auto path = req.path;
    auto method = req.method;

    // Route dispatch using string methods (no regex per CLAUDE.md)
    if (method == "POST" && path == "/ingest") {
        return handleIngest(req);
    } else if (method == "GET" && path == "/hex-viewer-module.js") {
        return serveGlyphModule();
    } else if (method == "GET" && startsWith(path, "/hex-view")) {
        return handleHexView(req);
    } else if (method == "GET" && path == "/status") {
        return handleStatus();
    } else if (method == "POST" && path == "/set-mode") {
        return handleSetMode(req);
    }

    // 404
    HTTPResponse resp;
    resp.statusCode = 404;
    resp.body_ = cast(ubyte[])("{\"error\":\"not found: " ~ path ~ "\"}");
    resp.headers = [httpHeader("Content-Type", "application/json")];
    return resp;
}

/// POST /ingest — accept binary data, detect format, parse, create attestations.
private HTTPResponse handleIngest(ref const HTTPRequest req) {
    if (req.body_.length == 0) {
        return jsonResponse(400, `{"error":"empty body"}`);
    }

    // Extract source hint from query or header
    string source = "upload";
    foreach (h; req.headers) {
        if (h.name == "X-Source" && h.values.length > 0) {
            source = h.values[0];
        }
    }

    auto result = .ingest(req.body_, source);

    // Create attestations via ATSStore
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

    // Build JSON response
    auto json = `{"format":"` ~ result.formatName ~
                `","size_bytes":` ~ to!string(req.body_.length) ~
                `,"attestations_generated":` ~ to!string(cast(int)result.attestations.length) ~
                `,"attestations_created":` ~ created.to!string;

    if (lastError.length > 0) {
        json ~= `,"last_error":"` ~ escapeJsonString(lastError) ~ `"`;
    }

    // Include summary fields
    json ~= `,"summary":{`;
    bool first = true;
    foreach (k, v; result.summary) {
        if (!first) json ~= ",";
        json ~= `"` ~ escapeJsonString(k) ~ `":"` ~ escapeJsonString(v) ~ `"`;
        first = false;
    }
    json ~= "}}";

    return jsonResponse(200, json);
}

/// GET /hex-view?data=base64... — return hex dump of data.
private HTTPResponse handleHexView(ref const HTTPRequest req) {
    // For now, return a simple hex view prompt
    return jsonResponse(200, `{"status":"hex-view endpoint ready","usage":"POST binary data to /ingest for analysis"}`);
}

/// GET /status — plugin status.
private HTTPResponse handleStatus() {
    auto json = `{"name":"` ~ PLUGIN_NAME ~
                `","version":"` ~ PLUGIN_VERSION ~
                `","initialized":` ~ (state.initialized ? "true" : "false") ~
                `,"paused":` ~ (state.paused ? "true" : "false") ~
                `,"ats_connected":` ~ (state.atsClient.connected ? "true" : "false") ~
                `}`;
    return jsonResponse(200, json);
}

/// POST /set-mode — pause or resume the plugin.
private HTTPResponse handleSetMode(ref const HTTPRequest req) {
    if (req.body_.length == 0) {
        return jsonResponse(400, `{"error":"missing body"}`);
    }

    // Simple JSON parsing for {"mode":"paused"} or {"mode":"active"}
    auto bodyStr = cast(string)req.body_;
    if (indexOf(bodyStr, "paused") >= 0) {
        state.paused = true;
        return jsonResponse(200, `{"mode":"paused"}`);
    } else if (indexOf(bodyStr, "active") >= 0) {
        state.paused = false;
        return jsonResponse(200, `{"mode":"active"}`);
    }
    return jsonResponse(400, `{"error":"mode must be 'paused' or 'active'"}`);
}

/// GET /hex-viewer-module.js — serve the glyph UI module.
private HTTPResponse serveGlyphModule() {
    HTTPResponse resp;
    resp.statusCode = 200;
    resp.body_ = cast(ubyte[])glyphModuleSource;
    resp.headers = [
        httpHeader("Content-Type", "application/javascript"),
        httpHeader("Cache-Control", "public, max-age=3600"),
    ];
    return resp;
}

// ---------------------------------------------------------------------------
// Job execution (Pulse integration)
// ---------------------------------------------------------------------------

ExecuteJobResponse executeJob(ref const ExecuteJobRequest req) {
    ExecuteJobResponse resp;
    resp.pluginVersion = PLUGIN_VERSION;

    if (req.handlerName != "ix-bin.ingest") {
        resp.success = false;
        resp.error = "unknown handler: " ~ req.handlerName;
        return resp;
    }

    if (state.paused) {
        resp.success = false;
        resp.error = "plugin is paused";
        return resp;
    }

    // The payload is the binary data to ingest
    auto result = .ingest(req.payload, "pulse-job-" ~ req.jobId);

    // Create attestations
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

    // Result JSON
    auto resultJson = `{"format":"` ~ result.formatName ~
                      `","attestations_created":` ~ created.to!string ~ `}`;
    resp.result = cast(ubyte[])resultJson;

    return resp;
}

// ---------------------------------------------------------------------------
// RPC dispatcher — wires gRPC method paths to handlers
// ---------------------------------------------------------------------------

/// Register all RPC handlers on the gRPC server.
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
        // Empty config schema — this plugin uses per-glyph attestation config
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

/// Escape a string for JSON embedding.
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

/// Find first occurrence of substring.
private ptrdiff_t indexOf(string haystack, string needle) {
    if (needle.length == 0) return 0;
    if (needle.length > haystack.length) return -1;
    foreach (i; 0 .. haystack.length - needle.length + 1) {
        if (haystack[i .. i + needle.length] == needle) return cast(ptrdiff_t)i;
    }
    return -1;
}

/// Check if string starts with prefix.
private bool startsWith(string s, string prefix) {
    if (prefix.length > s.length) return false;
    return s[0 .. prefix.length] == prefix;
}

// ---------------------------------------------------------------------------
// Embedded glyph module source
// ---------------------------------------------------------------------------

private enum glyphModuleSource = import("hex-viewer-module.js");
