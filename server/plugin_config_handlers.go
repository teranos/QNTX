package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
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
		s.writeRichError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
	}
}

// handleGetPluginConfig returns the configuration a plugin's record holds, and
// its schema when it is running to say one.
func (s *QNTXServer) handleGetPluginConfig(w http.ResponseWriter, r *http.Request, pluginName string) {
	record, found, err := s.pluginRecords().Plugin(pluginName)
	if err != nil {
		s.writeRichError(w, errors.Wrapf(err, "failed to read the record of plugin %s", pluginName), http.StatusInternalServerError)
		return
	}
	if !found {
		s.writeRichError(w, errors.Newf("plugin %q was never added: press + in the plugin element", pluginName), http.StatusNotFound)
		return
	}
	settings := make(map[string]string, len(record.Config))
	for key, value := range record.Config {
		if len(key) > 0 && key[0] != internalKeyPrefix {
			settings[key] = value
		}
	}

	// A plugin that is not running has no schema to say; its config is still its record's.
	var schema map[string]interface{}
	if pm := s.getPluginManager(); pm != nil {
		if pluginClient, ok := pm.GetPlugin(pluginName); ok {
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
		}
	}

	response := map[string]interface{}{
		"plugin":  pluginName,
		"config":  settings,
		"schema":  schema,
		"repo":    record.Repo,
		"enabled": record.Enabled,
	}

	respond(w, s.logger, http.StatusOK, response)
}

// handleUpdatePluginConfig writes a plugin's config into its record (ADR-043)
// and reinitializes the plugin with it when it is running. A plugin that is not
// running is configured all the same, and starts with this config.
func (s *QNTXServer) handleUpdatePluginConfig(w http.ResponseWriter, r *http.Request, pluginName string) {
	var req struct {
		Config   map[string]string `json:"config"`
		Validate bool              `json:"validate"` // If true, validate config without applying
	}

	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.Config == nil {
		s.writeRichError(w, errors.New("config field required"), http.StatusBadRequest)
		return
	}

	// A running plugin says what its config may hold; one that is not running
	// has nobody to ask, so its config is taken as written.
	pm := s.getPluginManager()
	var running bool
	if pm != nil {
		if proxy, ok := pm.GetPlugin(pluginName); ok {
			running = true
			if extProxy, ok := proxy.(*grpcplugin.ExternalDomainProxy); ok {
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				schema, err := extProxy.ConfigSchema(ctx)
				cancel()
				if err != nil {
					s.logger.Errorw("Failed to get config schema", "error", err, "plugin", pluginName)
					s.writeRichError(w, errors.Wrapf(err, "failed to validate the config of plugin %s", pluginName), http.StatusInternalServerError)
					return
				}
				if validationErrs := validateConfigAgainstSchema(req.Config, schema.Fields); len(validationErrs) > 0 {
					respond(w, s.logger, http.StatusBadRequest, map[string]interface{}{
						"success": false,
						"message": "Configuration validation failed",
						"errors":  validationErrs,
					})
					return
				}
			}
		}
	}

	if req.Validate {
		respond(w, s.logger, http.StatusOK, map[string]interface{}{
			"valid":   true,
			"plugin":  pluginName,
			"checked": running,
		})
		return
	}

	if err := s.pluginRecords().ConfigurePlugin(actorOf(r.Context()), pluginName, req.Config); err != nil {
		s.logger.Errorw("Failed to update plugin config", "error", err, "plugin", pluginName)
		s.writeRichError(w, errors.Wrapf(err, "failed to write the config of plugin %s", pluginName), http.StatusBadRequest)
		return
	}

	if running {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := pm.ReinitializePlugin(ctx, pluginName, s.services); err != nil {
			s.logger.Errorw("Failed to reinitialize plugin", "error", err, "plugin", pluginName)
			respond(w, s.logger, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"message": "Configuration saved but plugin reinitialization failed: " + err.Error(),
				"plugin":  pluginName,
			})
			return
		}
	}

	respond(w, s.logger, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Plugin configuration updated successfully",
		"plugin":  pluginName,
		"config":  req.Config,
	})
}

// validateConfigAgainstSchema validates config values against plugin schema constraints
func validateConfigAgainstSchema(settings map[string]string, schema map[string]*protocol.ConfigFieldSchema) map[string]string {
	errors := make(map[string]string)

	// Check all required fields are present
	for fieldName, fieldSchema := range schema {
		if fieldSchema.Required {
			if value, exists := settings[fieldName]; !exists || value == "" {
				errors[fieldName] = "This field is required"
				continue
			}
		}
	}

	// Validate each provided config value
	for fieldName, value := range settings {
		fieldSchema, schemaExists := schema[fieldName]
		if !schemaExists {
			errors[fieldName] = "Unknown configuration field"
			continue
		}

		// Validate by type
		switch fieldSchema.Type {
		case "integer":
			intVal, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				errors[fieldName] = "Must be a valid integer"
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

		case "number":
			floatVal, err := strconv.ParseFloat(value, 64)
			if err != nil {
				errors[fieldName] = "Must be a valid number"
				continue
			}

			// Check min_value constraint
			if fieldSchema.MinValue != "" {
				minVal, err := strconv.ParseFloat(fieldSchema.MinValue, 64)
				if err == nil && floatVal < minVal {
					errors[fieldName] = fmt.Sprintf("Must be at least %s", fieldSchema.MinValue)
					continue
				}
			}

			// Check max_value constraint
			if fieldSchema.MaxValue != "" {
				maxVal, err := strconv.ParseFloat(fieldSchema.MaxValue, 64)
				if err == nil && floatVal > maxVal {
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
