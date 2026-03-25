import Foundation
import GRPC
import NIOCore
import NIOPosix

@main
struct SwiftMetalPluginMain {
    static func main() throws {
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

        // Signal handling
        signal(SIGINT) { _ in exit(0) }
        signal(SIGTERM) { _ in exit(0) }

        let group = MultiThreadedEventLoopGroup(numberOfThreads: System.coreCount)
        defer { try? group.syncShutdownGracefully() }

        let plugin = SwiftMetalPlugin()

        // Try to bind, retry up to 64 times
        var server: Server?
        var boundPort = 0

        for attempt in 0..<64 {
            let tryPort = port + attempt
            do {
                server = try Server.insecure(group: group)
                    .withServiceProviders([plugin])
                    .bind(host: "127.0.0.1", port: tryPort)
                    .wait()
                boundPort = tryPort
                break
            } catch {
                continue
            }
        }

        guard let server = server else {
            fputs("Failed to bind to any port in range \(port)-\(port + 63)\n", stderr)
            Foundation.exit(1)
        }

        // Port announcement — core discovers us via this line
        print("QNTX_PLUGIN_PORT=\(boundPort)")
        fflush(stdout)

        try server.onClose.wait()
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
