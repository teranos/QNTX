// Package grpc provides gRPC transport for external domain plugins.
//
// This package enables QNTX domain plugins to run as separate processes,
// communicating with the main QNTX server via gRPC. This provides:
//   - Process isolation for plugin failures
//   - Language-agnostic plugins (any gRPC-compatible language)
//   - Third-party plugins without recompiling QNTX
package grpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// PluginServer wraps a DomainPlugin to expose it via gRPC.
// This is used by external plugins to serve their implementation.
type PluginServer struct {
	protocol.UnimplementedDomainPluginServiceServer

	plugin   plugin.DomainPlugin
	services plugin.ServiceRegistry
	logger   *zap.SugaredLogger

	// HTTP mux for handling HTTP requests via gRPC
	httpMux *http.ServeMux

	// Command registry for CLI execution
	commands map[string]*cobra.Command
}

// NewPluginServer creates a new gRPC server wrapper for a DomainPlugin.
func NewPluginServer(plugin plugin.DomainPlugin, logger *zap.SugaredLogger) *PluginServer {
	return &PluginServer{
		plugin:   plugin,
		logger:   logger,
		httpMux:  http.NewServeMux(),
		commands: make(map[string]*cobra.Command),
	}
}

// Serve starts the gRPC server on the given address.
func (s *PluginServer) Serve(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	grpcServer := grpc.NewServer()
	protocol.RegisterDomainPluginServiceServer(grpcServer, s)

	s.logger.Infow("Starting gRPC plugin server", "address", addr)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		s.logger.Info("Shutting down gRPC server")
		grpcServer.GracefulStop()
	}()

	if err := grpcServer.Serve(listener); err != nil {
		return fmt.Errorf("gRPC server error: %w", err)
	}

	return nil
}

// Metadata returns plugin metadata.
func (s *PluginServer) Metadata(ctx context.Context, _ *protocol.Empty) (*protocol.MetadataResponse, error) {
	meta := s.plugin.Metadata()
	return &protocol.MetadataResponse{
		Name:        meta.Name,
		Version:     meta.Version,
		QntxVersion: meta.QNTXVersion,
		Description: meta.Description,
		Author:      meta.Author,
		License:     meta.License,
	}, nil
}

// Initialize initializes the plugin with configuration.
func (s *PluginServer) Initialize(ctx context.Context, req *protocol.InitializeRequest) (*protocol.Empty, error) {
	// Create a remote service registry that connects back to QNTX
	s.services = NewRemoteServiceRegistry(
		req.DatabaseEndpoint,
		req.AtsStoreEndpoint,
		req.Config,
		s.logger,
	)

	// Initialize the plugin
	if err := s.plugin.Initialize(ctx, s.services); err != nil {
		return nil, fmt.Errorf("plugin %s initialization failed: %w", s.plugin.Metadata().Name, err)
	}

	// Register HTTP handlers
	if err := s.plugin.RegisterHTTP(s.httpMux); err != nil {
		return nil, fmt.Errorf("HTTP registration failed for plugin %s: %w", s.plugin.Metadata().Name, err)
	}

	// Build command registry
	for _, cmd := range s.plugin.Commands() {
		s.registerCommand("", cmd)
	}

	return &protocol.Empty{}, nil
}

// registerCommand recursively registers commands in the command map.
func (s *PluginServer) registerCommand(prefix string, cmd *cobra.Command) {
	name := cmd.Use
	if prefix != "" {
		name = prefix + " " + cmd.Use
	}
	// Strip arguments from command name (e.g., "ix git <repo>" -> "ix git")
	parts := strings.Fields(name)
	cmdPath := strings.Join(parts, " ")
	for i, p := range parts {
		if strings.HasPrefix(p, "<") || strings.HasPrefix(p, "[") {
			cmdPath = strings.Join(parts[:i], " ")
			break
		}
	}

	s.commands[cmdPath] = cmd

	for _, sub := range cmd.Commands() {
		s.registerCommand(cmdPath, sub)
	}
}

// Shutdown shuts down the plugin.
func (s *PluginServer) Shutdown(ctx context.Context, _ *protocol.Empty) (*protocol.Empty, error) {
	if err := s.plugin.Shutdown(ctx); err != nil {
		return nil, fmt.Errorf("plugin %s shutdown failed: %w", s.plugin.Metadata().Name, err)
	}
	return &protocol.Empty{}, nil
}

// Commands returns CLI command definitions.
func (s *PluginServer) Commands(ctx context.Context, _ *protocol.Empty) (*protocol.CommandsResponse, error) {
	cmds := s.plugin.Commands()
	response := &protocol.CommandsResponse{
		Commands: make([]*protocol.CommandDefinition, 0, len(cmds)),
	}

	for _, cmd := range cmds {
		def := s.buildCommandDefinition(cmd)
		response.Commands = append(response.Commands, def)
	}

	return response, nil
}

// buildCommandDefinition recursively builds command definitions.
func (s *PluginServer) buildCommandDefinition(cmd *cobra.Command) *protocol.CommandDefinition {
	def := &protocol.CommandDefinition{
		Name:        cmd.Use,
		Description: cmd.Short,
		Subcommands: make([]string, 0),
		Flags:       make([]*protocol.FlagDefinition, 0),
	}

	// Add subcommand names
	for _, sub := range cmd.Commands() {
		def.Subcommands = append(def.Subcommands, sub.Use)
	}

	// Add flag definitions
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		flagDef := &protocol.FlagDefinition{
			Name:         flag.Name,
			Short:        flag.Shorthand,
			Description:  flag.Usage,
			Type:         flag.Value.Type(),
			DefaultValue: flag.DefValue,
		}
		def.Flags = append(def.Flags, flagDef)
	})

	return def
}

// ExecuteCommand executes a CLI command.
func (s *PluginServer) ExecuteCommand(ctx context.Context, req *protocol.ExecuteCommandRequest) (*protocol.ExecuteCommandResponse, error) {
	// Find the command
	cmd, ok := s.commands[req.Command]
	if !ok {
		return &protocol.ExecuteCommandResponse{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("command not found: %s", req.Command),
		}, nil
	}

	// Set up output capture
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	// Set flags from request
	for name, value := range req.Flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			return &protocol.ExecuteCommandResponse{
				ExitCode: 1,
				Stderr:   fmt.Sprintf("invalid flag %s: %v", name, err),
			}, nil
		}
	}

	// Execute command
	cmd.SetArgs(req.Args)
	err := cmd.ExecuteContext(ctx)

	exitCode := int32(0)
	if err != nil {
		exitCode = 1
		stderr.WriteString(err.Error())
	}

	return &protocol.ExecuteCommandResponse{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// HandleHTTP handles an HTTP request via gRPC.
func (s *PluginServer) HandleHTTP(ctx context.Context, req *protocol.HTTPRequest) (*protocol.HTTPResponse, error) {
	// Create an HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	// Create a response recorder
	recorder := httptest.NewRecorder()

	// Serve the request
	s.httpMux.ServeHTTP(recorder, httpReq)

	// Build response
	result := recorder.Result()
	defer result.Body.Close()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	headers := make(map[string]string)
	for key := range result.Header {
		headers[key] = result.Header.Get(key)
	}

	return &protocol.HTTPResponse{
		StatusCode: int32(result.StatusCode),
		Headers:    headers,
		Body:       body,
	}, nil
}

// HandleWebSocket handles a WebSocket connection via gRPC streaming.
func (s *PluginServer) HandleWebSocket(stream protocol.DomainPluginService_HandleWebSocketServer) error {
	// WebSocket handling via gRPC streaming
	// This bridges WebSocket connections to the plugin's WebSocket handlers
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch msg.Type {
		case protocol.WebSocketMessage_CONNECT:
			s.logger.Debug("WebSocket connection established via gRPC")
		case protocol.WebSocketMessage_DATA:
			// TODO: Route to appropriate WebSocket handler
			s.logger.Debugw("WebSocket data received", "size", len(msg.Data))
		case protocol.WebSocketMessage_CLOSE:
			s.logger.Debug("WebSocket connection closed")
			return nil
		}
	}
}

// Health checks plugin health.
func (s *PluginServer) Health(ctx context.Context, _ *protocol.Empty) (*protocol.HealthResponse, error) {
	status := s.plugin.Health(ctx)

	details := make(map[string]string)
	for key, value := range status.Details {
		details[key] = fmt.Sprintf("%v", value)
	}

	return &protocol.HealthResponse{
		Healthy: status.Healthy,
		Message: status.Message,
		Details: details,
	}, nil
}
