import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2

@main
struct SwiftMetalPluginMain {
    static func main() async throws {
        let args = CommandLine.arguments
        var port = 50200
        var logLevel = "info"

        var i = 1
        while i < args.count {
            switch args[i] {
            case "--port":
                i += 1
                if i < args.count, let p = Int(args[i]) { port = p }
            case "--log-level":
                i += 1
                if i < args.count { logLevel = args[i] }
            case "--version":
                print("qntx-swift-metal \(pluginVersion)")
                return
            case "--help":
                printUsage()
                return
            default:
                break
            }
            i += 1
        }

        let plugin = SwiftMetalPlugin()

        // Try ports in range until one binds
        var boundPort = 0
        for attempt in 0..<64 {
            let tryPort = port + attempt
            let transport = HTTP2ServerTransport.Posix(
                address: .ipv4(host: "127.0.0.1", port: tryPort),
                transportSecurity: .plaintext
            )
            let server = GRPCServer(
                transport: transport,
                services: [plugin]
            )

            do {
                try await withThrowingTaskGroup(of: Void.self) { group in
                    group.addTask { try await server.serve() }
                    // If we get a listening address, the port bound successfully
                    if let _ = try await server.listeningAddress {
                        boundPort = tryPort
                        // Port announcement — core discovers us via this line
                        print("QNTX_PLUGIN_PORT=\(boundPort)")
                        fflush(stdout)
                        // Keep serving — don't cancel
                        try await group.next()
                    }
                }
                return // Server shut down cleanly
            } catch {
                // Port likely in use, try next
                continue
            }
        }

        fputs("Failed to bind to any port in range \(port)-\(port + 63)\n", stderr)
        Foundation.exit(1)
    }

    static func printUsage() {
        fputs("""
        Usage: qntx-swift-metal-plugin [options]
          --port N        Base port (default 50200)
          --log-level LVL Log level: debug|info|warn|error
          --version       Print version and exit

        """, stderr)
    }
}
