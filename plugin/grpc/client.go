package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/errors"
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

	// WebSocket configuration (set via SetWebSocketConfig)
	keepaliveConfig *KeepaliveConfig
	wsConfig        *WebSocketConfig
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
		return nil, errors.Wrapf(err, "failed to connect to plugin at %s", addr)
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
		return nil, errors.Wrapf(err, "failed to get plugin metadata from %s", addr)
	}

	proxy.metadata = plugin.Metadata{
		Name:        metaResp.Name,
		Version:     metaResp.Version,
		QNTXVersion: metaResp.QntxVersion,
		Description: metaResp.Description,
		Author:      metaResp.Author,
		License:     metaResp.License,
	}

	logger.Infof("Connected to '%s' plugin gRPC server v%s at %s (requires QNTX %s)",
		proxy.metadata.Name, proxy.metadata.Version, addr, proxy.metadata.QNTXVersion)

	return proxy, nil
}

// Close closes the gRPC connection.
func (c *ExternalDomainProxy) Close() error {
	return c.conn.Close()
}

// SetWebSocketConfig configures WebSocket settings for keepalive and origin validation.
// If not called, defaults will be used.
func (c *ExternalDomainProxy) SetWebSocketConfig(keepalive KeepaliveConfig, ws WebSocketConfig) {
	c.keepaliveConfig = &keepalive
	c.wsConfig = &ws
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
		return errors.Wrapf(err, "failed to initialize remote plugin %s at %s", c.metadata.Name, c.addr)
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
		return errors.Wrapf(err, "failed to shutdown remote plugin %s at %s", c.metadata.Name, c.addr)
	}
	return c.conn.Close()
}

// RegisterHTTP registers HTTP handlers that proxy to the remote plugin.
func (c *ExternalDomainProxy) RegisterHTTP(mux *http.ServeMux) error {
	// Register a catch-all handler for the plugin's namespace using Go 1.22+ wildcard pattern
	pattern := fmt.Sprintf("/api/%s/{path...}", c.metadata.Name)

	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})

	c.logger.Infow("Registered HTTP proxy handler", "pattern", pattern)
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

	// Strip plugin namespace prefix from path
	// e.g., /api/python/execute -> /execute
	namespace := "/api/" + c.metadata.Name + "/"
	path := strings.TrimPrefix(r.URL.Path, namespace)
	// Ensure path starts with / (TrimPrefix removes it)
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// Handle exact namespace match (e.g., /api/python -> /)
	if path == "" || path == r.URL.Path {
		path = "/"
	}

	// Create gRPC request
	req := &protocol.HTTPRequest{
		Method:  r.Method,
		Path:    path,
		Headers: headers,
		Body:    body,
	}

	// Add query string to path
	if r.URL.RawQuery != "" {
		req.Path = path + "?" + r.URL.RawQuery
	}

	// Call remote plugin
	resp, err := c.client.HandleHTTP(r.Context(), req)
	if err != nil {
		c.logger.Errorw("Remote HTTP request failed",
			"plugin", c.metadata.Name,
			"method", r.Method,
			"original_path", r.URL.Path,
			"stripped_path", path,
			"error", err)
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

	// Use configured keepalive or default
	keepaliveCfg := DefaultKeepaliveConfig()
	if c.keepaliveConfig != nil {
		keepaliveCfg = *c.keepaliveConfig
	}
	keepaliveHandler := NewKeepaliveHandler(keepaliveCfg, c.logger)

	// Use configured WebSocket security or default
	wsCfg := DefaultWebSocketConfig()
	if c.wsConfig != nil {
		wsCfg = *c.wsConfig
	}

	// Create a proxy handler for the plugin's WebSocket endpoints
	handlers[fmt.Sprintf("/%s-ws", c.metadata.Name)] = &wsProxyHandler{
		client:    c,
		logger:    c.logger,
		keepalive: keepaliveHandler,
		wsConfig:  wsCfg,
	}

	return handlers, nil
}

// RegisterWebSocketWithConfig returns WebSocket handlers with custom keepalive config.
func (c *ExternalDomainProxy) RegisterWebSocketWithConfig(config KeepaliveConfig) (map[string]plugin.WebSocketHandler, error) {
	handlers := make(map[string]plugin.WebSocketHandler)

	keepaliveHandler := NewKeepaliveHandler(config, c.logger)

	// TODO: Load WebSocket config from plugin manifest
	handlers[fmt.Sprintf("/%s-ws", c.metadata.Name)] = &wsProxyHandler{
		client:    c,
		logger:    c.logger,
		keepalive: keepaliveHandler,
		wsConfig:  DefaultWebSocketConfig(),
	}

	return handlers, nil
}

// wsProxyHandler proxies WebSocket connections to the remote plugin.
type wsProxyHandler struct {
	client    *ExternalDomainProxy
	logger    *zap.SugaredLogger
	keepalive *KeepaliveHandler
	wsConfig  WebSocketConfig
}

// ServeWS handles WebSocket upgrade and proxies to remote plugin.
func (h *wsProxyHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Add security headers before upgrade
	AddSecurityHeaders(w)

	// Create upgrader with origin validation
	upgrader := websocket.Upgrader{
		CheckOrigin:  CreateOriginChecker(h.wsConfig, h.logger),
		Subprotocols: []string{"qntx-plugin-v1"},
	}

	// Upgrade HTTP connection to WebSocket
	wsConn, err := upgrader.Upgrade(w, r, nil)
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
		Type:      protocol.WebSocketMessage_CONNECT,
		Timestamp: time.Now().UnixNano(),
	}); err != nil {
		h.logger.Errorw("Failed to send CONNECT message", "error", err)
		return
	}

	// Start keepalive handler
	sendPing := func(timestamp int64) error {
		return stream.Send(&protocol.WebSocketMessage{
			Type:      protocol.WebSocketMessage_PING,
			Timestamp: timestamp,
		})
	}
	h.keepalive.Start(ctx, sendPing)
	defer h.keepalive.Stop()

	// Bridge WebSocket and gRPC stream bidirectionally
	errChan := make(chan error, 2)

	// WebSocket -> gRPC stream
	go func() {
		defer func() {
			stream.Send(&protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_CLOSE,
				Timestamp: time.Now().UnixNano(),
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
				Type:      protocol.WebSocketMessage_DATA,
				Data:      data,
				Timestamp: time.Now().UnixNano(),
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

			// Handle keepalive messages
			if msg.Type == protocol.WebSocketMessage_PING ||
				msg.Type == protocol.WebSocketMessage_PONG ||
				msg.Type == protocol.WebSocketMessage_ERROR {
				response, err := h.keepalive.HandleMessage(msg)
				if err != nil {
					h.logger.Errorw("Keepalive error", "error", err)
					errChan <- err
					return
				}
				// Send PONG response if needed
				if response != nil {
					if err := stream.Send(response); err != nil {
						h.logger.Errorw("Failed to send PONG", "error", err)
					}
				}
				continue
			}

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

	// Log connection metrics
	metrics := h.keepalive.Metrics()
	h.logger.Infow("WebSocket connection closed",
		"uptime", metrics.GetConnectionUptime(),
		"pings_sent", metrics.GetTotalPings(),
		"pongs_received", metrics.GetTotalPongs(),
		"avg_latency", metrics.GetAverageLatency(),
	)
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
