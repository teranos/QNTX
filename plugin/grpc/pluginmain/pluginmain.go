// Package pluginmain provides the shared entrypoint for QNTX gRPC plugins.
//
// Each plugin binary's main.go reduces to:
//
//	func main() {
//	    pluginmain.Run(qntxcode.NewPlugin(), 9000)
//	}
package pluginmain

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Run starts a gRPC plugin server for the given DomainPlugin.
// defaultPort is the port used when --address is not specified.
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

	logger := setupLogger(*logLevel)
	defer logger.Sync()

	addr := *address
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", *port)
	}

	server := plugingrpc.NewPluginServer(p, logger)

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
