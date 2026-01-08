package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

const (
	// internalKeyPrefix marks internal config keys that should not be exposed via API
	internalKeyPrefix = '_'
)

// writeRichErrorMethod is a method wrapper for writeRichError that uses the server's logger.
// This is kept for backward compatibility - new code should use writeRichError directly.
func (s *QNTXServer) writeRichError(w http.ResponseWriter, err error, statusCode int) {
	writeRichError(w, s.logger, err, statusCode)
}

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
		err := errors.WithDetail(
			errors.New("plugin name required in URL path"),
			"The URL path must include the plugin name: /api/plugins/{name}/config",
		)
		s.writeRichError(w, err, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetPluginConfig(w, r, pluginName)
	case http.MethodPut:
		s.handleUpdatePluginConfig(w, r, pluginName)
	default:
		s.writeRichError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
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
			if len(configKey) > 0 && configKey[0] != internalKeyPrefix {
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
					wrappedErr := errors.Wrap(err, "ConfigSchema RPC failed")
					s.logger.Warnw("Failed to get config schema from plugin", "plugin", pluginName, "error", err)
					s.writeRichError(w, wrappedErr, http.StatusServiceUnavailable)
					return
				}
			} else {
				// Not an external gRPC plugin
				err := errors.WithDetail(
					errors.Newf("plugin %q does not support configuration", pluginName),
					"This plugin does not implement the ConfigSchema RPC method. Only external gRPC plugins with configuration support can be configured through this API.",
				)
				s.writeRichError(w, err, http.StatusNotImplemented)
				return
			}
		} else {
			err := errors.WithDetail(
				errors.Newf("plugin %q not found", pluginName),
				"The requested plugin is not registered with the plugin manager. Check the plugin name and ensure the plugin is properly installed and loaded.",
			)
			s.writeRichError(w, err, http.StatusNotFound)
			return
		}
	} else {
		err := errors.New("plugin manager not initialized")
		s.writeRichError(w, err, http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"plugin": pluginName,
		"config": config,
		"schema": schema,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode plugin config response", "error", err, "plugin", pluginName)
		s.writeRichError(w, errors.Wrap(err, "failed to encode plugin config response"), http.StatusInternalServerError)
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
		s.writeRichError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if req.Config == nil {
		s.writeRichError(w, errors.New("config field required"), http.StatusBadRequest)
		return
	}

	// If validate-only mode, write to temp file and test initialize
	if req.Validate {
		s.handleValidatePluginConfig(w, r, pluginName, req.Config)
		return
	}

	// Validate config against plugin schema before writing to disk
	if s.pluginManager != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		proxy, ok := s.pluginManager.GetPlugin(pluginName)
		if !ok {
			s.writeRichError(w, errors.Newf("plugin not found: %s", pluginName), http.StatusNotFound)
			return
		}

		// Get schema from plugin (only external plugins support ConfigSchema)
		if extProxy, ok := proxy.(*grpcplugin.ExternalDomainProxy); ok {
			schema, err := extProxy.ConfigSchema(ctx)
			if err != nil {
				s.logger.Errorw("Failed to get config schema", "error", err, "plugin", pluginName)
				s.writeRichError(w, errors.Wrap(err, "failed to validate config"), http.StatusInternalServerError)
				return
			}

			// Validate config against schema
			if validationErrs := validateConfigAgainstSchema(req.Config, schema.Fields); len(validationErrs) > 0 {
				response := map[string]interface{}{
					"success": false,
					"message": "Configuration validation failed",
					"errors":  validationErrs,
				}
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(response)
				return
			}
		}
	}

	// Update config in TOML file and viper
	if err := am.UpdatePluginConfig(pluginName, req.Config); err != nil {
		s.logger.Errorw("Failed to update plugin config", "error", err, "plugin", pluginName)
		s.writeRichError(w, errors.Wrap(err, "failed to update config"), http.StatusInternalServerError)
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
		s.writeRichError(w, errors.Wrap(err, "failed to encode response"), http.StatusInternalServerError)
	}
}

// handleValidatePluginConfig validates plugin config without applying changes
func (s *QNTXServer) handleValidatePluginConfig(w http.ResponseWriter, r *http.Request, pluginName string, config map[string]string) {
	// Write config to temp file
	tempPath, err := am.WritePluginConfigToTemp(pluginName, config)
	if err != nil {
		s.logger.Errorw("Failed to write temp config", "error", err, "plugin", pluginName)
		s.writeRichError(w, errors.Wrap(err, "config validation failed"), http.StatusBadRequest)
		return
	}
	defer os.Remove(tempPath)

	// TODO: Test-initialize plugin with temp config
	// This would require launching a test instance of the plugin with the temp config
	// For now, we just validate that the config can be written as valid TOML
	// Future: Call plugin.Initialize() with test config in isolated context

	response := map[string]interface{}{
		"valid":   true,
		"message": "TOML syntax valid. Semantic validation pending (will occur on save)",
		"plugin":  pluginName,
		"warning": "Some invalid values may not be detected until plugin restart",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode validation response", "error", err, "plugin", pluginName)
		s.writeRichError(w, errors.Wrap(err, "failed to encode validation response"), http.StatusInternalServerError)
	}
}

// validateConfigAgainstSchema validates config values against plugin schema constraints
func validateConfigAgainstSchema(config map[string]string, schema map[string]*protocol.ConfigFieldSchema) map[string]string {
	errors := make(map[string]string)

	// Check all required fields are present
	for fieldName, fieldSchema := range schema {
		if fieldSchema.Required {
			if value, exists := config[fieldName]; !exists || value == "" {
				errors[fieldName] = "This field is required"
				continue
			}
		}
	}

	// Validate each provided config value
	for fieldName, value := range config {
		fieldSchema, schemaExists := schema[fieldName]
		if !schemaExists {
			errors[fieldName] = "Unknown configuration field"
			continue
		}

		// Validate by type
		switch fieldSchema.Type {
		case "integer", "number":
			intVal, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				if fieldSchema.Type == "integer" {
					errors[fieldName] = "Must be a valid integer"
				} else {
					errors[fieldName] = "Must be a valid number"
				}
				continue
			}

			// Check min_value constraint
			if fieldSchema.MinValue != "" {
				minVal, err := strconv.ParseInt(fieldSchema.MinValue, 10, 64)
				if err == nil && intVal < minVal {
					errors[fieldName] = fmt.Sprintf("Must be at least %s", fieldSchema.MinValue)
					continue
				}
			}

			// Check max_value constraint
			if fieldSchema.MaxValue != "" {
				maxVal, err := strconv.ParseInt(fieldSchema.MaxValue, 10, 64)
				if err == nil && intVal > maxVal {
					errors[fieldName] = fmt.Sprintf("Must be at most %s", fieldSchema.MaxValue)
					continue
				}
			}

		case "boolean":
			if value != "true" && value != "false" {
				errors[fieldName] = "Must be 'true' or 'false'"
				continue
			}

		case "string":
			// String type - no additional validation needed
			// Could add min_length/max_length in future if needed

		default:
			// Unknown type - shouldn't happen if schema is valid
			errors[fieldName] = fmt.Sprintf("Unknown field type: %s", fieldSchema.Type)
		}
	}

	return errors
}
