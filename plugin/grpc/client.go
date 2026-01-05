package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ExternalDomainProxy implements DomainPlugin by proxying to a remote gRPC process.
// This is the adapter that allows external plugins to be used identically to built-in plugins.
// From the Registry's perspective, there is no difference between a built-in plugin
// and an ExternalDomainProxy - both implement DomainPlugin.
type ExternalDomainProxy struct {
	conn     *grpc.ClientConn
	client   protocol.DomainPluginServiceClient
	logger   *zap.SugaredLogger
	addr     string
	metadata plugin.Metadata
}

// NewExternalDomainProxy creates a new proxy to an external plugin running at the given address.
// The returned proxy implements DomainPlugin and can be registered with the Registry
// just like any built-in plugin.
func NewExternalDomainProxy(addr string, logger *zap.SugaredLogger) (*ExternalDomainProxy, error) {
	// Create gRPC connection with retry and timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin at %s: %w", addr, err)
	}

	client := protocol.NewDomainPluginServiceClient(conn)

	proxy := &ExternalDomainProxy{
		conn:   conn,
		client: client,
		logger: logger,
		addr:   addr,
	}

	// Fetch and cache metadata
	metaResp, err := client.Metadata(ctx, &protocol.Empty{})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to get plugin metadata from %s: %w", addr, err)
	}

	proxy.metadata = plugin.Metadata{
		Name:        metaResp.Name,
		Version:     metaResp.Version,
		QNTXVersion: metaResp.QntxVersion,
		Description: metaResp.Description,
		Author:      metaResp.Author,
		License:     metaResp.License,
	}

	logger.Infow("Connected to external plugin",
		"name", proxy.metadata.Name,
		"version", proxy.metadata.Version,
		"address", addr,
	)

	return proxy, nil
}

// Close closes the gRPC connection.
func (c *ExternalDomainProxy) Close() error {
	return c.conn.Close()
}

// Metadata returns the plugin's metadata (cached from connection).
func (c *ExternalDomainProxy) Metadata() plugin.Metadata {
	return c.metadata
}

// Initialize initializes the remote plugin.
func (c *ExternalDomainProxy) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	// Build config map from service registry
	config := make(map[string]string)
	pluginConfig := services.Config(c.metadata.Name)

	// Pass common configuration keys to plugin
	// Note: Config interface doesn't support enumerating all keys, so we manually
	// specify known configuration keys. Plugins that need custom config should
	// document their keys.
	//
	// TODO: Add AllSettings() method to Config interface for complete config passing

	// String configuration
	stringKeys := []string{
		"workspace_root", "api_token", "base_url", "model",
		"github.token", "gopls.workspace_root",
	}
	for _, key := range stringKeys {
		if val := pluginConfig.GetString(key); val != "" {
			config[key] = val
		}
	}

	// Boolean configuration
	boolKeys := []string{"enabled", "gopls.enabled"}
	for _, key := range boolKeys {
		// Always pass boolean values (even false) to avoid ambiguity
		config[key] = fmt.Sprintf("%v", pluginConfig.GetBool(key))
	}

	// Integer configuration
	intKeys := []string{"context_size", "timeout_seconds"}
	for _, key := range intKeys {
		if val := pluginConfig.GetInt(key); val != 0 {
			config[key] = fmt.Sprintf("%d", val)
		}
	}

	// String slice configuration (JSON-encoded)
	sliceKeys := []string{"allowed_domains", "blocked_domains"}
	for _, key := range sliceKeys {
		if slice := pluginConfig.GetStringSlice(key); len(slice) > 0 {
			if jsonBytes, err := json.Marshal(slice); err == nil {
				config[key] = string(jsonBytes)
			}
		}
	}

	// Get service endpoints from the service registry if available
	// These will be empty strings if services aren't running
	atsStoreEndpoint := ""
	queueEndpoint := ""
	authToken := ""

	// Try to extract endpoints from config (passed by PluginManager)
	if ep := pluginConfig.GetString("_ats_store_endpoint"); ep != "" {
		atsStoreEndpoint = ep
	}
	if ep := pluginConfig.GetString("_queue_endpoint"); ep != "" {
		queueEndpoint = ep
	}
	if token := pluginConfig.GetString("_auth_token"); token != "" {
		authToken = token
	}

	req := &protocol.InitializeRequest{
		AtsStoreEndpoint: atsStoreEndpoint,
		QueueEndpoint:    queueEndpoint,
		AuthToken:        authToken,
		Config:           config,
	}

	_, err := c.client.Initialize(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to initialize remote plugin %s at %s: %w", c.metadata.Name, c.addr, err)
	}

	c.logger.Infow("Remote plugin initialized",
		"name", c.metadata.Name,
		"ats_store", atsStoreEndpoint != "",
		"queue", queueEndpoint != "",
	)
	return nil
}

// Shutdown shuts down the remote plugin.
func (c *ExternalDomainProxy) Shutdown(ctx context.Context) error {
	_, err := c.client.Shutdown(ctx, &protocol.Empty{})
	if err != nil {
		return fmt.Errorf("failed to shutdown remote plugin %s at %s: %w", c.metadata.Name, c.addr, err)
	}
	return c.conn.Close()
}

// RegisterHTTP registers HTTP handlers that proxy to the remote plugin.
func (c *ExternalDomainProxy) RegisterHTTP(mux *http.ServeMux) error {
	// Register a catch-all handler for the plugin's namespace
	namespace := fmt.Sprintf("/api/%s/", c.metadata.Name)

	mux.HandleFunc(namespace, func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})

	c.logger.Infow("Registered HTTP proxy handler", "namespace", namespace)
	return nil
}

// proxyHTTPRequest forwards an HTTP request to the remote plugin.
func (c *ExternalDomainProxy) proxyHTTPRequest(w http.ResponseWriter, r *http.Request) {
	// Read request body
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}
	}

	// Convert HTTP headers to protocol format
	// HTTP headers can have multiple values (e.g., Set-Cookie, Accept)
	headers := make([]*protocol.HTTPHeader, 0, len(r.Header))
	for name, values := range r.Header {
		headers = append(headers, &protocol.HTTPHeader{
			Name:   name,
			Values: values,
		})
	}

	// Create gRPC request
	req := &protocol.HTTPRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
		Body:    body,
	}

	// Add query string to path
	if r.URL.RawQuery != "" {
		req.Path = r.URL.Path + "?" + r.URL.RawQuery
	}

	// Call remote plugin
	resp, err := c.client.HandleHTTP(r.Context(), req)
	if err != nil {
		c.logger.Errorw("Remote HTTP request failed", "error", err, "path", r.URL.Path)
		http.Error(w, "Plugin error", http.StatusBadGateway)
		return
	}

	// Write response headers
	// Support multi-value headers (e.g., Set-Cookie)
	for _, header := range resp.Headers {
		for _, value := range header.Values {
			w.Header().Add(header.Name, value)
		}
	}

	// Write status and body
	w.WriteHeader(int(resp.StatusCode))
	if len(resp.Body) > 0 {
		w.Write(resp.Body)
	}
}

// RegisterWebSocket returns WebSocket handlers that proxy to the remote plugin.
func (c *ExternalDomainProxy) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	// Return a proxy WebSocket handler
	handlers := make(map[string]plugin.WebSocketHandler)

	// Create a proxy handler for the plugin's WebSocket endpoints
	handlers[fmt.Sprintf("/%s-ws", c.metadata.Name)] = &wsProxyHandler{
		client: c,
		logger: c.logger,
	}

	return handlers, nil
}

// wsProxyHandler proxies WebSocket connections to the remote plugin.
type wsProxyHandler struct {
	client *ExternalDomainProxy
	logger *zap.SugaredLogger
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // TODO: Implement proper origin validation
	},
}

// ServeWS handles WebSocket upgrade and proxies to remote plugin.
func (h *wsProxyHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	wsConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Errorw("WebSocket upgrade failed", "error", err)
		return
	}
	defer wsConn.Close()

	// Establish bidirectional gRPC stream
	ctx := r.Context()
	stream, err := h.client.client.HandleWebSocket(ctx)
	if err != nil {
		h.logger.Errorw("Failed to establish gRPC stream", "error", err)
		wsConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Failed to connect to plugin"))
		return
	}

	h.logger.Info("WebSocket connection established, bridging to gRPC stream")

	// Send CONNECT message to plugin
	if err := stream.Send(&protocol.WebSocketMessage{
		Type: protocol.WebSocketMessage_CONNECT,
		Data: []byte{},
	}); err != nil {
		h.logger.Errorw("Failed to send CONNECT message", "error", err)
		return
	}

	// Bridge WebSocket and gRPC stream bidirectionally
	errChan := make(chan error, 2)

	// WebSocket -> gRPC stream
	go func() {
		defer func() {
			stream.Send(&protocol.WebSocketMessage{
				Type: protocol.WebSocketMessage_CLOSE,
				Data: []byte{},
			})
		}()

		for {
			messageType, data, err := wsConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
					h.logger.Errorw("WebSocket read error", "error", err)
				}
				errChan <- err
				return
			}

			h.logger.Debugw("WebSocket -> gRPC", "type", messageType, "size", len(data))

			// Convert WebSocket message to protocol message
			protoMsg := &protocol.WebSocketMessage{
				Type: protocol.WebSocketMessage_DATA,
				Data: data,
			}

			// Send to gRPC stream
			if err := stream.Send(protoMsg); err != nil {
				h.logger.Errorw("Failed to send to gRPC stream", "error", err)
				errChan <- err
				return
			}
		}
	}()

	// gRPC stream -> WebSocket
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					h.logger.Errorw("gRPC stream read error", "error", err)
				}
				errChan <- err
				return
			}

			h.logger.Debugw("gRPC -> WebSocket", "type", msg.Type, "size", len(msg.Data))

			// Handle CLOSE message from plugin
			if msg.Type == protocol.WebSocketMessage_CLOSE {
				wsConn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				errChan <- io.EOF
				return
			}

			// Send DATA message to WebSocket client
			if msg.Type == protocol.WebSocketMessage_DATA {
				if err := wsConn.WriteMessage(websocket.TextMessage, msg.Data); err != nil {
					h.logger.Errorw("WebSocket write error", "error", err)
					errChan <- err
					return
				}
			}
		}
	}()

	// Wait for error from either direction
	err = <-errChan
	if err != nil && err != io.EOF {
		h.logger.Errorw("WebSocket proxy error", "error", err)
	}

	h.logger.Info("WebSocket connection closed")
}

// Health returns the remote plugin's health status.
func (c *ExternalDomainProxy) Health(ctx context.Context) plugin.HealthStatus {
	// Add explicit timeout for health checks to prevent hanging
	// Use provided context's deadline if set, otherwise default to 5 seconds
	healthCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		healthCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	resp, err := c.client.Health(healthCtx, &protocol.Empty{})
	if err != nil {
		return plugin.HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("Failed to check plugin health: %v", err),
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	details := make(map[string]interface{})
	for key, value := range resp.Details {
		details[key] = value
	}

	return plugin.HealthStatus{
		Healthy: resp.Healthy,
		Message: resp.Message,
		Details: details,
	}
}

// Verify ExternalDomainProxy implements DomainPlugin
var _ plugin.DomainPlugin = (*ExternalDomainProxy)(nil)
