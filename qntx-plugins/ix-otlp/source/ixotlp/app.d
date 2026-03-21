/// qntx-ix-otlp-plugin entry point.
///
/// Receives OpenTelemetry trace exports (OTLP/HTTP JSON) and persists
/// each span as an OTLPSpan attestation in ATS. Loom weaves these into
/// embedding-ready text blocks on startup or import.
module ixotlp.app;

import ixotlp.grpc;
import ixotlp.plugin;
import ixotlp.proto;
import ixotlp.version_ : PLUGIN_VERSION;

import std.conv : convTo = to;
import std.stdio : stdout, writeln, writefln;
import ixotlp.log;

void main(string[] args) {
    ushort port = 9020;
    bool showVersion = false;

    for (size_t i = 1; i < args.length; i++) {
        if (args[i] == "--port" && i + 1 < args.length) {
            port = convTo!ushort(args[i + 1]);
            i++;
        } else if (args[i] == "--address" && i + 1 < args.length) {
            auto addr = args[i + 1];
            auto colonIdx = lastIndexOf(addr, ':');
            if (colonIdx >= 0) {
                port = convTo!ushort(addr[colonIdx + 1 .. $]);
            }
            i++;
        } else if (args[i] == "--version") {
            showVersion = true;
        } else if (args[i] == "--log-level" && i + 1 < args.length) {
            i++;
        }
    }

    if (showVersion) {
        auto meta = metadata();
        writefln("qntx-%s-plugin %s", meta.name, meta.version_);
        writefln("QNTX Version: %s", meta.qntxVersion);
        stdout.flush();
        return;
    }

    GrpcServer server;
    registerHandlers(server);

    auto actualPort = server.bind(port);
    if (actualPort == 0) {
        logError("[ix-otlp] failed to bind to port %d (tried 64 ports)", port);
        return;
    }

    writefln("QNTX_PLUGIN_PORT=%d", actualPort);
    stdout.flush();

    logInfo("[ix-otlp] gRPC server listening on 127.0.0.1:%d", actualPort);
    logInfo("[ix-otlp] OTLP trace ingestion plugin v%s ready", PLUGIN_VERSION);

    server.serve();
}

private ptrdiff_t lastIndexOf(string s, char c) {
    for (ptrdiff_t i = cast(ptrdiff_t)s.length - 1; i >= 0; i--) {
        if (s[i] == c) return i;
    }
    return -1;
}
