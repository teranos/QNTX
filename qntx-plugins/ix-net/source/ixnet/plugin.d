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
    glyph.modulePath   = "/glyph-module.js";
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
    } else if (method == "GET" && path == "/glyph-module.js") {
        return serveGlyphModule();
    } else if (method == "GET" && path.length >= 7 && path[0 .. 7] == "/images") {
        return handleImages(path, req);
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

    string certFile, keyFile;
    resolveCertPaths(certFile, keyFile);

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

/// GET /glyph-module.js — serve the glyph UI module.
/// Source of truth: web/glyph-module.ts (keep in sync).
private HTTPResponse serveGlyphModule() {
    HTTPResponse resp;
    resp.statusCode = 200;
    resp.body_ = cast(ubyte[])(
        "export const glyphDef = {\n" ~
        "  symbol: '\\u{1F50D}',\n" ~
        "  title: 'Network Inspector',\n" ~
        "  label: 'ix-net',\n" ~
        "  defaultWidth: 320,\n" ~
        "  defaultHeight: 280,\n" ~
        "};\n" ~
        "\n" ~
        "export const render = async (glyph, ui) => {\n" ~
        "  const { element } = ui.container({\n" ~
        "    defaults: {\n" ~
        "      x: glyph.x ?? 100,\n" ~
        "      y: glyph.y ?? 100,\n" ~
        "      width: 320,\n" ~
        "      height: 280,\n" ~
        "    },\n" ~
        "    titleBar: { label: 'ix-net' },\n" ~
        "    resizable: true,\n" ~
        "  });\n" ~
        "\n" ~
        "  const body = document.createElement('div');\n" ~
        "  body.style.flex = '1';\n" ~
        "  body.style.overflow = 'auto';\n" ~
        "  body.style.padding = '12px';\n" ~
        "  body.style.fontFamily = 'monospace';\n" ~
        "  body.style.fontSize = '13px';\n" ~
        "  element.appendChild(body);\n" ~
        "\n" ~
        "  const status = ui.statusLine();\n" ~
        "  element.appendChild(status.element);\n" ~
        "\n" ~
        "  function row(parent, label, value) {\n" ~
        "    const el = document.createElement('div');\n" ~
        "    el.style.display = 'flex';\n" ~
        "    el.style.justifyContent = 'space-between';\n" ~
        "    el.style.padding = '2px 0';\n" ~
        "    const lbl = document.createElement('span');\n" ~
        "    lbl.style.color = 'var(--muted-foreground, #888)';\n" ~
        "    lbl.textContent = label;\n" ~
        "    const val = document.createElement('span');\n" ~
        "    val.textContent = value;\n" ~
        "    el.appendChild(lbl);\n" ~
        "    el.appendChild(val);\n" ~
        "    parent.appendChild(el);\n" ~
        "  }\n" ~
        "\n" ~
        "  async function refresh() {\n" ~
        "    try {\n" ~
        "      const resp = await ui.pluginFetch('/captures');\n" ~
        "      const data = await resp.json();\n" ~
        "      const caps = data.captures || [];\n" ~
        "      const total = data.total || 0;\n" ~
        "      const withImages = caps.filter(c => c.has_images).length;\n" ~
        "      const totalImages = caps.reduce((n, c) => n + c.image_count, 0);\n" ~
        "      const totalIn = caps.reduce((n, c) => n + c.input_tokens, 0);\n" ~
        "      const totalOut = caps.reduce((n, c) => n + c.output_tokens, 0);\n" ~
        "\n" ~
        "      body.innerHTML = '';\n" ~
        "      row(body, 'Proxy', 'listening');\n" ~
        "      row(body, 'Captures', String(total));\n" ~
        "      row(body, 'With images', String(withImages));\n" ~
        "      row(body, 'Total images', String(totalImages));\n" ~
        "      row(body, 'Input tokens', totalIn.toLocaleString());\n" ~
        "      row(body, 'Output tokens', totalOut.toLocaleString());\n" ~
        "\n" ~
        "      if (caps.length > 0) {\n" ~
        "        const last = caps[caps.length - 1];\n" ~
        "        row(body, 'Last model', last.model);\n" ~
        "        row(body, 'Last status', String(last.status_code));\n" ~
        "      }\n" ~
        "\n" ~
        "      status.clear();\n" ~
        "    } catch {\n" ~
        "      body.innerHTML = '';\n" ~
        "      row(body, 'Proxy', 'not reachable');\n" ~
        "      status.show('fetch failed', true);\n" ~
        "    }\n" ~
        "  }\n" ~
        "\n" ~
        "  await refresh();\n" ~
        "  const interval = setInterval(refresh, 5000);\n" ~
        "  ui.onCleanup(() => clearInterval(interval));\n" ~
        "\n" ~
        "  return element;\n" ~
        "};\n"
    );
    resp.headers = [
        httpHeader("Content-Type", "application/javascript"),
        httpHeader("Cache-Control", "no-cache"),
    ];
    return resp;
}

/// GET /images?session=<id> — list images for a session.
/// GET /images?branch=<name> — find sessions for a branch, list all images.
/// GET /images/<session>/<filename> — serve an image file.
private HTTPResponse handleImages(string path, ref const HTTPRequest req) {
    import std.file : exists, isDir, dirEntries, SpanMode, read_ = read;

    auto baseDir = getImageDir();
    if (baseDir.length == 0)
        return jsonResponse(500, `{"error":"cannot resolve image directory"}`);

    if (path == "/images" || (path.length > 7 && path[7] == '?')) {
        auto branch = getQueryParam(req, "branch");
        auto session = getQueryParam(req, "session");

        // Branch query: resolve branch → session IDs via ATS
        if (branch.length > 0) {
            return handleImagesByBranch(branch, baseDir);
        }

        if (session.length == 0)
            return jsonResponse(400, `{"error":"missing session or branch parameter"}`);

        return handleImagesBySession(session, baseDir);
    }

    // Serve image: GET /images/<session>/<filename>
    // path is "/images/<session>/<filename>"
    auto rest = path[8 .. $]; // skip "/images/"
    auto slashIdx = indexOf(rest, "/");
    if (slashIdx < 0)
        return jsonResponse(400, `{"error":"expected /images/<session>/<filename>"}`);

    auto session = rest[0 .. slashIdx];
    auto filename = rest[slashIdx + 1 .. $];

    if (!isSafePath(session) || !isSafePath(filename))
        return jsonResponse(400, `{"error":"invalid path"}`);

    auto filePath = baseDir ~ "/" ~ session ~ "/" ~ filename;
    if (!exists(filePath))
        return jsonResponse(404, `{"error":"image not found"}`);

    // Read and serve
    try {
        auto data = cast(ubyte[])read_(filePath);
        HTTPResponse resp;
        resp.statusCode = 200;
        resp.body_ = data;
        resp.headers = [
            httpHeader("Content-Type", mimeForFile(filename)),
            httpHeader("Cache-Control", "max-age=3600"),
        ];
        return resp;
    } catch (Exception e) {
        return jsonResponse(500, `{"error":"failed to read image"}`);
    }
}

/// Extract the filename from a full path.
private string entryName(string fullPath) {
    for (ptrdiff_t i = cast(ptrdiff_t)fullPath.length - 1; i >= 0; i--) {
        if (fullPath[i] == '/') return fullPath[i + 1 .. $];
    }
    return fullPath;
}

/// Check if a filename looks like an image.
private bool isImageFile(string name) {
    return endsWith(name, ".png") || endsWith(name, ".jpeg") ||
           endsWith(name, ".jpg") || endsWith(name, ".gif") ||
           endsWith(name, ".webp");
}

/// Get MIME type from filename extension.
private string mimeForFile(string name) {
    if (endsWith(name, ".png")) return "image/png";
    if (endsWith(name, ".jpeg") || endsWith(name, ".jpg")) return "image/jpeg";
    if (endsWith(name, ".gif")) return "image/gif";
    if (endsWith(name, ".webp")) return "image/webp";
    return "application/octet-stream";
}

/// List images for a single session.
private HTTPResponse handleImagesBySession(string session, string baseDir) {
    import std.file : exists, isDir, dirEntries, SpanMode;

    if (!isSafePath(session))
        return jsonResponse(400, `{"error":"invalid session id"}`);

    auto sessionDir = baseDir ~ "/" ~ session;
    if (!exists(sessionDir) || !isDir(sessionDir))
        return jsonResponse(404, `{"error":"session not found"}`);

    string json = `{"session":"` ~ jsonEscape(session) ~ `","images":[`;
    bool first = true;
    foreach (entry; dirEntries(sessionDir, SpanMode.shallow)) {
        auto name = entryName(entry.name);
        if (!isImageFile(name)) continue;
        if (!first) json ~= ",";
        json ~= `"` ~ jsonEscape(name) ~ `"`;
        first = false;
    }
    json ~= `]}`;
    return jsonResponse(200, json);
}

/// Resolve branch → sessions via ATS, then list all images across sessions.
///
/// Queries GraundedPreToolUse attestations with control=git-checkout-b
/// to find which sessions created branches. The branch name is in the
/// companion PreToolUse attestation's tool_input.command.
/// Then collects images from ix-net captures for those sessions.
private HTTPResponse handleImagesByBranch(string branch, string baseDir) {
    import std.file : exists, isDir, dirEntries, SpanMode;
    import ixnet.ats : ATSClient;
    import ixnet.proto : AttestationFilter, Attestation;

    if (!state.atsClient.connected)
        return jsonResponse(503, `{"error":"ATS not connected"}`);

    // Step 1: Find ix-net capture attestations that have image_dir set.
    // These have predicate "captured", source "ix-net", and attributes
    // with session_id and image_dir.
    AttestationFilter captureFilter;
    captureFilter.predicates = ["captured"];
    captureFilter.limit = 100;

    string err;
    auto captures = state.atsClient.getAttestations(captureFilter, err);
    if (err.length > 0)
        return jsonResponse(500, `{"error":"ATS query failed: ` ~ jsonEscape(err) ~ `"}`);

    // Step 2: For each capture, check if its session had a checkout of this branch.
    // Collect session IDs from captures that have images.
    string[] sessionIds;
    string[string] sessionImageDirs; // session_id → image_dir
    foreach (ref cap; captures) {
        auto attrs = decodeStructToStringMap(cap.attributes);
        auto sid = "session_id" in attrs;
        auto imgDir = "image_dir" in attrs;
        if (sid is null || imgDir is null) continue;
        sessionImageDirs[*sid] = *imgDir;
        sessionIds ~= *sid;
    }

    if (sessionIds.length == 0)
        return jsonResponse(200, `{"branch":"` ~ jsonEscape(branch) ~ `","sessions":[]}`);

    // Step 3: Check which sessions had a checkout of this branch.
    // Query PreToolUse attestations for each session that contain the branch name.
    string json = `{"branch":"` ~ jsonEscape(branch) ~ `","sessions":[`;
    bool firstSession = true;

    foreach (sid; sessionIds) {
        AttestationFilter branchFilter;
        branchFilter.predicates = ["PreToolUse"];
        branchFilter.contexts = ["session:" ~ sid];
        branchFilter.limit = 50;

        string berr;
        auto events = state.atsClient.getAttestations(branchFilter, berr);

        // Check if any event has checkout -b <branch> in the raw attributes.
        // tool_input is a nested Struct so we search the raw bytes for the branch name.
        bool found = false;
        foreach (ref ev; events) {
            auto raw = cast(string)ev.attributes;
            if (indexOf(raw, "checkout -b " ~ branch) >= 0 ||
                indexOf(raw, "checkout " ~ branch) >= 0) {
                found = true;
                break;
            }
        }

        if (!found) continue;

        // This session checked out this branch — list its images
        auto imgDir = sessionImageDirs[sid];
        if (!exists(imgDir) || !isDir(imgDir)) continue;

        if (!firstSession) json ~= ",";
        json ~= `{"session":"` ~ jsonEscape(sid) ~ `","images":[`;
        bool firstImg = true;
        foreach (entry; dirEntries(imgDir, SpanMode.shallow)) {
            auto name = entryName(entry.name);
            if (!isImageFile(name)) continue;
            if (!firstImg) json ~= ",";
            json ~= `"` ~ jsonEscape(name) ~ `"`;
            firstImg = false;
        }
        json ~= `]}`;
        firstSession = false;
    }

    json ~= `]}`;
    return jsonResponse(200, json);
}


/// Decode a google.protobuf.Struct (raw bytes) into a flat string[string] map.
/// Only extracts string values — skips numbers, bools, nested structs.
private string[string] decodeStructToStringMap(const(ubyte)[] data) {
    import ixnet.proto : decodeVarint, WireType, skipField;
    string[string] result;
    size_t pos = 0;
    while (pos < data.length) {
        auto tag = decodeVarint(data, pos);
        int fieldNum = cast(int)(tag >> 3);
        WireType wt = cast(WireType)(tag & 0x7);
        if (fieldNum == 1 && wt == WireType.LengthDelimited) {
            // Map entry message
            auto len = cast(size_t)decodeVarint(data, pos);
            auto end = pos + len;
            string k, v;
            while (pos < end) {
                auto etag = decodeVarint(data, pos);
                int eNum = cast(int)(etag >> 3);
                WireType ewt = cast(WireType)(etag & 0x7);
                if (eNum == 1 && ewt == WireType.LengthDelimited) {
                    // key
                    auto klen = cast(size_t)decodeVarint(data, pos);
                    k = cast(string)data[pos .. pos + klen].idup;
                    pos += klen;
                } else if (eNum == 2 && ewt == WireType.LengthDelimited) {
                    // Value message — extract string_value (field 3)
                    auto vlen = cast(size_t)decodeVarint(data, pos);
                    auto vend = pos + vlen;
                    while (pos < vend) {
                        auto vtag = decodeVarint(data, pos);
                        int vNum = cast(int)(vtag >> 3);
                        WireType vwt = cast(WireType)(vtag & 0x7);
                        if (vNum == 3 && vwt == WireType.LengthDelimited) {
                            auto slen = cast(size_t)decodeVarint(data, pos);
                            v = cast(string)data[pos .. pos + slen].idup;
                            pos += slen;
                        } else {
                            skipField(data, pos, vwt);
                        }
                    }
                } else {
                    skipField(data, pos, ewt);
                }
            }
            if (k.length > 0) result[k] = v;
        } else {
            skipField(data, pos, wt);
        }
    }
    return result;
}

/// Check that a path segment contains only safe characters (no traversal).
private bool isSafePath(string s) {
    if (s.length == 0) return false;
    foreach (c; s) {
        if (c == '/' || c == '\\' || c == '\0') return false;
        if (c == '.' && s.length >= 2 && s[0] == '.' && s[1] == '.') return false;
    }
    // Also reject if it starts with ".."
    if (s.length >= 2 && s[0] == '.' && s[1] == '.') return false;
    return true;
}

/// Check if string ends with suffix.
private bool endsWith(string s, string suffix) {
    if (suffix.length > s.length) return false;
    return s[$ - suffix.length .. $] == suffix;
}

/// Extract a query parameter from the request.
/// Looks for "key=value" in the query string portion of the path or body.
private string getQueryParam(ref const HTTPRequest req, string key) {
    // Check if path contains query string
    auto qIdx = indexOf(req.path, "?");
    if (qIdx < 0) return "";
    auto query = req.path[qIdx + 1 .. $];
    return findParam(query, key);
}

private string findParam(string query, string key) {
    size_t pos = 0;
    while (pos < query.length) {
        // Find key=
        auto eqIdx = indexOf(query[pos .. $], "=");
        if (eqIdx < 0) break;
        auto paramName = query[pos .. pos + eqIdx];
        auto valStart = pos + eqIdx + 1;
        // Find end of value (& or end)
        auto ampIdx = indexOf(query[valStart .. $], "&");
        size_t valEnd = ampIdx < 0 ? query.length : valStart + ampIdx;
        if (paramName == key) return query[valStart .. valEnd].idup;
        pos = valEnd + 1;
    }
    return "";
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
    if (auto pp = "proxy_port" in state.config) {
        import std.conv : to;
        try { proxyPort = (*pp).to!ushort; }
        catch (Exception) {}
    }

    string certFile, keyFile;
    resolveCertPaths(certFile, keyFile);

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

/// Resolve leaf cert/key paths relative to the executable.
/// Sets certFile/keyFile to empty strings if not found (passthrough mode).
private void resolveCertPaths(out string certFile, out string keyFile) {
    import ixnet.log;
    certFile = "";
    keyFile = "";
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
