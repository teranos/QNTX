#include <csignal>
#include <cstdlib>
#include <iostream>
#include <memory>
#include <string>

#include <grpcpp/grpcpp.h>

#include "plugin.h"
#include "log_capture.h"

static std::unique_ptr<grpc::Server> g_server;

static void signal_handler(int signum) {
    if (g_server) {
        g_server->Shutdown();
    }
}

static void print_usage(const char* prog) {
    std::cerr << "Usage: " << prog << " [options]\n"
              << "  --port N        Base port (default 50100)\n"
              << "  --model PATH    Path to GGUF model file\n"
              << "  --log-level LVL Log level: debug|info|warn|error\n"
              << "  --version       Print version and exit\n";
}

int main(int argc, char* argv[]) {
    int port = 50100;
    std::string model_path;
    std::string log_level = "info";

    for (int i = 1; i < argc; i++) {
        std::string arg = argv[i];
        if (arg == "--port" && i + 1 < argc) {
            port = std::atoi(argv[++i]);
        } else if (arg == "--model" && i + 1 < argc) {
            model_path = argv[++i];
        } else if (arg == "--log-level" && i + 1 < argc) {
            log_level = argv[++i];
        } else if (arg == "--version") {
            std::cout << "qntx-llama-cpp " << PLUGIN_VERSION << std::endl;
            return 0;
        } else if (arg == "--help") {
            print_usage(argv[0]);
            return 0;
        }
    }

    // Install log capture before any llama.cpp calls
    LogCapture::instance().install();

    // Signal handling for graceful shutdown
    std::signal(SIGINT, signal_handler);
    std::signal(SIGTERM, signal_handler);

    // Create plugin service
    auto plugin_service = std::make_unique<LlamaCppPlugin>();
    auto llm_service = std::make_unique<LlamaCppLLMService>(plugin_service.get());

    // Try to bind to port, retry up to 64 times
    std::string server_address;
    int bound_port = 0;

    for (int attempt = 0; attempt < 64; attempt++) {
        int try_port = port + attempt;
        server_address = "127.0.0.1:" + std::to_string(try_port);

        grpc::ServerBuilder builder;
        builder.AddListeningPort(server_address, grpc::InsecureServerCredentials(), &bound_port);
        builder.SetMaxReceiveMessageSize(64 * 1024 * 1024); // 64 MB — PDF attachments via data URI
        builder.RegisterService(plugin_service.get());
        builder.RegisterService(llm_service.get());

        g_server = builder.BuildAndStart();
        if (g_server && bound_port > 0) {
            break;
        }
    }

    if (!g_server) {
        std::cerr << "Failed to bind to any port in range "
                  << port << "-" << (port + 63) << std::endl;
        return 1;
    }

    // Port announcement — core discovers us via this line
    std::cout << "QNTX_PLUGIN_PORT=" << bound_port << std::endl;
    std::cout.flush();

    g_server->Wait();
    return 0;
}
