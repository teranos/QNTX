// qntx-atproto-plugin is an external gRPC plugin for the AT Protocol domain.
//
// This binary runs as a standalone process, communicating with QNTX
// via gRPC. It provides Bluesky/AT Protocol integration including
// authentication, social graph operations, and feed access.
//
// Usage:
//
//	qntx-atproto-plugin --port 9001
//	qntx-atproto-plugin --address localhost:9001
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

	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxatproto "github.com/teranos/QNTX/qntx-atproto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	port     = flag.Int("port", 9001, "gRPC server port")
	address  = flag.String("address", "", "gRPC server address (overrides port)")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	version  = flag.Bool("version", false, "Print version and exit")
)

func main() {
	flag.Parse()

	if *version {
		plugin := qntxatproto.NewPlugin()
		meta := plugin.Metadata()
		fmt.Printf("qntx-atproto-plugin %s\n", meta.Version)
		fmt.Printf("QNTX Version: %s\n", meta.QNTXVersion)
		os.Exit(0)
	}

	logger := setupLogger(*logLevel)
	defer logger.Sync()

	addr := *address
	if addr == "" {
		addr = fmt.Sprintf(":%d", *port)
	}

	plugin := qntxatproto.NewPlugin()

	server := plugingrpc.NewPluginServer(plugin, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Infow("Received shutdown signal", "signal", sig)
		cancel()
	}()

	logger.Infow("Starting QNTX AT Protocol Domain Plugin",
		"version", plugin.Metadata().Version,
		"address", addr,
	)

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
