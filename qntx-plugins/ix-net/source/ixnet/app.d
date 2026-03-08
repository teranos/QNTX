/// qntx-ix-net-plugin entry point.
///
/// Parses CLI flags, starts the gRPC server, and outputs the
/// QNTX_PLUGIN_PORT=N announcement for the plugin manager.
module ixnet.app;

import ixnet.grpc;
import ixnet.plugin;
import ixnet.proto;
import ixnet.version_ : PLUGIN_VERSION;

import std.conv : convTo = to;
import std.stdio : stdout, writeln, writefln;
import ixnet.log;

void main(string[] args) {
    ushort port = 9010;
    bool showVersion = false;
    bool standalone = false;
    ushort proxyPort = 9100;

    // Parse CLI args (--port N, --address host:port, --version, --log-level, --standalone)
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
        } else if (args[i] == "--standalone") {
            standalone = true;
        } else if (args[i] == "--proxy-port" && i + 1 < args.length) {
            proxyPort = convTo!ushort(args[i + 1]);
            i++;
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

    // Standalone mode: start proxy directly without gRPC (for testing)
    if (standalone) {
        import ixnet.proxy;
        import ixnet.tls;
        import std.file : thisExePath;
        import core.thread : Thread;
        import core.time : dur;

        // Resolve certs relative to executable
        auto exePath = thisExePath();
        string exeDir = "";
        for (ptrdiff_t i = cast(ptrdiff_t)exePath.length - 1; i >= 0; i--) {
            if (exePath[i] == '/') { exeDir = cast(string)exePath[0 .. i]; break; }
        }

        string certFile = exeDir ~ "/../certs/leaf.pem";
        string keyFile = exeDir ~ "/../certs/leaf.key";

        import std.file : exists;
        if (!exists(certFile) || !exists(keyFile)) {
            logError("[ix-net] standalone: certs not found at %s/../certs/", exeDir);
            logError("[ix-net] standalone: run: cd certs && sh generate.sh");
            return;
        }

        ProxyState proxy;
        if (!startProxy(proxy, proxyPort, certFile, keyFile)) {
            logError("[ix-net] standalone: failed to start proxy on port %d", proxyPort);
            return;
        }

        writefln("ix-net standalone proxy on port %d (mode=%s)",
                 proxy.proxyPort, proxy.tlsEnabled ? "intercept" : "passthrough");
        writefln("Test with:");
        writefln("  HTTPS_PROXY=http://localhost:%d curl --cacert certs/ca.pem https://api.anthropic.com/v1/messages",
                 proxy.proxyPort);
        stdout.flush();

        // Block until interrupted
        while (proxy.running) {
            Thread.sleep(dur!"seconds"(1));
        }
        return;
    }

    // Create and configure gRPC server
    GrpcServer server;
    registerHandlers(server);

    // Bind to port
    auto actualPort = server.bind(port);
    if (actualPort == 0) {
        logError("[ix-net] failed to bind to port %d (tried 64 ports)", port);
        return;
    }

    // Output port announcement for QNTX plugin manager
    writefln("QNTX_PLUGIN_PORT=%d", actualPort);
    stdout.flush();

    logInfo("[ix-net] gRPC server listening on 127.0.0.1:%d", actualPort);
    logInfo("[ix-net] Network capture plugin v%s ready", PLUGIN_VERSION);

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
