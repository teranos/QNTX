/// qntx-ix-bin-plugin entry point.
///
/// Parses CLI flags, starts the gRPC server, and outputs the
/// QNTX_PLUGIN_PORT=N announcement for the plugin manager.
module ixbin.app;

import ixbin.grpc;
import ixbin.plugin;
import ixbin.proto;

import std.conv : convTo = to;
import std.stdio : stdout, stderr, writeln, writefln;

void main(string[] args) {
    ushort port = 9005;
    bool showVersion = false;

    // Parse CLI args (--port N, --address host:port, --version, --log-level)
    for (size_t i = 1; i < args.length; i++) {
        if (args[i] == "--port" && i + 1 < args.length) {
            port = convTo!ushort(args[i + 1]);
            i++;
        } else if (args[i] == "--address" && i + 1 < args.length) {
            // Extract port from address (host:port)
            auto addr = args[i + 1];
            auto colonIdx = lastIndexOf(addr, ':');
            if (colonIdx >= 0) {
                port = convTo!ushort(addr[colonIdx + 1 .. $]);
            }
            i++;
        } else if (args[i] == "--version") {
            showVersion = true;
        } else if (args[i] == "--log-level" && i + 1 < args.length) {
            i++; // accepted but ignored for now
        }
    }

    if (showVersion) {
        auto meta = metadata();
        writefln("qntx-%s-plugin %s", meta.name, meta.version_);
        writefln("QNTX Version: %s", meta.qntxVersion);
        stdout.flush();
        return;
    }

    // Create and configure gRPC server
    GrpcServer server;
    registerHandlers(server);

    // Bind to port
    auto actualPort = server.bind(port);
    if (actualPort == 0) {
        stderr.writefln("failed to bind to port %d (tried 64 ports)", port);
        return;
    }

    // Output port announcement for QNTX plugin manager
    // Must go to stdout and flush immediately — PluginManager reads this line
    writefln("QNTX_PLUGIN_PORT=%d", actualPort);
    stdout.flush();

    stderr.writefln("[ix-bin] gRPC server listening on 127.0.0.1:%d", actualPort);
    stderr.writefln("[ix-bin] Binary ingestion plugin v%s ready", PLUGIN_VERSION);

    // Serve (blocks until shutdown)
    server.serve();
}

/// Find last occurrence of a character.
private ptrdiff_t lastIndexOf(string s, char c) {
    for (ptrdiff_t i = cast(ptrdiff_t)s.length - 1; i >= 0; i--) {
        if (s[i] == c) return i;
    }
    return -1;
}
