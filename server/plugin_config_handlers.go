package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/am"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

// HandlePluginConfig handles plugin configuration operations
// GET /api/plugins/{name}/config - Get plugin configuration
// PUT /api/plugins/{name}/config - Update plugin configuration
func (s *QNTXServer) HandlePluginConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse plugin name from path: /api/plugins/{name}/config
	path := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	path = strings.TrimSuffix(path, "/config")
	pluginName := path

	if pluginName == "" {
		http.Error(w, "Plugin name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetPluginConfig(w, r, pluginName)
	case http.MethodPut:
		s.handleUpdatePluginConfig(w, r, pluginName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetPluginConfig returns the current configuration and schema for a plugin
func (s *QNTXServer) handleGetPluginConfig(w http.ResponseWriter, r *http.Request, pluginName string) {
	// Get plugin config from am (viper)
	config := make(map[string]string)

	// Get all keys for this plugin namespace
	for _, key := range am.GetViper().AllKeys() {
		// Check if key starts with plugin namespace
		prefix := pluginName + "."
		if strings.HasPrefix(key, prefix) {
			// Strip prefix to get config key
			configKey := strings.TrimPrefix(key, prefix)
			// Skip internal keys (prefixed with _)
			if len(configKey) > 0 && configKey[0] != '_' {
				config[configKey] = am.GetString(key)
			}
		}
	}

	// Get schema from plugin if available
	var schema map[string]interface{}
	if s.pluginManager != nil {
		if pluginClient, ok := s.pluginManager.GetPlugin(pluginName); ok {
			// Try to get schema from plugin
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()

			// Type assert to ExternalDomainProxy to access ConfigSchema
			if proxy, ok := pluginClient.(*grpcplugin.ExternalDomainProxy); ok {
				if schemaResp, err := proxy.ConfigSchema(ctx); err == nil && schemaResp != nil {
					// Convert protobuf schema to JSON-friendly map
					schema = make(map[string]interface{})
					for fieldName, fieldSchema := range schemaResp.Fields {
						schema[fieldName] = map[string]interface{}{
							"type":          fieldSchema.Type,
							"description":   fieldSchema.Description,
							"default_value": fieldSchema.DefaultValue,
							"required":      fieldSchema.Required,
							"min_value":     fieldSchema.MinValue,
							"max_value":     fieldSchema.MaxValue,
							"pattern":       fieldSchema.Pattern,
							"element_type":  fieldSchema.ElementType,
						}
					}
				} else if err != nil {
					s.logger.Warnw("Failed to get config schema from plugin", "plugin", pluginName, "error", err)
				}
			}
		}
	}

	response := map[string]interface{}{
		"plugin": pluginName,
		"config": config,
		"schema": schema, // May be nil if plugin doesn't support schema
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode plugin config response", "error", err, "plugin", pluginName)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleUpdatePluginConfig updates plugin configuration and reinitializes the plugin
func (s *QNTXServer) handleUpdatePluginConfig(w http.ResponseWriter, r *http.Request, pluginName string) {
	// Parse request body
	var req struct {
		Config   map[string]string `json:"config"`
		Validate bool              `json:"validate"` // If true, validate config without applying
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Config == nil {
		http.Error(w, "Config field required", http.StatusBadRequest)
		return
	}

	// If validate-only mode, write to temp file and test initialize
	if req.Validate {
		s.handleValidatePluginConfig(w, r, pluginName, req.Config)
		return
	}

	// Update config in TOML file and viper
	if err := am.UpdatePluginConfig(pluginName, req.Config); err != nil {
		s.logger.Errorw("Failed to update plugin config", "error", err, "plugin", pluginName)
		http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reinitialize the plugin with new config if it's running
	if s.pluginManager != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := s.pluginManager.ReinitializePlugin(ctx, pluginName, s.services); err != nil {
			s.logger.Errorw("Failed to reinitialize plugin", "error", err, "plugin", pluginName)

			// Config was written but reinitialization failed
			response := map[string]interface{}{
				"success": false,
				"message": "Configuration saved but plugin reinitialization failed: " + err.Error(),
				"plugin":  pluginName,
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	// Success
	response := map[string]interface{}{
		"success": true,
		"message": "Plugin configuration updated successfully",
		"plugin":  pluginName,
		"config":  req.Config,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode response", "error", err, "plugin", pluginName)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleValidatePluginConfig validates plugin config without applying changes
func (s *QNTXServer) handleValidatePluginConfig(w http.ResponseWriter, r *http.Request, pluginName string, config map[string]string) {
	// Write config to temp file
	tempPath, err := am.WritePluginConfigToTemp(pluginName, config)
	if err != nil {
		s.logger.Errorw("Failed to write temp config", "error", err, "plugin", pluginName)
		http.Error(w, "Config validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// TODO: Test-initialize plugin with temp config
	// This would require launching a test instance of the plugin with the temp config
	// For now, we just validate that the config can be written as valid TOML
	// Future: Call plugin.Initialize() with test config in isolated context

	// Clean up temp file
	// Note: We keep the temp file for now in case manual inspection is needed
	// In production, this would be cleaned up after test-initialize
	_ = tempPath

	response := map[string]interface{}{
		"valid":   true,
		"message": "Configuration is valid TOML (full validation requires plugin restart)",
		"plugin":  pluginName,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode validation response", "error", err, "plugin", pluginName)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
