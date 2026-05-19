package grpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ExternalDomainProxy implements DomainPlugin by proxying to a gRPC plugin process.
// All QNTX plugins run via gRPC - this is the client-side proxy that connects to them.
// From the Registry's perspective, all plugins implement the same DomainPlugin interface.
type ExternalDomainProxy struct {
	conn     *grpc.ClientConn
	client   protocol.DomainPluginServiceClient
	logger   *zap.SugaredLogger
	addr     string
	metadata plugin.Metadata

	// Handler names this plugin can execute (populated during Initialize)
	handlerNames []string

	// Schedules this plugin wants QNTX to create (populated during Initialize)
	schedules []*protocol.ScheduleInfo

	// llmProvider indicates this plugin implements LLMProvider (populated during Initialize)
	llmProvider bool

	// vectorSearchProvider indicates this plugin implements VectorSearchService (populated during Initialize)
	vectorSearchProvider bool

	// searchProvider indicates this plugin implements SearchProvider (populated during Initialize)
	searchProvider bool

	// embeddingProvider indicates this plugin implements EmbeddingService (populated during Initialize)
	embeddingProvider bool

	// pythonProvider indicates this plugin can execute Python code (populated during Initialize)
	pythonProvider bool

	// Watchers this plugin wants registered (populated during Initialize)
	watchers []*protocol.WatcherRegistration

	// httpRoutes lists HTTP endpoints this plugin handles (populated during Initialize, optional)
	httpRoutes []*protocol.RouteInfo

	// WebSocket configuration (set via SetWebSocketConfig)
	keepaliveConfig *KeepaliveConfig
	wsConfig        *WebSocketConfig

	// Callback invoked after plugin watchers are written to DB.
	// Allows the server to reload the watcher engine's in-memory map.
	OnWatchersSetup func()

	// Initialize idempotency — multiple code paths may call Initialize
	// (server/init.go eager init + async goroutine in main.go)
	initOnce sync.Once
	initErr  error
}

// NewExternalDomainProxy creates a new client proxy to a gRPC plugin at the given address.
// The returned proxy implements DomainPlugin and can be registered with the Registry.
func NewExternalDomainProxy(addr string, logger *zap.SugaredLogger) (*ExternalDomainProxy, error) {
	// Create gRPC connection with retry and timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const maxMsgSize = 100 * 1024 * 1024 // 100MB — binary ingestion payloads exceed the 4MB default
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second, // ping every 30s if idle
			Timeout:             10 * time.Second, // wait 10s for pong before closing
			PermitWithoutStream: true,             // ping even with no active RPCs
		}),
	)
	if err != nil {
		wrappedErr := errors.Wrapf(err, "failed to connect to plugin at %s", addr)
		return nil, errors.WithHint(wrappedErr, "verify the plugin is running and the address/port is correct")
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
		wrappedErr := errors.Wrapf(err, "failed to get plugin metadata from %s", addr)
		return nil, errors.WithHint(wrappedErr, "plugin may not implement the required gRPC interface or is still starting up")
	}

	proxy.metadata = plugin.Metadata{
		Name:        metaResp.Name,
		Version:     metaResp.Version,
		QNTXVersion: metaResp.QntxVersion,
		Description: metaResp.Description,
		Author:      metaResp.Author,
		License:     metaResp.License,
	}

	logger.Debugf("Connected to '%s' plugin gRPC server v%s at %s (requires QNTX %s)",
		proxy.metadata.Name, proxy.metadata.Version, addr, proxy.metadata.QNTXVersion)

	return proxy, nil
}

// Addr returns the gRPC address this proxy is connected to.
func (c *ExternalDomainProxy) Addr() string {
	return c.addr
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

// GetHandlerNames returns the async handler names this plugin announced during Initialize.
// Returns empty slice if plugin provides no async handlers (Phase 1: all plugins return empty).
func (c *ExternalDomainProxy) GetHandlerNames() []string {
	return c.handlerNames
}

// GetSchedules returns the schedules this plugin announced during Initialize.
// Returns empty slice if plugin provides no schedules.
func (c *ExternalDomainProxy) GetSchedules() []*protocol.ScheduleInfo {
	return c.schedules
}

// Client returns the underlying gRPC client for making RPC calls to the plugin.
// Used by PluginProxyHandler to forward ExecuteJob calls.
func (c *ExternalDomainProxy) Client() protocol.DomainPluginServiceClient {
	return c.client
}

// IsLLMProvider returns true if this plugin declared LLM provider capability during Initialize.
func (c *ExternalDomainProxy) IsLLMProvider() bool {
	return c.llmProvider
}

// GetWatchers returns the watcher registrations this plugin announced during Initialize.
func (c *ExternalDomainProxy) GetWatchers() []*protocol.WatcherRegistration {
	return c.watchers
}

// LLMServiceClient returns an LLMServiceClient using this plugin's existing gRPC connection.
// Only meaningful when IsLLMProvider() is true.
func (c *ExternalDomainProxy) LLMServiceClient() protocol.LLMServiceClient {
	return protocol.NewLLMServiceClient(c.conn)
}

// IsVectorSearchProvider returns true if this plugin declared VectorSearch provider capability during Initialize.
func (c *ExternalDomainProxy) IsVectorSearchProvider() bool {
	return c.vectorSearchProvider
}

// VectorSearchServiceClient returns a VectorSearchServiceClient using this plugin's existing gRPC connection.
// Only meaningful when IsVectorSearchProvider() is true.
func (c *ExternalDomainProxy) VectorSearchServiceClient() protocol.VectorSearchServiceClient {
	return protocol.NewVectorSearchServiceClient(c.conn)
}

// IsSearchProvider returns true if this plugin declared search provider capability during Initialize.
func (c *ExternalDomainProxy) IsSearchProvider() bool {
	return c.searchProvider
}

// SearchServiceClient returns a SearchServiceClient using this plugin's existing gRPC connection.
// Only meaningful when IsSearchProvider() is true.
func (c *ExternalDomainProxy) SearchServiceClient() protocol.SearchServiceClient {
	return protocol.NewSearchServiceClient(c.conn)
}

// IsEmbeddingProvider returns true if this plugin declared embedding provider capability during Initialize.
func (c *ExternalDomainProxy) IsEmbeddingProvider() bool {
	return c.embeddingProvider
}

// EmbeddingServiceClient returns an EmbeddingServiceClient using this plugin's existing gRPC connection.
// Only meaningful when IsEmbeddingProvider() is true.
func (c *ExternalDomainProxy) EmbeddingServiceClient() protocol.EmbeddingServiceClient {
	return protocol.NewEmbeddingServiceClient(c.conn)
}

// IsPythonProvider returns true if this plugin declared Python execution capability during Initialize.
func (c *ExternalDomainProxy) IsPythonProvider() bool {
	return c.pythonProvider
}

// PythonServiceClient returns a PythonServiceClient using this plugin's existing gRPC connection.
// Only meaningful when IsPythonProvider() is true.
func (c *ExternalDomainProxy) PythonServiceClient() protocol.PythonServiceClient {
	return protocol.NewPythonServiceClient(c.conn)
}

// GetHTTPRoutes returns the HTTP routes this plugin advertised during Initialize.
func (c *ExternalDomainProxy) GetHTTPRoutes() []*protocol.RouteInfo {
	return c.httpRoutes
}

// Initialize initializes the remote plugin. Idempotent — safe to call from multiple code paths.
func (c *ExternalDomainProxy) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	c.initOnce.Do(func() {
		c.initErr = c.doInitialize(ctx, services)
	})
	return c.initErr
}

// ForceInitialize re-initializes the plugin (e.g. after config update).
// Bypasses the once-guard so the gRPC call is actually sent again.
func (c *ExternalDomainProxy) ForceInitialize(ctx context.Context, services plugin.ServiceRegistry) error {
	return c.doInitialize(ctx, services)
}

// doInitialize performs the actual gRPC Initialize RPC.
// Called once per proxy via initOnce (boot/restart), or directly via ForceInitialize (config update).
// Plugins must handle being initialized again: stop previous state before starting new.
// See ADR-018 for the full Initialize contract.
func (c *ExternalDomainProxy) doInitialize(ctx context.Context, services plugin.ServiceRegistry) error {
	// Build config map from service registry
	config := make(map[string]string)
	pluginConfig := services.Config(c.metadata.Name)

	// Pass all configuration keys from the plugin's namespace
	// This includes both built-in keys and custom keys from ~/.qntx/plugins/{name}.toml [config] sections
	for _, key := range pluginConfig.GetKeys() {
		// Skip internal keys (prefixed with _)
		if len(key) > 0 && key[0] == '_' {
			continue
		}

		// Get raw value and convert to string for protobuf
		val := pluginConfig.Get(key)
		if val == nil {
			continue
		}

		// Type-based conversion to string
		switch v := val.(type) {
		case string:
			if v != "" {
				config[key] = v
			}
		case int, int8, int16, int32, int64:
			config[key] = fmt.Sprintf("%d", v)
		case float32, float64:
			config[key] = fmt.Sprintf("%f", v)
		case bool:
			config[key] = fmt.Sprintf("%v", v)
		case []interface{}:
			// Array types - serialize as JSON
			if jsonBytes, err := json.Marshal(v); err == nil {
				config[key] = string(jsonBytes)
			}
		default:
			// Try JSON marshaling for complex types
			if jsonBytes, err := json.Marshal(v); err == nil {
				config[key] = string(jsonBytes)
			}
		}
	}

	// Get service endpoints from the service registry if available
	// These will be empty strings if services aren't running
	atsStoreEndpoint := ""
	queueEndpoint := ""
	scheduleEndpoint := ""
	fileServiceEndpoint := ""
	authToken := ""

	// Try to extract endpoints from config (passed by PluginManager)
	if ep := pluginConfig.GetString("_ats_store_endpoint"); ep != "" {
		atsStoreEndpoint = ep
		c.logger.Debugw("Extracted ATSStore endpoint from config", "endpoint", ep)
	}
	if ep := pluginConfig.GetString("_queue_endpoint"); ep != "" {
		queueEndpoint = ep
		c.logger.Debugw("Extracted Queue endpoint from config", "endpoint", ep)
	}
	if ep := pluginConfig.GetString("_schedule_endpoint"); ep != "" {
		scheduleEndpoint = ep
		c.logger.Debugw("Extracted Schedule endpoint from config", "endpoint", ep)
	}
	if ep := pluginConfig.GetString("_file_service_endpoint"); ep != "" {
		fileServiceEndpoint = ep
		c.logger.Debugw("Extracted FileService endpoint from config", "endpoint", ep)
	}
	llmEndpoint := ""
	if ep := pluginConfig.GetString("_llm_endpoint"); ep != "" {
		llmEndpoint = ep
		c.logger.Debugw("Extracted LLM endpoint from config", "endpoint", ep)
	}
	embeddingEndpoint := ""
	if ep := pluginConfig.GetString("_embedding_endpoint"); ep != "" {
		embeddingEndpoint = ep
		c.logger.Debugw("Extracted Embedding endpoint from config", "endpoint", ep)
	}
	vectorSearchEndpoint := ""
	if ep := pluginConfig.GetString("_vector_search_endpoint"); ep != "" {
		vectorSearchEndpoint = ep
		c.logger.Debugw("Extracted VectorSearch endpoint from config", "endpoint", ep)
	}
	groundEndpoint := ""
	if ep := pluginConfig.GetString("_ground_endpoint"); ep != "" {
		groundEndpoint = ep
		c.logger.Debugw("Extracted Ground endpoint from config", "endpoint", ep)
	}
	searchEndpoint := ""
	if ep := pluginConfig.GetString("_search_endpoint"); ep != "" {
		searchEndpoint = ep
		c.logger.Debugw("Extracted Search endpoint from config", "endpoint", ep)
	}
	fetchEndpoint := ""
	if ep := pluginConfig.GetString("_fetch_endpoint"); ep != "" {
		fetchEndpoint = ep
		c.logger.Debugw("Extracted Fetch endpoint from config", "endpoint", ep)
	}
	if token := pluginConfig.GetString("_auth_token"); token != "" {
		authToken = token
	}

	req := &protocol.InitializeRequest{
		AtsStoreEndpoint:     atsStoreEndpoint,
		QueueEndpoint:        queueEndpoint,
		ScheduleEndpoint:     scheduleEndpoint,
		FileServiceEndpoint:  fileServiceEndpoint,
		LlmEndpoint:          llmEndpoint,
		EmbeddingEndpoint:    embeddingEndpoint,
		VectorSearchEndpoint: vectorSearchEndpoint,
		GroundEndpoint:       groundEndpoint,
		SearchEndpoint:       searchEndpoint,
		FetchEndpoint:        fetchEndpoint,
		AuthToken:            authToken,
		Config:               config,
	}

	c.logger.Debugw("Sending Initialize RPC to plugin",
		"name", c.metadata.Name,
		"ats_store_endpoint", atsStoreEndpoint,
		"queue_endpoint", queueEndpoint,
		"schedule_endpoint", scheduleEndpoint,
		"file_service_endpoint", fileServiceEndpoint,
		"llm_endpoint", llmEndpoint,
		"embedding_endpoint", embeddingEndpoint,
		"vector_search_endpoint", vectorSearchEndpoint,
		"ground_endpoint", groundEndpoint,
		"search_endpoint", searchEndpoint,
		"fetch_endpoint", fetchEndpoint,
	)

	resp, err := c.client.Initialize(ctx, req)
	if err != nil {
		wrappedErr := errors.Wrapf(err, "failed to initialize remote plugin %s at %s", c.metadata.Name, c.addr)
		return errors.WithHint(wrappedErr, "check plugin logs for initialization errors or verify required configuration is set")
	}

	// Store handler names announced by plugin
	c.handlerNames = resp.GetHandlerNames()

	// Store schedules announced by plugin
	c.schedules = resp.GetSchedules()

	// Store LLM provider capability
	c.llmProvider = resp.GetLlmProvider()

	// Store VectorSearch provider capability
	c.vectorSearchProvider = resp.GetVectorSearchProvider()

	// Store search provider capability
	c.searchProvider = resp.GetSearchProvider()

	// Store embedding provider capability
	c.embeddingProvider = resp.GetEmbeddingProvider()

	// Store HTTP routes (optional, for discovery)
	c.httpRoutes = resp.GetHttpRoutes()

	// Store Python provider capability
	c.pythonProvider = resp.GetPythonProvider()

	// Store and create watcher registrations
	c.watchers = resp.GetWatchers()
	if len(c.watchers) > 0 {
		if err := SetupPluginWatchers(services.Database(), c.metadata.Name, c.watchers, c.handlerNames, c.logger); err != nil {
			c.logger.Errorw("Failed to setup plugin watchers",
				"plugin", c.metadata.Name,
				"error", err,
			)
		} else if c.OnWatchersSetup != nil {
			c.OnWatchersSetup()
		}
	}

	c.logger.Debugw("Plugin initialized",
		"name", c.metadata.Name,
		"handlers", len(c.handlerNames),
		"schedules", len(c.schedules),
		"watchers", len(c.watchers),
		"llm_provider", c.llmProvider,
		"vector_search_provider", c.vectorSearchProvider,
		"search_provider", c.searchProvider,
		"embedding_provider", c.embeddingProvider,
	)
	return nil
}

// ConfigSchema returns the configuration schema from the remote plugin.
func (c *ExternalDomainProxy) ConfigSchema(ctx context.Context) (*protocol.ConfigSchemaResponse, error) {
	resp, err := c.client.ConfigSchema(ctx, &protocol.Empty{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get config schema from plugin %s at %s", c.metadata.Name, c.addr)
	}
	return resp, nil
}

// RegisterGlyphs returns custom glyph type definitions from the remote plugin.
func (c *ExternalDomainProxy) RegisterGlyphs(ctx context.Context) (*protocol.GlyphDefResponse, error) {
	resp, err := c.client.RegisterGlyphs(ctx, &protocol.Empty{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get glyph definitions from plugin %s at %s", c.metadata.Name, c.addr)
	}
	return resp, nil
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
// Uses method-specific wildcards to catch all paths including / (Issue #277).
func (c *ExternalDomainProxy) RegisterHTTP(mux *http.ServeMux) error {
	// Register method-specific wildcards that match all paths (including /)
	// The {path...} wildcard matches zero or more segments, so it matches / too
	mux.HandleFunc("GET /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})
	mux.HandleFunc("POST /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})
	mux.HandleFunc("PUT /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})
	mux.HandleFunc("PATCH /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})
	mux.HandleFunc("DELETE /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})
	mux.HandleFunc("HEAD /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})
	mux.HandleFunc("OPTIONS /{path...}", func(w http.ResponseWriter, r *http.Request) {
		c.proxyHTTPRequest(w, r)
	})

	c.logger.Infow("Registered HTTP proxy handlers", "plugin", c.metadata.Name)
	return nil
}

// proxyHTTPRequest forwards an HTTP request to the remote plugin.
// Tries stripped path first (without /api/{plugin}), then full path if 404 (Issue #277).
// This allows plugins to register routes either way without friction.
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
	headers := make([]*protocol.HTTPHeader, 0, len(r.Header))
	for name, values := range r.Header {
		headers = append(headers, &protocol.HTTPHeader{
			Name:   name,
			Values: values,
		})
	}

	// Calculate both stripped and full paths
	originalPath := r.URL.Path
	prefix := "/api/" + c.metadata.Name
	strippedPath := originalPath

	if originalPath == prefix {
		// Exact match: /api/code -> /
		strippedPath = "/"
	} else if len(originalPath) > len(prefix) && originalPath[:len(prefix)] == prefix {
		// Has prefix: /api/code/... -> /...
		strippedPath = originalPath[len(prefix):]
		// Ensure stripped path starts with /
		if strippedPath == "" || strippedPath[0] != '/' {
			strippedPath = "/" + strippedPath
		}
	}

	// Add query string
	queryString := ""
	if r.URL.RawQuery != "" {
		queryString = "?" + r.URL.RawQuery
	}

	// Try stripped path first (modern approach: plugins don't need to know mount point)
	req := &protocol.HTTPRequest{
		Method:  r.Method,
		Path:    strippedPath + queryString,
		Headers: headers,
		Body:    body,
	}

	c.logger.Debugw("Proxying to plugin (stripped)", "plugin", c.metadata.Name, "original", originalPath, "trying", strippedPath)
	resp, err := c.client.HandleHTTP(r.Context(), req)

	// If stripped path returns 404, try full path (for LLMs that naturally include prefix)
	if err == nil && resp.StatusCode == 404 && strippedPath != originalPath {
		c.logger.Debugw("Stripped path 404, retrying with full path", "plugin", c.metadata.Name, "full_path", originalPath)
		req.Path = originalPath + queryString
		resp, err = c.client.HandleHTTP(r.Context(), req)
	}

	// Handle errors — context canceled means the browser navigated away or refreshed.
	// The request is abandoned; no response needed, no error to log.
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		// Unimplemented/NotFound = plugin doesn't serve this path; debug, not error
		if s, ok := status.FromError(err); ok && (s.Code() == codes.Unimplemented || s.Code() == codes.NotFound) {
			c.logger.Debugw("Plugin does not serve path",
				"plugin", c.metadata.Name,
				"method", r.Method,
				"path", req.Path)
			http.Error(w, fmt.Sprintf("Plugin '%s': %s not found", c.metadata.Name, req.Path), http.StatusNotFound)
			return
		}
		c.logger.Errorw("Remote HTTP request failed",
			"plugin", c.metadata.Name,
			"method", r.Method,
			"path", req.Path,
			"addr", c.addr,
			"error", err)
		http.Error(w, fmt.Sprintf("Plugin '%s' error: %v (%s %s)", c.metadata.Name, err, r.Method, req.Path), http.StatusBadGateway)
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
	pluginLogger := c.logger.With("plugin", c.metadata.Name)
	pluginLabel := fmt.Sprintf("%s v%s", c.metadata.Name, c.metadata.Version)
	keepaliveHandler := NewKeepaliveHandler(keepaliveCfg, pluginLogger, pluginLabel)

	// Use configured WebSocket security or default
	wsCfg := DefaultWebSocketConfig()
	if c.wsConfig != nil {
		wsCfg = *c.wsConfig
	}

	// Create a proxy handler for the plugin's WebSocket endpoints
	handlers[fmt.Sprintf("/ws/%s", c.metadata.Name)] = &wsProxyHandler{
		client:    c,
		logger:    pluginLogger,
		keepalive: keepaliveHandler,
		wsConfig:  wsCfg,
	}

	return handlers, nil
}

// RegisterWebSocketWithConfig returns WebSocket handlers with custom keepalive config.
func (c *ExternalDomainProxy) RegisterWebSocketWithConfig(config KeepaliveConfig) (map[string]plugin.WebSocketHandler, error) {
	handlers := make(map[string]plugin.WebSocketHandler)

	pluginLogger := c.logger.With("plugin", c.metadata.Name)
	pluginLabel := fmt.Sprintf("%s v%s", c.metadata.Name, c.metadata.Version)
	keepaliveHandler := NewKeepaliveHandler(config, pluginLogger, pluginLabel)

	handlers[fmt.Sprintf("/ws/%s", c.metadata.Name)] = &wsProxyHandler{
		client:    c,
		logger:    pluginLogger,
		keepalive: keepaliveHandler,
		wsConfig:  DefaultWebSocketConfig(),
	}

	return handlers, nil
}

// browserWSMessage mirrors the JSON protocol used by the browser.
// The browser sends/receives JSON with type (enum int), base64-encoded data,
// headers map, and a timestamp. The Go proxy translates this to/from gRPC protobuf.
type browserWSMessage struct {
	Type      int32             `json:"type"`
	Data      string            `json:"data"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timestamp int64             `json:"timestamp"`
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
	// Use a standalone context — NOT r.Context(). The HTTP request context
	// cancels when the browser disconnects, which kills the gRPC stream and
	// produces "context canceled" errors. The WebSocket read loop already
	// detects disconnection and sends CLOSE, so we don't need r.Context().
	ctx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	md := metadata.New(nil)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			md.Set(key, values[0])
		}
	}
	ctx = metadata.NewOutgoingContext(ctx, md)
	stream, err := h.client.client.HandleWebSocket(ctx)
	if err != nil {
		h.logger.Errorw("Failed to establish gRPC stream", "error", err)
		wsConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Failed to connect to plugin"))
		return
	}

	h.logger.Debug("WebSocket connection established, bridging to gRPC stream")

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

			// Parse browser JSON message into protobuf
			var browserMsg browserWSMessage
			if err := json.Unmarshal(data, &browserMsg); err != nil {
				h.logger.Errorw("Failed to parse browser WebSocket message", "error", err, "raw", string(data))
				continue
			}

			// Decode base64 data field
			var rawData []byte
			if browserMsg.Data != "" {
				var decErr error
				rawData, decErr = base64.StdEncoding.DecodeString(browserMsg.Data)
				if decErr != nil {
					h.logger.Errorw("Failed to decode base64 data", "error", decErr)
					continue
				}
			}

			protoMsg := &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_Type(browserMsg.Type),
				Data:      rawData,
				Headers:   browserMsg.Headers,
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
				// Unavailable = plugin process was killed (restart/shutdown) — expected.
				// EOF / Canceled / ctx done = normal teardown.
				if err != io.EOF && err != context.Canceled && ctx.Err() == nil &&
					status.Code(err) != codes.Unavailable {
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

			// Send DATA message to WebSocket client as JSON with base64-encoded data
			if msg.Type == protocol.WebSocketMessage_DATA {
				outMsg := browserWSMessage{
					Type:      int32(msg.Type),
					Data:      base64.StdEncoding.EncodeToString(msg.Data),
					Headers:   msg.Headers,
					Timestamp: msg.Timestamp,
				}
				jsonBytes, err := json.Marshal(outMsg)
				if err != nil {
					h.logger.Errorw("Failed to marshal outbound message", "error", err)
					errChan <- err
					return
				}
				if err := wsConn.WriteMessage(websocket.TextMessage, jsonBytes); err != nil {
					h.logger.Errorw("WebSocket write error", "error", err)
					errChan <- err
					return
				}
			}
		}
	}()

	// Wait for error from either direction
	err = <-errChan
	if err != nil && err != io.EOF &&
		!websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) &&
		status.Code(err) != codes.Unavailable {
		h.logger.Errorw("WebSocket proxy error", "error", err)
	}

	// Log connection metrics
	metrics := h.keepalive.Metrics()
	h.logger.Debugw("WebSocket connection closed",
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

// ParseAxQuery calls the ParseAxQuery RPC on the remote plugin.
// Returns the raw JSON AST bytes on success.
func (c *ExternalDomainProxy) ParseAxQuery(ctx context.Context, query string) ([]byte, error) {
	resp, err := c.client.ParseAxQuery(ctx, &protocol.ParseAxQueryRequest{
		Query: query,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "ParseAxQuery RPC failed for plugin %s at %s", c.metadata.Name, c.addr)
	}
	if resp.Error != "" {
		return nil, errors.Newf("ax parse: %s", resp.Error)
	}
	return resp.Result, nil
}

// Verify ExternalDomainProxy implements DomainPlugin
var _ plugin.DomainPlugin = (*ExternalDomainProxy)(nil)
