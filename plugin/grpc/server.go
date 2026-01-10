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
	"sync"

	"github.com/teranos/QNTX/errors"
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

	// initOnce ensures Initialize is only executed once
	initOnce sync.Once
	initErr  error
}

// NewPluginServer creates a new gRPC server wrapper for a DomainPlugin.
func NewPluginServer(plugin plugin.DomainPlugin, logger *zap.SugaredLogger) *PluginServer {
	return &PluginServer{
		plugin:  plugin,
		logger:  logger,
		httpMux: http.NewServeMux(),
	}
}

// Serve starts the gRPC server on the given address.
func (s *PluginServer) Serve(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrapf(err, "failed to listen on %s", addr)
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
		return errors.Wrap(err, "gRPC server error")
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
// This method is idempotent - concurrent calls will block until the first completes.
func (s *PluginServer) Initialize(ctx context.Context, req *protocol.InitializeRequest) (*protocol.Empty, error) {
	// Use sync.Once to ensure initialization happens exactly once,
	// even under concurrent access
	s.initOnce.Do(func() {
		// Create a remote service registry with service endpoints
		s.services = NewRemoteServiceRegistry(
			req.AtsStoreEndpoint,
			req.QueueEndpoint,
			req.AuthToken,
			req.Config,
			s.logger,
		)

		// Initialize the plugin
		if err := s.plugin.Initialize(ctx, s.services); err != nil {
			s.initErr = errors.Wrapf(err, "plugin %s initialization failed", s.plugin.Metadata().Name)
			return
		}

		// Register HTTP handlers
		if err := s.plugin.RegisterHTTP(s.httpMux); err != nil {
			s.initErr = errors.Wrapf(err, "HTTP registration failed for plugin %s", s.plugin.Metadata().Name)
			return
		}
	})

	if s.initErr != nil {
		return nil, s.initErr
	}

	return &protocol.Empty{}, nil
}

// Shutdown shuts down the plugin.
func (s *PluginServer) Shutdown(ctx context.Context, _ *protocol.Empty) (*protocol.Empty, error) {
	if err := s.plugin.Shutdown(ctx); err != nil {
		return nil, errors.Wrapf(err, "plugin %s shutdown failed", s.plugin.Metadata().Name)
	}
	return &protocol.Empty{}, nil
}

// HandleHTTP handles an HTTP request via gRPC.
func (s *PluginServer) HandleHTTP(ctx context.Context, req *protocol.HTTPRequest) (*protocol.HTTPResponse, error) {
	// Create an HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create HTTP request")
	}

	// Set headers (support multi-value headers)
	for _, header := range req.Headers {
		for _, value := range header.Values {
			httpReq.Header.Add(header.Name, value)
		}
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
		return nil, errors.Wrap(err, "failed to read response body")
	}

	// Build response headers (preserve multi-value headers like Set-Cookie)
	headers := make([]*protocol.HTTPHeader, 0, len(result.Header))
	for name, values := range result.Header {
		headers = append(headers, &protocol.HTTPHeader{
			Name:   name,
			Values: values,
		})
	}

	return &protocol.HTTPResponse{
		StatusCode: int32(result.StatusCode),
		Headers:    headers,
		Body:       body,
	}, nil
}

// HandleWebSocket handles a WebSocket connection via gRPC streaming.
// This implementation provides a simple echo server that demonstrates
// bidirectional streaming between client and plugin.
func (s *PluginServer) HandleWebSocket(stream protocol.DomainPluginService_HandleWebSocketServer) error {
	s.logger.Debug("WebSocket stream established via gRPC")

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			s.logger.Debug("WebSocket stream EOF")
			return nil
		}
		if err != nil {
			s.logger.Errorw("WebSocket stream receive error", "error", err)
			return err
		}

		switch msg.Type {
		case protocol.WebSocketMessage_CONNECT:
			s.logger.Info("WebSocket CONNECT message received")
			// Connection established, ready to receive data

		case protocol.WebSocketMessage_DATA:
			s.logger.Debugw("WebSocket DATA received", "size", len(msg.Data))

			// Echo the message back
			// This demonstrates bidirectional streaming working correctly
			// Real plugins would process the data and respond appropriately
			echoMsg := &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_DATA,
				Data:      msg.Data,
				Timestamp: msg.Timestamp,
			}

			if err := stream.Send(echoMsg); err != nil {
				s.logger.Errorw("Failed to send WebSocket message", "error", err)
				return err
			}

		case protocol.WebSocketMessage_PING:
			s.logger.Debug("WebSocket PING received, sending PONG")
			// Respond with PONG, echoing back the timestamp for latency measurement
			pongMsg := &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_PONG,
				Timestamp: msg.Timestamp,
			}
			if err := stream.Send(pongMsg); err != nil {
				s.logger.Errorw("Failed to send PONG message", "error", err)
				return err
			}

		case protocol.WebSocketMessage_PONG:
			s.logger.Debug("WebSocket PONG received")
			// PONG received, connection is alive
			// Client-side keepalive handler processes latency

		case protocol.WebSocketMessage_ERROR:
			s.logger.Errorw("WebSocket ERROR received", "error", string(msg.Data))
			// Log the error and continue, let the connection decide if it should close

		case protocol.WebSocketMessage_CLOSE:
			s.logger.Info("WebSocket CLOSE message received")
			// Send CLOSE acknowledgment
			closeMsg := &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_CLOSE,
				Timestamp: msg.Timestamp,
			}
			if err := stream.Send(closeMsg); err != nil {
				s.logger.Errorw("Failed to send CLOSE message", "error", err)
			}
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

// ConfigSchema returns the plugin's configuration schema for UI-based configuration.
// If the plugin implements ConfigurablePlugin, returns its schema; otherwise empty.
func (s *PluginServer) ConfigSchema(ctx context.Context, _ *protocol.Empty) (*protocol.ConfigSchemaResponse, error) {
	// Check if plugin implements ConfigurablePlugin
	configurable, ok := s.plugin.(plugin.ConfigurablePlugin)
	if !ok {
		// Plugin doesn't support configuration schema - return empty
		return &protocol.ConfigSchemaResponse{
			Fields: make(map[string]*protocol.ConfigFieldSchema),
		}, nil
	}

	// Get schema from plugin and convert to protocol format
	schema := configurable.ConfigSchema()
	fields := make(map[string]*protocol.ConfigFieldSchema, len(schema))

	for name, field := range schema {
		fields[name] = &protocol.ConfigFieldSchema{
			Type:         field.Type,
			Description:  field.Description,
			DefaultValue: field.DefaultValue,
			Required:     field.Required,
			MinValue:     field.MinValue,
			MaxValue:     field.MaxValue,
			Pattern:      field.Pattern,
			ElementType:  field.ElementType,
		}
	}

	return &protocol.ConfigSchemaResponse{
		Fields: fields,
	}, nil
}
