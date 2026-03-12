/// faal plugin implementation — configurable failure modes for chaos testing.
///
/// Failure modes are selected via the "failure_mode" config key during Initialize.
/// The plugin boots normally so QNTX can connect, then misbehaves as configured.
module faal.plugin;

import faal.proto;
import faal.grpc;
import faal.log;
import faal.version_ : PLUGIN_NAME, PLUGIN_VERSION;

import core.thread : Thread;
import core.time : dur;
import core.stdc.stdlib : exit;

// ---------------------------------------------------------------------------
// Failure modes
// ---------------------------------------------------------------------------

enum FailureMode {
    none,                // behave normally
    crash_once,          // crash on first launch, work on second (disk marker)
    crash_before_health, // exit before Health can respond
    crash_after_health,  // respond to Health once, then exit on next RPC
    hang_on_initialize,  // block forever on Initialize
    slow_responses,      // delay every RPC response
    bad_metadata,        // return corrupt Metadata
    random,              // pick a random misbehavior per RPC
}

// ---------------------------------------------------------------------------
// Plugin state
// ---------------------------------------------------------------------------

struct PluginState {
    FailureMode mode = FailureMode.none;
    int delayMs = 3000;          // delay for slow_responses mode
    bool healthResponded = false; // tracks first health response for crash_after_health
    bool initialized = false;
    string[string] config;
    int rpcCount = 0;            // total RPCs served, for random mode seeding
}

private __gshared PluginState state;

// ---------------------------------------------------------------------------
// Mode parsing
// ---------------------------------------------------------------------------

private FailureMode parseMode(string s) {
    if (s == "none")                return FailureMode.none;
    if (s == "crash_once")          return FailureMode.crash_once;
    if (s == "crash_before_health") return FailureMode.crash_before_health;
    if (s == "crash_after_health")  return FailureMode.crash_after_health;
    if (s == "hang_on_initialize")  return FailureMode.hang_on_initialize;
    if (s == "slow_responses")      return FailureMode.slow_responses;
    if (s == "bad_metadata")        return FailureMode.bad_metadata;
    if (s == "random")              return FailureMode.random;
    return FailureMode.none;
}

// ---------------------------------------------------------------------------
// Random misbehavior (for fuzzing mode)
// ---------------------------------------------------------------------------

/// Simple deterministic "random" based on rpcCount — no imports needed.
private FailureMode randomFailure() {
    // Skip none and random itself from the options
    FailureMode[] options = [
        FailureMode.crash_before_health,
        FailureMode.crash_after_health,
        FailureMode.slow_responses,
        FailureMode.bad_metadata,
    ];
    auto idx = state.rpcCount % options.length;
    return options[idx];
}

/// Apply pre-RPC failure effects. Returns true if the RPC should be skipped.
private bool applyFailure(string rpcName) {
    state.rpcCount++;
    auto mode = state.mode;

    if (mode == FailureMode.random) {
        mode = randomFailure();
        logInfo("[faal] random mode selected %s for RPC %s (#%d)",
                modeStr(mode), rpcName, state.rpcCount);
    }

    final switch (mode) {
        case FailureMode.none:
            return false;

        case FailureMode.crash_once:
            // Handled at startup in initialize(), not per-RPC
            return false;

        case FailureMode.crash_before_health:
            if (rpcName == "Health") {
                logInfo("[faal] crash_before_health: exiting before Health response");
                exit(1);
            }
            return false;

        case FailureMode.crash_after_health:
            if (rpcName == "Health" && !state.healthResponded) {
                state.healthResponded = true;
                return false; // allow first health check
            }
            if (state.healthResponded && rpcName != "Metadata") {
                logInfo("[faal] crash_after_health: exiting on RPC %s after first health", rpcName);
                exit(1);
            }
            return false;

        case FailureMode.hang_on_initialize:
            if (rpcName == "Initialize") {
                logInfo("[faal] hang_on_initialize: blocking forever");
                while (true) {
                    Thread.sleep(dur!"hours"(1));
                }
            }
            return false;

        case FailureMode.slow_responses:
            logInfo("[faal] slow_responses: delaying %dms before %s", state.delayMs, rpcName);
            Thread.sleep(dur!"msecs"(state.delayMs));
            return false;

        case FailureMode.bad_metadata:
            return false; // handled in metadata() directly

        case FailureMode.random:
            return false; // already resolved above
    }
}

private string modeStr(FailureMode m) {
    final switch (m) {
        case FailureMode.none:                return "none";
        case FailureMode.crash_once:          return "crash_once";
        case FailureMode.crash_before_health: return "crash_before_health";
        case FailureMode.crash_after_health:  return "crash_after_health";
        case FailureMode.hang_on_initialize:  return "hang_on_initialize";
        case FailureMode.slow_responses:      return "slow_responses";
        case FailureMode.bad_metadata:        return "bad_metadata";
        case FailureMode.random:              return "random";
    }
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

MetadataResponse metadata() {
    applyFailure("Metadata");

    MetadataResponse resp;

    if (state.mode == FailureMode.bad_metadata) {
        // Return garbage — wrong name, empty version, nonsense fields
        resp.name        = "\xFF\xFE\x00garbage";
        resp.version_    = "";
        resp.qntxVersion = ">=999.999.999";
        resp.description = "\x00\x01\x02\x03";
        resp.author      = "";
        resp.license     = "";
        logInfo("[faal] bad_metadata: returning corrupt Metadata");
        return resp;
    }

    resp.name        = PLUGIN_NAME;
    resp.version_    = PLUGIN_VERSION;
    resp.qntxVersion = ">= 0.1.0";
    resp.description = "Chaos testing plugin — configurable failure modes";
    resp.author      = "QNTX";
    resp.license     = "MIT";
    return resp;
}

// ---------------------------------------------------------------------------
// Initialize / Shutdown
// ---------------------------------------------------------------------------

InitializeResponse initialize(ref const InitializeRequest req) {
    state.config = cast(string[string])req.config;

    // Read failure mode from config
    if ("failure_mode" in state.config) {
        state.mode = parseMode(state.config["failure_mode"]);
    }

    // Read delay for slow_responses mode
    if ("delay_ms" in state.config) {
        import std.conv : to;
        try {
            state.delayMs = state.config["delay_ms"].to!int;
        } catch (Exception) {
            state.delayMs = 3000;
        }
    }

    logInfo("[faal] Initialize: mode=%s delay=%dms", modeStr(state.mode), state.delayMs);

    // crash_once: crash on first launch, work on second
    if (state.mode == FailureMode.crash_once) {
        import std.file : exists, write, remove;
        enum marker = "/tmp/faal-crash-once";
        if (!exists(marker)) {
            write(marker, "1");
            logInfo("[faal] crash_once: first launch, wrote marker, crashing");
            exit(1);
        } else {
            remove(marker);
            logInfo("[faal] crash_once: second launch, marker found, proceeding normally");
        }
    }

    applyFailure("Initialize");

    state.initialized = true;

    InitializeResponse resp;
    return resp;
}

void shutdown() {
    logInfo("[faal] Shutdown");
    state.initialized = false;
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

HealthResponse health() {
    applyFailure("Health");

    HealthResponse resp;
    resp.healthy = true;
    resp.message = "faal mode=" ~ modeStr(state.mode);
    resp.details["version"] = PLUGIN_VERSION;
    resp.details["mode"]    = modeStr(state.mode);
    resp.details["rpcs"]    = intToStr(state.rpcCount);
    return resp;
}

// ---------------------------------------------------------------------------
// HTTP request handling
// ---------------------------------------------------------------------------

HTTPResponse handleHTTP(ref const HTTPRequest req) {
    applyFailure("HandleHTTP");

    auto path = req.path;

    if (req.method == "GET" && path == "/status") {
        auto json = `{"name":"` ~ PLUGIN_NAME ~
                    `","version":"` ~ PLUGIN_VERSION ~
                    `","mode":"` ~ modeStr(state.mode) ~
                    `","rpcs":` ~ intToStr(state.rpcCount) ~ `}`;
        return jsonResponse(200, json);
    }

    HTTPResponse resp;
    resp.statusCode = 404;
    resp.body_ = cast(ubyte[])(`{"error":"not found: ` ~ path ~ `"}`);
    resp.headers = [httpHeader("Content-Type", "application/json")];
    return resp;
}

// ---------------------------------------------------------------------------
// Glyph definition (none — faal is headless)
// ---------------------------------------------------------------------------

GlyphDefResponse registerGlyphs() {
    GlyphDefResponse resp;
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
        resp.fields["failure_mode"] = "none|crash_once|crash_before_health|crash_after_health|hang_on_initialize|slow_responses|bad_metadata|random";
        resp.fields["delay_ms"]     = "Delay in milliseconds for slow_responses mode (default: 3000)";
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/RegisterGlyphs", (const ubyte[] _) {
        auto resp = registerGlyphs();
        return encode(resp);
    });

    server.registerHandler("/protocol.DomainPluginService/ExecuteJob", (const ubyte[] data) {
        applyFailure("ExecuteJob");
        ExecuteJobResponse resp;
        resp.success = false;
        resp.error = "faal does not support jobs";
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

/// int to string without std.conv (avoids template bloat in hot path).
private string intToStr(int n) {
    import std.conv : to;
    return n.to!string;
}
