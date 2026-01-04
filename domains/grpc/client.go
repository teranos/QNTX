package grpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/teranos/QNTX/domains"
	"github.com/teranos/QNTX/domains/grpc/protocol"
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
	metadata domains.Metadata

	// Cached command definitions
	commands []*protocol.CommandDefinition
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
		return nil, fmt.Errorf("failed to get plugin metadata: %w", err)
	}

	proxy.metadata = domains.Metadata{
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
func (c *ExternalDomainProxy) Metadata() domains.Metadata {
	return c.metadata
}

// Initialize initializes the remote plugin.
func (c *ExternalDomainProxy) Initialize(ctx context.Context, services domains.ServiceRegistry) error {
	// Build config map from service registry
	config := make(map[string]string)
	pluginConfig := services.Config(c.metadata.Name)
	// Note: We can only pass string configuration values via gRPC
	// Complex configuration would need to be serialized

	req := &protocol.InitializeRequest{
		// TODO: Expose QNTX service endpoints for plugin callbacks
		// DatabaseEndpoint:  "",
		// AtsStoreEndpoint: "",
		Config: config,
	}

	// Add common configuration keys
	for _, key := range []string{"workspace_root", "api_token", "enabled"} {
		if val := pluginConfig.GetString(key); val != "" {
			config[key] = val
		}
	}

	_, err := c.client.Initialize(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to initialize remote plugin: %w", err)
	}

	// Cache command definitions
	cmdResp, err := c.client.Commands(ctx, &protocol.Empty{})
	if err != nil {
		c.logger.Warnw("Failed to fetch plugin commands", "error", err)
	} else {
		c.commands = cmdResp.Commands
	}

	c.logger.Infow("Remote plugin initialized", "name", c.metadata.Name)
	return nil
}

// Shutdown shuts down the remote plugin.
func (c *ExternalDomainProxy) Shutdown(ctx context.Context) error {
	_, err := c.client.Shutdown(ctx, &protocol.Empty{})
	if err != nil {
		return fmt.Errorf("failed to shutdown remote plugin: %w", err)
	}
	return c.conn.Close()
}

// Commands returns CLI commands that proxy to the remote plugin.
func (c *ExternalDomainProxy) Commands() []*cobra.Command {
	if len(c.commands) == 0 {
		return nil
	}

	cmds := make([]*cobra.Command, 0, len(c.commands))
	for _, cmdDef := range c.commands {
		cmd := c.buildCobraCommand(cmdDef)
		cmds = append(cmds, cmd)
	}
	return cmds
}

// buildCobraCommand creates a Cobra command that proxies execution to the remote plugin.
func (c *ExternalDomainProxy) buildCobraCommand(def *protocol.CommandDefinition) *cobra.Command {
	cmd := &cobra.Command{
		Use:   def.Name,
		Short: def.Description,
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.executeRemoteCommand(cmd, args)
		},
	}

	// Add flags
	for _, flagDef := range def.Flags {
		switch flagDef.Type {
		case "bool":
			defaultVal := flagDef.DefaultValue == "true"
			if flagDef.Short != "" {
				cmd.Flags().BoolP(flagDef.Name, flagDef.Short, defaultVal, flagDef.Description)
			} else {
				cmd.Flags().Bool(flagDef.Name, defaultVal, flagDef.Description)
			}
		case "int":
			// TODO: Parse default value
			if flagDef.Short != "" {
				cmd.Flags().IntP(flagDef.Name, flagDef.Short, 0, flagDef.Description)
			} else {
				cmd.Flags().Int(flagDef.Name, 0, flagDef.Description)
			}
		default: // string
			if flagDef.Short != "" {
				cmd.Flags().StringP(flagDef.Name, flagDef.Short, flagDef.DefaultValue, flagDef.Description)
			} else {
				cmd.Flags().String(flagDef.Name, flagDef.DefaultValue, flagDef.Description)
			}
		}
	}

	// Recursively add subcommands
	for _, subName := range def.Subcommands {
		// Find the subcommand definition
		for _, subDef := range c.commands {
			if subDef.Name == subName {
				cmd.AddCommand(c.buildCobraCommand(subDef))
				break
			}
		}
	}

	return cmd
}

// executeRemoteCommand executes a command on the remote plugin.
func (c *ExternalDomainProxy) executeRemoteCommand(cmd *cobra.Command, args []string) error {
	// Collect flags
	flags := make(map[string]string)
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			flags[f.Name] = f.Value.String()
		}
	})

	req := &protocol.ExecuteCommandRequest{
		Command: cmd.Use,
		Args:    args,
		Flags:   flags,
	}

	resp, err := c.client.ExecuteCommand(context.Background(), req)
	if err != nil {
		return fmt.Errorf("remote command execution failed: %w", err)
	}

	// Write output
	if resp.Stdout != "" {
		cmd.OutOrStdout().Write([]byte(resp.Stdout))
	}
	if resp.Stderr != "" {
		cmd.ErrOrStderr().Write([]byte(resp.Stderr))
	}

	if resp.ExitCode != 0 {
		return fmt.Errorf("command exited with code %d", resp.ExitCode)
	}

	return nil
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

	// Build headers map
	headers := make(map[string]string)
	for key := range r.Header {
		headers[key] = r.Header.Get(key)
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
	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}

	// Write status and body
	w.WriteHeader(int(resp.StatusCode))
	if len(resp.Body) > 0 {
		w.Write(resp.Body)
	}
}

// RegisterWebSocket returns WebSocket handlers that proxy to the remote plugin.
func (c *ExternalDomainProxy) RegisterWebSocket() (map[string]domains.WebSocketHandler, error) {
	// Return a proxy WebSocket handler
	handlers := make(map[string]domains.WebSocketHandler)

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

// ServeWS handles WebSocket upgrade and proxies to remote plugin.
func (h *wsProxyHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement WebSocket proxying via gRPC streaming
	// This requires establishing a bidirectional gRPC stream and bridging
	// WebSocket messages to gRPC messages
	h.logger.Warn("WebSocket proxying not yet implemented")
	http.Error(w, "WebSocket proxying not implemented", http.StatusNotImplemented)
}

// Health returns the remote plugin's health status.
func (c *ExternalDomainProxy) Health(ctx context.Context) domains.HealthStatus {
	resp, err := c.client.Health(ctx, &protocol.Empty{})
	if err != nil {
		return domains.HealthStatus{
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

	return domains.HealthStatus{
		Healthy: resp.Healthy,
		Message: resp.Message,
		Details: details,
	}
}

// Verify ExternalDomainProxy implements DomainPlugin
var _ domains.DomainPlugin = (*ExternalDomainProxy)(nil)
