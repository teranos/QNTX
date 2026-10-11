package grpc

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Run is the standard entry point for a QNTX plugin binary.
// It handles flag parsing, logger setup, signal handling, and gRPC server lifecycle.
//
// Usage in main.go:
//
//	func main() {
//	    plugingrpc.Run(myplugin.NewPlugin(), 9002)
//	}
func Run(p plugin.DomainPlugin, defaultPort int) {
	port := flag.Int("port", defaultPort, "gRPC server port")
	address := flag.String("address", "", "gRPC server address (overrides port)")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	version := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	meta := p.Metadata()

	if *version {
		fmt.Printf("qntx-%s-plugin %s\n", meta.Name, meta.Version)
		fmt.Printf("QNTX Version: %s\n", meta.QNTXVersion)
		os.Exit(0)
	}

	// No Sync defer: SetupLogger builds a stderr sink, which needs no flush —
	// Sync on a terminal answers ENOTTY, an error that means nothing here.
	logger := SetupLogger(*logLevel)

	// The address a plugin serves on is the one it was told; told only a port,
	// it serves on that port of the loopback.
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	if given["address"] {
		if given["port"] {
			logger.Errorw("Told both an address and a port; serve on one", "address", *address, "port", *port)
			os.Exit(2)
		}
		addr = *address
	}

	server := NewPluginServer(p, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Infow("Received shutdown signal", "signal", sig)
		cancel()
	}()

	logger.Infow(fmt.Sprintf("Starting QNTX %s plugin", meta.Name),
		"version", meta.Version,
		"address", addr,
	)

	if err := server.Serve(ctx, addr); err != nil {
		logger.Errorw("Server error", "error", err)
		os.Exit(1)
	}

	logger.Info("Plugin shutdown complete")
}

// SetupLogger creates a zap SugaredLogger with the given log level. A level
// zap does not know stops the plugin: it is not read as info.
func SetupLogger(level string) *zap.SugaredLogger {
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Log level %q is not one of debug, info, warn, error: %v\n", level, err)
		os.Exit(2)
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
