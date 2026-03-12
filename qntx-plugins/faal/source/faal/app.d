/// qntx-faal-plugin entry point.
///
/// Parses CLI flags, starts the gRPC server, and outputs the
/// QNTX_PLUGIN_PORT=N announcement for the plugin manager.
module faal.app;

import faal.grpc;
import faal.plugin;
import faal.proto;
import faal.version_ : PLUGIN_VERSION;

import std.conv : convTo = to;
import std.stdio : stdout, writefln;
import faal.log;

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
            i++; // accepted but ignored
        }
    }

    if (showVersion) {
        auto meta = metadata();
        writefln("qntx-%s-plugin %s", meta.name, meta.version_);
        stdout.flush();
        return;
    }

    GrpcServer server;
    registerHandlers(server);

    auto actualPort = server.bind(port);
    if (actualPort == 0) {
        logError("[faal] could not bind to port %d (tried 64 ports)", port);
        return;
    }

    writefln("QNTX_PLUGIN_PORT=%d", actualPort);
    stdout.flush();

    logInfo("[faal] gRPC server listening on 127.0.0.1:%d", actualPort);
    logInfo("[faal] Chaos testing plugin v%s ready", PLUGIN_VERSION);

    server.serve();
}

/// Find last occurrence of a character.
private ptrdiff_t lastIndexOf(string s, char c) {
    for (ptrdiff_t i = cast(ptrdiff_t)s.length - 1; i >= 0; i--) {
        if (s[i] == c) return i;
    }
    return -1;
}
