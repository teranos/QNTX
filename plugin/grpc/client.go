package grpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/internal/admission"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
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

	// signa is what this plugin does, as signa (ADR-039), populated during
	// Initialize. The node serves each sigil and hands it to HandleHTTP.
	signa []*protocol.Signum

	// httpRoutes are the routes this plugin declared at Initialize (ADR-001).
	httpRoutes []*protocol.RouteInfo

	// traffic is what the proxy carried to this plugin, per declared route.
	traffic *Traffic

	// WebSocket configuration: the defaults a proxy is made with, until
	// SetWebSocketConfig hands it the node's.
	keepaliveConfig KeepaliveConfig
	wsConfig        WebSocketConfig

	// Callback invoked after plugin watchers are written to DB.
	// Allows the server to reload the watcher engine's in-memory map.
	// A proxy is made with one that has nothing to reload.
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
		conn:            conn,
		client:          client,
		logger:          logger,
		addr:            addr,
		traffic:         newTraffic(),
		keepaliveConfig: DefaultKeepaliveConfig(),
		wsConfig:        DefaultWebSocketConfig(),
		OnWatchersSetup: func() {},
	}

	// Fetch and cache metadata
	metaResp, err := client.Metadata(ctx, &protocol.Empty{})
	if err != nil {
		sqlclose.Log(conn.Close(), logger, "the plugin connection that failed its metadata call")
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
// If not called, the defaults the proxy was made with are used.
func (c *ExternalDomainProxy) SetWebSocketConfig(keepalive KeepaliveConfig, ws WebSocketConfig) {
	c.keepaliveConfig = keepalive
	c.wsConfig = ws
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

// GetSigna returns the signa this plugin handed the node during Initialize.
func (c *ExternalDomainProxy) GetSigna() []*protocol.Signum {
	return c.signa
}

// GetHTTPRoutes returns the routes this plugin declared during Initialize.
func (c *ExternalDomainProxy) GetHTTPRoutes() []*protocol.RouteInfo {
	return c.httpRoutes
}

// SigilRoutes is each endpoint the plugin's signa bind, as "METHOD /path", for
// the banner and the lifecycle event. Whether the node serves them is the
// node's to say (server.pluginSignaOf).
func (c *ExternalDomainProxy) SigilRoutes() []string {
	var routes []string
	for _, signum := range c.signa {
		for _, held := range signum.GetSigils() {
			routes = append(routes, held.GetHttp().GetMethod()+" "+held.GetHttp().GetPath())
		}
	}
	return routes
}

// AnswerHTTP hands the plugin one request the node built, as a sigil asked of
// it: the path below /api/{plugin}, and only the headers the node set.
func (c *ExternalDomainProxy) AnswerHTTP(ctx context.Context, req *protocol.HTTPRequest) (*protocol.HTTPResponse, error) {
	resp, err := c.client.HandleHTTP(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "plugin %s at %s did not answer %s %s", c.metadata.Name, c.addr, req.GetMethod(), req.GetPath())
	}
	return resp, nil
}

// askerHeaders are who is asking, set by the node when a sigil hands a plugin
// a request (server.HeaderAsker). A caller sending them is not believed.
var askerHeaders = map[string]bool{
	"X-Qntx-Asker":        true,
	"X-Qntx-Asker-User":   true,
	"X-Qntx-Asker-Did":    true,
	"X-Qntx-Asker-Label":  true,
	"X-Qntx-Asker-Client": true,
	"X-Qntx-Namespace":    true,
	"X-Qntx-Store-Token":  true,
	"X-Qntx-Asker-Level":  true,
}

// openedCall is the call the node opened for one request: the token a plugin
// presents to the store, and the namespace the caller acts in.
type openedCall struct {
	token     string
	namespace string
}

type openedCallKey struct{}

// WithCall hands the proxy the call the node opened for this request, which it
// sends as X-Qntx-Store-Token and X-Qntx-Namespace, as a sigil does.
func WithCall(ctx context.Context, token, namespace string) context.Context {
	return context.WithValue(ctx, openedCallKey{}, openedCall{token: token, namespace: namespace})
}

// HeadersFor is what a plugin is handed about who asked, for the node to check.
func HeadersFor(ctx context.Context) []*protocol.HTTPHeader { return askerFrom(ctx) }

// askerFrom is who the node admitted, as the headers a plugin reads it by.
func askerFrom(ctx context.Context) []*protocol.HTTPHeader {
	admitted, gated := admission.AdmissionFrom(ctx)
	if !gated {
		return nil
	}
	// Each header says what the admission holds, as it holds it.
	var headers []*protocol.HTTPHeader
	add := func(name, value string) {
		headers = append(headers, &protocol.HTTPHeader{Name: name, Values: []string{value}})
	}
	if call, opened := ctx.Value(openedCallKey{}).(openedCall); opened {
		add("X-Qntx-Store-Token", call.token)
		add("X-Qntx-Namespace", call.namespace)
	}
	add("X-Qntx-Asker", admitted.Identity)
	add("X-Qntx-Asker-User", admitted.UserID)
	add("X-Qntx-Asker-Level", admitted.LevelName())
	did, label := admitted.TokenDID, admitted.TokenLabel
	if admitted.Grant != nil {
		did, label = admitted.Grant.DID, admitted.Grant.Label
	}
	add("X-Qntx-Asker-Did", did)
	add("X-Qntx-Asker-Label", label)
	add("X-Qntx-Asker-Client", admitted.ClientDID)
	return headers
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
	// This includes both built-in keys and the keys the plugin's record holds,
	// each as the string it reads as.
	keys := pluginConfig.GetKeys()
	if err := configErr(pluginConfig); err != nil {
		return errors.Wrapf(err, "plugin %s was not handed its config", c.metadata.Name)
	}
	for _, key := range keys {
		// Skip internal keys (prefixed with _)
		if strings.HasPrefix(key, "_") {
			continue
		}
		config[key] = pluginConfig.GetString(key)
	}

	// The service endpoints and token, as the node's config hands them: a
	// service the node is not running is handed as it is.
	atsStoreEndpoint := pluginConfig.GetString("_ats_store_endpoint")
	queueEndpoint := pluginConfig.GetString("_queue_endpoint")
	scheduleEndpoint := pluginConfig.GetString("_schedule_endpoint")
	fileServiceEndpoint := pluginConfig.GetString("_file_service_endpoint")
	llmEndpoint := pluginConfig.GetString("_llm_endpoint")
	embeddingEndpoint := pluginConfig.GetString("_embedding_endpoint")
	vectorSearchEndpoint := pluginConfig.GetString("_vector_search_endpoint")
	groundEndpoint := pluginConfig.GetString("_ground_endpoint")
	searchEndpoint := pluginConfig.GetString("_search_endpoint")
	fetchEndpoint := pluginConfig.GetString("_fetch_endpoint")
	mailEndpoint := pluginConfig.GetString("_mail_endpoint")
	authToken := pluginConfig.GetString("_auth_token")
	// A token the node could not mint for the namespace the plugin stands in
	// fails the Initialize here, rather than starting the plugin on no token.
	if err := configErr(pluginConfig); err != nil {
		return errors.Wrapf(err, "plugin %s was not handed its config", c.metadata.Name)
	}

	// A plugin needing a credential would otherwise need it written literally
	// in am.toml, which ships as an unencrypted parameter and is kept in
	// version control. Resolving here means the secret reaches the plugin
	// without ever being in the file, and plugins need no code for it.
	for key, value := range config {
		if !secretref.IsReference(value) {
			continue
		}

		resolved, err := secretref.Resolve(ctx, value)
		if err != nil {
			// The reference is named, never the value it failed to fetch.
			return errors.Wrapf(err, "failed to resolve the reference configured for %s.%s", c.metadata.Name, key)
		}

		config[key] = resolved
		c.logger.Debugw("Resolved a config reference", "plugin", c.metadata.Name, "key", key)
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
		MailEndpoint:         mailEndpoint,
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
		"mail_endpoint", mailEndpoint,
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

	c.signa = resp.GetSigna()
	c.httpRoutes = resp.GetHttpRoutes()

	// Store Python provider capability
	c.pythonProvider = resp.GetPythonProvider()

	// Store and create watcher registrations. Declaring none withdraws
	// every watcher the plugin declared before.
	c.watchers = resp.GetWatchers()
	if err := SetupPluginWatchers(services.Database(), c.metadata.Name, c.watchers, c.handlerNames, c.logger); err != nil {
		return errors.Wrapf(err, "plugin %s at %s: its watchers were not set up", c.metadata.Name, c.addr)
	}
	c.OnWatchersSetup()

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

// RegisterElements returns custom element type definitions from the remote plugin.
func (c *ExternalDomainProxy) RegisterElements(ctx context.Context) (*protocol.ElementDefResponse, error) {
	resp, err := c.client.RegisterElements(ctx, &protocol.Empty{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get element definitions from plugin %s at %s", c.metadata.Name, c.addr)
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

// Traffic is what the proxy carried to this plugin.
func (c *ExternalDomainProxy) Traffic() *Traffic {
	return c.traffic
}

// proxyHTTPRequest forwards an HTTP request to the remote plugin.
// Tries stripped path first (without /api/{plugin}), then full path if 404 (Issue #277).
// This allows plugins to register routes either way without friction.
func (c *ExternalDomainProxy) proxyHTTPRequest(w http.ResponseWriter, r *http.Request) {
	// Counted once per request, as it was answered. A request the browser
	// abandoned was answered by nobody, and is not counted.
	started := time.Now()

	// Read request body. A server's request always has one.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.count(r, http.StatusInternalServerError, started)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Convert HTTP headers to protocol format
	headers := make([]*protocol.HTTPHeader, 0, len(r.Header))
	for name, values := range r.Header {
		if askerHeaders[http.CanonicalHeaderKey(name)] {
			continue
		}
		headers = append(headers, &protocol.HTTPHeader{
			Name:   name,
			Values: values,
		})
	}
	headers = append(headers, askerFrom(r.Context())...)

	// Calculate both stripped and full paths
	originalPath := r.URL.Path
	strippedPath := c.strippedPath(originalPath)

	// Try stripped path first (modern approach: plugins don't need to know mount point)
	req := &protocol.HTTPRequest{
		Method:  r.Method,
		Path:    withQuery(strippedPath, r.URL.RawQuery),
		Headers: headers,
		Body:    body,
	}

	c.logger.Debugw("Proxying to plugin (stripped)", "plugin", c.metadata.Name, "original", originalPath, "trying", strippedPath)
	resp, err := c.client.HandleHTTP(r.Context(), req)

	// If stripped path returns 404, try full path (for LLMs that naturally include prefix)
	if err == nil && resp.StatusCode == 404 && strippedPath != originalPath {
		c.logger.Debugw("Stripped path 404, retrying with full path", "plugin", c.metadata.Name, "full_path", originalPath)
		req.Path = withQuery(originalPath, r.URL.RawQuery)
		resp, err = c.client.HandleHTTP(r.Context(), req)
	}

	// Handle errors — context canceled means the browser navigated away or refreshed.
	// The request is abandoned; no response needed, no error to log.
	if err != nil {
		if ctxErr := r.Context().Err(); ctxErr != nil {
			return
		}
		// Unimplemented/NotFound = plugin doesn't serve this path; debug, not error
		if s, ok := status.FromError(err); ok && (s.Code() == codes.Unimplemented || s.Code() == codes.NotFound) {
			c.logger.Debugw("Plugin does not serve path",
				"plugin", c.metadata.Name,
				"method", r.Method,
				"path", req.Path)
			c.count(r, http.StatusNotFound, started)
			http.Error(w, fmt.Sprintf("Plugin '%s': %s not found", c.metadata.Name, req.Path), http.StatusNotFound)
			return
		}
		c.count(r, http.StatusBadGateway, started)
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

	// Write status and body. A body the client did not take was answered to
	// nobody, and is not counted.
	w.WriteHeader(int(resp.StatusCode))
	if n, err := w.Write(resp.Body); err != nil {
		c.logger.Warnw("Plugin response body not delivered",
			"plugin", c.metadata.Name, "status", resp.StatusCode, "bytes", len(resp.Body), "written", n, "error", err)
		return
	}
	c.count(r, int(resp.StatusCode), started)
}

// count records one request the plugin was asked and how it was answered.
func (c *ExternalDomainProxy) count(r *http.Request, status int, started time.Time) {
	route := routeOf(r.Method, c.strippedPath(r.URL.Path), c.httpRoutes)
	took := time.Since(started)
	c.traffic.record(route, status, took, started)
	attrs := []measure.Attr{
		measure.String(measure.AttrPlugin, c.metadata.Name),
		measure.String(measure.AttrPluginRoute, route),
		measure.String(measure.AttrOutcome, outcomeOf(status)),
	}
	measure.Count(measure.PluginCalled, 1, attrs...)
	measure.Took(measure.PluginTook, took, attrs...)
}

// withQuery is path with the raw query the request carried, as a request URI.
func withQuery(path, rawQuery string) string {
	return (&url.URL{Path: path, RawQuery: rawQuery}).RequestURI()
}

// configErr is why a plugin config could not be handed, for a config that
// keeps one.
func configErr(config plugin.Config) error {
	if keeps, says := config.(interface{ Err() error }); says {
		return keeps.Err()
	}
	return nil
}

// strippedPath is a path under /api/{plugin} with that prefix taken off:
// /api/code -> /, /api/code/x -> /x. Anything else comes back as it was.
func (c *ExternalDomainProxy) strippedPath(originalPath string) string {
	stripped, under := strings.CutPrefix(originalPath, "/api/"+c.metadata.Name)
	if !under {
		return originalPath
	}
	if !strings.HasPrefix(stripped, "/") {
		stripped = "/" + stripped
	}
	return stripped
}

// RegisterWebSocket returns WebSocket handlers that proxy to the remote plugin.
func (c *ExternalDomainProxy) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	// Return a proxy WebSocket handler
	handlers := make(map[string]plugin.WebSocketHandler)

	pluginLogger := c.logger.With("plugin", c.metadata.Name)
	pluginLabel := fmt.Sprintf("%s v%s", c.metadata.Name, c.metadata.Version)
	keepaliveHandler := NewKeepaliveHandler(c.keepaliveConfig, pluginLogger, pluginLabel)

	// Create a proxy handler for the plugin's WebSocket endpoints
	handlers[fmt.Sprintf("/ws/%s", c.metadata.Name)] = &wsProxyHandler{
		client:    c,
		logger:    pluginLogger,
		keepalive: keepaliveHandler,
		wsConfig:  c.wsConfig,
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
	defer func() { sqlclose.Log(wsConn.Close(), h.logger, "the proxied websocket") }()

	// Establish bidirectional gRPC stream
	// Use a standalone context — NOT r.Context(). The HTTP request context
	// cancels when the browser disconnects, which kills the gRPC stream and
	// produces "context canceled" errors. The WebSocket read loop already
	// detects disconnection and sends CLOSE, so we don't need r.Context().
	ctx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	// Each query parameter, with every value it was given.
	md := metadata.New(nil)
	for key, values := range r.URL.Query() {
		md.Set(key, values...)
	}
	ctx = metadata.NewOutgoingContext(ctx, md)
	stream, err := h.client.client.HandleWebSocket(ctx)
	if err != nil {
		// Without this the client sees the socket drop with no reason given.
		closeErr := wsConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Failed to connect to plugin"))
		h.logger.Errorw("Failed to establish gRPC stream", "error", err, "close_reason_not_delivered", closeErr)
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

	// Each direction ends by handing why on errChan, and that is said once,
	// below, where the connection ends.

	// WebSocket -> gRPC stream
	go func() {
		for {
			messageType, data, err := wsConn.ReadMessage()
			if err != nil {
				// A CLOSE the plugin never receives leaves it holding a session
				// for a client that is gone, so its failure travels with err.
				closeErr := stream.Send(&protocol.WebSocketMessage{
					Type:      protocol.WebSocketMessage_CLOSE,
					Timestamp: time.Now().UnixNano(),
				})
				errChan <- errors.WithSecondaryError(err, closeErr)
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
			rawData, decErr := base64.StdEncoding.DecodeString(browserMsg.Data)
			if decErr != nil {
				h.logger.Errorw("Failed to decode base64 data", "error", decErr)
				continue
			}

			protoMsg := &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_Type(browserMsg.Type),
				Data:      rawData,
				Headers:   browserMsg.Headers,
				Timestamp: time.Now().UnixNano(),
			}

			// Send to gRPC stream
			if err := stream.Send(protoMsg); err != nil {
				errChan <- errors.Wrap(err, "failed to send to gRPC stream")
				return
			}
		}
	}()

	// gRPC stream -> WebSocket
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}

			h.logger.Debugw("gRPC -> WebSocket", "type", msg.Type, "size", len(msg.Data))

			// Keepalive messages, routed by what each one is — a PONG needs
			// no reply, and that is the type saying so, not a nil to decode.
			switch msg.Type {
			case protocol.WebSocketMessage_PING:
				// A PONG that cannot be sent is a stream that is gone.
				if err := stream.Send(h.keepalive.HandlePing(msg)); err != nil {
					errChan <- errors.Wrap(err, "failed to send PONG")
					return
				}
				continue
			case protocol.WebSocketMessage_PONG:
				h.keepalive.HandlePong(msg)
				continue
			case protocol.WebSocketMessage_ERROR:
				errChan <- errors.Newf("websocket error: %s", string(msg.Data))
				return
			}

			// Handle CLOSE message from plugin
			if msg.Type == protocol.WebSocketMessage_CLOSE {
				ended := io.EOF
				if closeErr := wsConn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); closeErr != nil {
					ended = errors.Wrap(closeErr, "normal close not delivered to WebSocket client")
				}
				errChan <- ended
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
					errChan <- errors.Wrap(err, "failed to marshal outbound message")
					return
				}
				if err := wsConn.WriteMessage(websocket.TextMessage, jsonBytes); err != nil {
					errChan <- errors.Wrap(err, "WebSocket write error")
					return
				}
			}
		}
	}()

	// Wait for why either direction ended. EOF and the normal closes are a
	// connection ending; Unavailable is the plugin process killed (restart or
	// shutdown), expected.
	err = <-errChan
	if !errors.Is(err, io.EOF) &&
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
	// A health check takes at most 5 seconds, or less when ctx ends sooner.
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.client.Health(healthCtx, &protocol.Empty{})
	if err != nil {
		return plugin.HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("Failed to check plugin health: %v", err),
			Details: map[string]any{
				"error": err.Error(),
			},
		}
	}

	details := make(map[string]any)
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
