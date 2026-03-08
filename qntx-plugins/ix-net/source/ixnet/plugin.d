/// ix-net plugin implementation.
///
/// Metadata, lifecycle, HTTP handlers, glyph definition, and proxy control
/// for Claude Code API traffic capture.
///
/// Known limitations:
///   - Glyph UI is a placeholder (static text, no live capture display).
///   - /captures endpoint returns JSON from ring buffer but has no
///     pagination or filtering.
///   - Cert paths resolved relative to executable — assumes certs/ is
///     a sibling of bin/. No config override.
module ixnet.plugin;

import ixnet.proto;
import ixnet.grpc;
import ixnet.ats;
import ixnet.proxy;

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
    bool capturing;       // whether the proxy is actively capturing
    ProxyState proxy;     // HTTPS proxy state
}

private __gshared PluginState state;

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

import ixnet.version_ : PLUGIN_NAME, PLUGIN_VERSION;

MetadataResponse metadata() {
    MetadataResponse resp;
    resp.name        = PLUGIN_NAME;
    resp.version_    = PLUGIN_VERSION;
    resp.qntxVersion = ">= 0.1.0";
    resp.description = "Claude Code API traffic capture — HTTPS proxy with attestation";
    resp.author      = "QNTX";
    resp.license     = "MIT";
    return resp;
}

// ---------------------------------------------------------------------------
// Initialize / Shutdown
// ---------------------------------------------------------------------------

InitializeResponse initialize(ref const InitializeRequest req) {
    import ixnet.log;

    state.authToken       = req.authToken;
    state.atsEndpoint     = req.atsStoreEndpoint;
    state.queueEndpoint   = req.queueEndpoint;
    state.scheduleEndpoint = req.scheduleEndpoint;
    state.config          = cast(string[string])req.config;

    logInfo("[ix-net] Initialize: ats=%s queue=%s schedule=%s",
        state.atsEndpoint, state.queueEndpoint, state.scheduleEndpoint);

    // Connect to ATSStore
    if (state.atsEndpoint.length > 0) {
        if (!state.atsClient.connect(state.atsEndpoint, state.authToken)) {
            logWarn("[ix-net] Initialize: ATSStore connection failed, attestation writes disabled");
        }
    }

    state.initialized = true;

    // Auto-start the HTTPS proxy on initialize
    autoStartProxy();

    InitializeResponse resp;
    return resp;
}

void shutdown() {
    stopProxy(state.proxy);
    state.atsClient.close();
    state.initialized = false;
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

HealthResponse health() {
    HealthResponse resp;
    resp.healthy = true;
    resp.message = state.capturing
        ? "ix-net proxy active on port " ~ state.proxy.proxyPort.to!string
        : "ix-net proxy idle";
    resp.details["version"]       = PLUGIN_VERSION;
    resp.details["initialized"]   = state.initialized ? "true" : "false";
    resp.details["capturing"]     = state.capturing ? "true" : "false";
    resp.details["ats_connected"] = state.atsClient.connected ? "true" : "false";
    if (state.capturing) {
        resp.details["proxy_port"]    = state.proxy.proxyPort.to!string;
        resp.details["captures"]      = state.proxy.captureCount.to!string;
        resp.details["mode"]          = state.proxy.tlsEnabled ? "intercept" : "passthrough";
    }
    return resp;
}

// ---------------------------------------------------------------------------
// Glyph definition
// ---------------------------------------------------------------------------

GlyphDefResponse registerGlyphs() {
    GlyphDefResponse resp;
    GlyphDef glyph;
    glyph.symbol       = "\U0001F50D"; // magnifying glass: 🔍
    glyph.title        = "Network Inspector";
    glyph.label        = "ix-net";
    glyph.modulePath   = "/net-inspector-module.js";
    glyph.defaultWidth = 800;
    glyph.defaultHeight = 600;
    resp.glyphs = [glyph];
    return resp;
}

// ---------------------------------------------------------------------------
// HTTP request handling
// ---------------------------------------------------------------------------

HTTPResponse handleHTTP(ref const HTTPRequest req) {
    auto path = req.path;
    auto method = req.method;

    if (method == "GET" && path == "/status") {
        return handleStatus();
    } else if (method == "POST" && path == "/start") {
        return handleStart(req);
    } else if (method == "POST" && path == "/stop") {
        return handleStop();
    } else if (method == "GET" && path == "/captures") {
        return handleCaptures();
    } else if (method == "GET" && path == "/net-inspector-module.js") {
        return serveGlyphModule();
    }

    // 404
    HTTPResponse resp;
    resp.statusCode = 404;
    resp.body_ = cast(ubyte[])("{\"error\":\"not found: " ~ path ~ "\"}");
    resp.headers = [httpHeader("Content-Type", "application/json")];
    return resp;
}

/// GET /status — proxy status and stats.
private HTTPResponse handleStatus() {
    auto json = `{"name":"` ~ PLUGIN_NAME ~
                `","version":"` ~ PLUGIN_VERSION ~
                `","initialized":` ~ (state.initialized ? "true" : "false") ~
                `,"capturing":` ~ (state.capturing ? "true" : "false") ~
                `,"ats_connected":` ~ (state.atsClient.connected ? "true" : "false");
    if (state.capturing) {
        json ~= `,"proxy_port":` ~ state.proxy.proxyPort.to!string ~
                `,"captures":` ~ state.proxy.captureCount.to!string;
    }
    json ~= `}`;
    return jsonResponse(200, json);
}

/// POST /start — start the HTTPS proxy.
private HTTPResponse handleStart(ref const HTTPRequest req) {
    import ixnet.log;

    if (state.capturing) {
        return jsonResponse(409, `{"error":"proxy already running","port":` ~
            state.proxy.proxyPort.to!string ~ `}`);
    }

    ushort proxyPort = 9100; // default proxy port

    // Parse port from request body if provided
    if (req.body_.length > 0) {
        auto bodyStr = cast(string)req.body_;
        auto portIdx = indexOf(bodyStr, `"port":`);
        if (portIdx >= 0) {
            auto numStart = portIdx + 7;
            while (numStart < bodyStr.length && bodyStr[numStart] == ' ') numStart++;
            auto numEnd = numStart;
            while (numEnd < bodyStr.length && bodyStr[numEnd] >= '0' && bodyStr[numEnd] <= '9') numEnd++;
            if (numEnd > numStart) {
                proxyPort = bodyStr[numStart .. numEnd].to!ushort;
            }
        }
    }

    // Resolve cert paths relative to executable location
    string certFile = "";
    string keyFile = "";
    auto exeDir = getExeDir();
    if (exeDir.length > 0) {
        auto certsDir = exeDir ~ "/../certs/";
        certFile = certsDir ~ "leaf.pem";
        keyFile = certsDir ~ "leaf.key";

        // Check if certs exist
        import std.file : exists;
        if (!exists(certFile) || !exists(keyFile)) {
            logWarn("[ix-net] certs not found at %s — running in passthrough mode", certsDir);
            certFile = "";
            keyFile = "";
        }
    }

    if (startProxy(state.proxy, proxyPort, certFile, keyFile)) {
        state.capturing = true;
        auto mode = state.proxy.tlsEnabled ? "intercept" : "passthrough";
        logInfo("[ix-net] HTTPS proxy started on port %d (mode=%s)",
                state.proxy.proxyPort, mode);
        return jsonResponse(200, `{"status":"started","port":` ~
            state.proxy.proxyPort.to!string ~
            `,"mode":"` ~ mode ~
            `","usage":"export HTTPS_PROXY=http://localhost:` ~
            state.proxy.proxyPort.to!string ~
            ` NODE_EXTRA_CA_CERTS=<ca.pem>"}`);
    }

    return jsonResponse(500, `{"error":"failed to start proxy"}`);
}

/// POST /stop — stop the HTTPS proxy.
private HTTPResponse handleStop() {
    import ixnet.log;

    if (!state.capturing) {
        return jsonResponse(409, `{"error":"proxy not running"}`);
    }

    stopProxy(state.proxy);
    state.capturing = false;
    logInfo("[ix-net] HTTPS proxy stopped");
    return jsonResponse(200, `{"status":"stopped"}`);
}

/// GET /captures — return recent captured API exchanges.
private HTTPResponse handleCaptures() {
    auto captures = getRecentCaptures(state.proxy);
    return jsonResponse(200, captures);
}

/// GET /net-inspector-module.js — serve the glyph UI module.
private HTTPResponse serveGlyphModule() {
    HTTPResponse resp;
    resp.statusCode = 200;
    // Minimal placeholder — will be expanded
    resp.body_ = cast(ubyte[])(
        "export function render(glyph, ui) {\n" ~
        "  const c = document.createElement('div');\n" ~
        "  c.style.cssText = 'padding: 20px; font-family: monospace; color: #33ff33; background: #0a0a0f; height: 100%;';\n" ~
        "  c.textContent = 'ix-net: Claude Code API Inspector';\n" ~
        "  return c;\n" ~
        "}\n"
    );
    resp.headers = [
        httpHeader("Content-Type", "application/javascript"),
        httpHeader("Cache-Control", "no-cache"),
    ];
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
        ExecuteJobResponse resp;
        resp.success = false;
        resp.error = "ix-net does not support async jobs";
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

private ptrdiff_t indexOf(string haystack, string needle) {
    if (needle.length == 0) return 0;
    if (needle.length > haystack.length) return -1;
    foreach (i; 0 .. haystack.length - needle.length + 1) {
        if (haystack[i .. i + needle.length] == needle) return cast(ptrdiff_t)i;
    }
    return -1;
}

/// Auto-start the proxy during plugin initialization.
private void autoStartProxy() {
    import ixnet.log;

    ushort proxyPort = 9100;

    string certFile = "";
    string keyFile = "";
    auto exeDir = getExeDir();
    if (exeDir.length > 0) {
        auto certsDir = exeDir ~ "/../certs/";
        certFile = certsDir ~ "leaf.pem";
        keyFile = certsDir ~ "leaf.key";

        import std.file : exists;
        if (!exists(certFile) || !exists(keyFile)) {
            logWarn("[ix-net] certs not found at %s — running in passthrough mode", certsDir);
            certFile = "";
            keyFile = "";
        }
    }

    // Pass ATSClient to proxy for attestation writes
    state.proxy.atsClient = cast(void*)&state.atsClient;

    if (startProxy(state.proxy, proxyPort, certFile, keyFile)) {
        state.capturing = true;
        auto mode = state.proxy.tlsEnabled ? "intercept" : "passthrough";
        logInfo("[ix-net] proxy auto-started on port %d (mode=%s)",
                state.proxy.proxyPort, mode);
    } else {
        logError("[ix-net] proxy auto-start failed on port %d", proxyPort);
    }
}

/// Get the directory containing the running executable.
private string getExeDir() {
    import std.file : thisExePath;
    auto path = thisExePath();
    // Find last '/'
    for (ptrdiff_t i = cast(ptrdiff_t)path.length - 1; i >= 0; i--) {
        if (path[i] == '/') return cast(string)path[0 .. i];
    }
    return "";
}
