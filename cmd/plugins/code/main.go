// qntx-code-plugin is an external gRPC plugin for the code domain.
//
// This binary can run as a standalone process, communicating with QNTX
// via gRPC. It provides the same functionality as the built-in code domain
// but runs in a separate process for isolation and independent deployment.
//
// Usage:
//
//	qntx-code-plugin --port 9000
//	qntx-code-plugin --address localhost:9000
//
// The plugin will start a gRPC server and wait for QNTX to connect.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/teranos/QNTX/qntx-code"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	port     = flag.Int("port", 9000, "gRPC server port")
	address  = flag.String("address", "", "gRPC server address (overrides port)")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	version  = flag.Bool("version", false, "Print version and exit")
)

func main() {
	flag.Parse()

	if *version {
		plugin := qntxcode.NewPlugin()
		meta := plugin.Metadata()
		fmt.Printf("qntx-code-plugin %s\n", meta.Version)
		fmt.Printf("QNTX Version: %s\n", meta.QNTXVersion)
		os.Exit(0)
	}

	// Set up logger
	logger := setupLogger(*logLevel)
	defer logger.Sync()

	// Determine server address
	addr := *address
	if addr == "" {
		addr = fmt.Sprintf(":%d", *port)
	}

	// Create the code domain plugin
	plugin := qntxcode.NewPlugin()

	// Create the gRPC server wrapper
	server := plugingrpc.NewPluginServer(plugin, logger)

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Infow("Received shutdown signal", "signal", sig)
		cancel()
	}()

	logger.Infow("Starting QNTX Code Domain Plugin",
		"version", plugin.Metadata().Version,
		"address", addr,
	)

	// Start the gRPC server
	if err := server.Serve(ctx, addr); err != nil {
		logger.Errorw("Server error", "error", err)
		os.Exit(1)
	}

	logger.Info("Plugin shutdown complete")
}

func setupLogger(level string) *zap.SugaredLogger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := config.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	return logger.Sugar()
}
