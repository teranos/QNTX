package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// getAxUpgrader creates a WebSocket upgrader with origin checking from config
func getAxUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  2048,
		WriteBufferSize: 2048,
		CheckOrigin:     checkOrigin,
	}
}

// checkOrigin validates WebSocket origin against configured allowed origins
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// Allow requests with no origin header (e.g., direct WebSocket clients, testing)
	if origin == "" {
		return true
	}

	// Load allowed origins from config
	serverCfg, err := appcfg.GetServerConfig()
	if err != nil {
		// If config fails to load, use secure defaults (localhost only)
		return strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "https://localhost")
	}

	// Check if origin matches any of the configured allowed origins
	// We use prefix matching to allow any port number
	for _, allowedOrigin := range serverCfg.AllowedOrigins {
		if strings.HasPrefix(origin, allowedOrigin) {
			return true
		}
	}

	return false
}

// isPortAvailable checks if a port is available for binding
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close() // Error ignored: best-effort port check, caller will retry on actual bind
	return true
}

// findAvailablePort tries to find an available port starting from the requested port
// It tries the requested port first, then tries up to 10 alternative ports (requested+1 to requested+10)
func findAvailablePort(requestedPort int) (int, error) {
	// Try the requested port first
	if isPortAvailable(requestedPort) {
		return requestedPort, nil
	}

	// Preferred fallback ports for development and production
	preferredPorts := []int{appcfg.DefaultGraphPort, appcfg.FallbackGraphPort}

	// Try preferred ports if they differ from requested
	for _, port := range preferredPorts {
		if port != requestedPort && isPortAvailable(port) {
			return port, nil
		}
	}

	// Try high-range fallback ports as last resort (56787-56796)
	fallbackStart := 56787
	for i := 0; i < 10; i++ {
		port := fallbackStart + i
		if isPortAvailable(port) {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports found (tried %d, %d, %d, and range 56787-56796)", requestedPort, appcfg.DefaultGraphPort, appcfg.FallbackGraphPort)
}

// createFileCore creates a zap core for file logging without colors
func createFileCore(path string, verbosity int) (zapcore.Core, error) {
	// Ensure directory exists (os.OpenFile doesn't create intermediate directories)
	dir := "tmp"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", path, err)
	}

	// Create encoder config for plain file output (no colors)
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // No color codes in files
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05.000")
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	encoder := zapcore.NewConsoleEncoder(encoderConfig)
	writer := zapcore.AddSync(file)

	return zapcore.NewCore(encoder, writer, logger.VerbosityToLevel(verbosity)), nil
}
