package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/named"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

const (
	// internalKeyPrefix marks internal config keys that should not be exposed via API
	internalKeyPrefix = "_"
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
	// The mux's {name} is a whole path segment, so it names a plugin; one
	// nobody added is refused by its record.
	pluginName := r.PathValue("name")

	// A plugin's config is read and written; any other method is refused.
	answers := map[string]func(http.ResponseWriter, *http.Request, string){
		http.MethodGet: s.handleGetPluginConfig,
		http.MethodPut: s.handleUpdatePluginConfig,
	}
	answer, answered := answers[r.Method]
	if !answered {
		w.Header().Set("Allow", "GET, PUT")
		s.writeRichError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
		return
	}
	answer(w, r, pluginName)
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
		if !strings.HasPrefix(key, internalKeyPrefix) {
			settings[key] = value
		}
	}

	// A plugin that is not running has no schema to say; its config is still its
	// record's. A node running without a plugin manager runs no plugin.
	var schema map[string]any
	if pm, err := s.getPluginManager(); err == nil {
		if pluginClient, ok := pm.GetPlugin(pluginName); ok {
			// Try to get schema from plugin
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()

			// Type assert to ExternalDomainProxy to access ConfigSchema
			if proxy, ok := pluginClient.(*grpcplugin.ExternalDomainProxy); ok {
				schemaResp, err := proxy.ConfigSchema(ctx)
				if err != nil {
					s.writeRichError(w, errors.Wrapf(err, "ConfigSchema RPC of plugin %s failed", pluginName), http.StatusServiceUnavailable)
					return
				}
				// Convert protobuf schema to JSON-friendly map
				schema = make(map[string]any)
				for fieldName, fieldSchema := range schemaResp.GetFields() {
					field := map[string]any{
						"type":          fieldSchema.Type,
						"description":   fieldSchema.Description,
						"default_value": fieldSchema.DefaultValue,
						"required":      fieldSchema.Required,
						"pattern":       fieldSchema.Pattern,
						"element_type":  fieldSchema.ElementType,
					}
					// The form holds a value to the bounds a field names. One
					// naming none is refused when the config is written.
					if least, bounded := fieldSchema.GetMin().(*protocol.ConfigFieldSchema_MinValue); bounded {
						field["min_value"] = least.MinValue
					}
					if most, bounded := fieldSchema.GetMax().(*protocol.ConfigFieldSchema_MaxValue); bounded {
						field["max_value"] = most.MaxValue
					}
					schema[fieldName] = field
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

	response := map[string]any{
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
	var body map[string]json.RawMessage
	if err := readJSON(w, r, &body); err != nil {
		return
	}

	// A plugin nobody added has no record to configure, or to validate for.
	found, err := s.pluginRecords().Added(pluginName)
	if err != nil {
		s.writeRichError(w, errors.Wrapf(err, "failed to read the record of plugin %s", pluginName), http.StatusInternalServerError)
		return
	}
	if !found {
		s.writeRichError(w, errors.Newf("plugin %q was never added: press + in the plugin element", pluginName), http.StatusNotFound)
		return
	}

	// The config is what the record holds after: {} is a plugin with no keys,
	// and a body naming none, or naming null, says nothing to write.
	raw, named := body["config"]
	if !named {
		s.writeRichError(w, errors.Newf("the config of plugin %s was not sent: the body names no config", pluginName), http.StatusBadRequest)
		return
	}
	if string(raw) == "null" {
		s.writeRichError(w, errors.Newf("the config of plugin %s was sent as null: send {} for a plugin with no keys", pluginName), http.StatusBadRequest)
		return
	}
	var config map[string]string
	if err := json.Unmarshal(raw, &config); err != nil {
		s.writeRichError(w, errors.Wrapf(err, "the config of plugin %s is not an object of text values", pluginName), http.StatusBadRequest)
		return
	}
	// validate asks whether the config would be taken, without writing it.
	var validate bool
	if raw, named := body["validate"]; named {
		if err := json.Unmarshal(raw, &validate); err != nil {
			s.writeRichError(w, errors.Wrapf(err, "validate of plugin %s's config is not true or false", pluginName), http.StatusBadRequest)
			return
		}
	}

	// A running plugin says what its config may hold; one that is not running
	// has nobody to ask, so its config is taken as written. A node running
	// without a plugin manager runs no plugin.
	pm, managerErr := s.getPluginManager()
	var running bool
	if managerErr == nil {
		if proxy, ok := pm.GetPlugin(pluginName); ok {
			running = true
			if extProxy, ok := proxy.(*grpcplugin.ExternalDomainProxy); ok {
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				schema, err := extProxy.ConfigSchema(ctx)
				cancel()
				if err != nil {
					s.writeRichError(w, errors.Wrapf(err, "failed to validate the config of plugin %s", pluginName), http.StatusInternalServerError)
					return
				}
				if refused, err := validateConfigAgainstSchema(config, schema.Fields); err != nil {
					respond(w, s.logger, http.StatusBadRequest, map[string]any{
						"success": false,
						"message": "Configuration validation failed",
						"errors":  refused,
					})
					return
				}
			}
		}
	}

	if validate {
		respond(w, s.logger, http.StatusOK, map[string]any{
			"valid":   true,
			"plugin":  pluginName,
			"checked": running,
		})
		return
	}

	if err := s.pluginRecords().ConfigurePlugin(actorOf(r.Context()), pluginName, config); err != nil {
		s.writeRichError(w, errors.Wrapf(err, "failed to write the config of plugin %s", pluginName), http.StatusBadRequest)
		return
	}

	if running {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := pm.ReinitializePlugin(ctx, pluginName, s.services); err != nil {
			s.writeRichError(w, errors.Wrapf(err, "the config of plugin %s was saved, and the plugin was not reinitialized with it", pluginName), http.StatusInternalServerError)
			return
		}
	}

	respond(w, s.logger, http.StatusOK, map[string]any{
		"success": true,
		"message": "Plugin configuration updated successfully",
		"plugin":  pluginName,
		"config":  config,
	})
}

// validateConfigAgainstSchema validates config values against plugin schema
// constraints: what each refused field is told, and the refusal of them all.
func validateConfigAgainstSchema(settings map[string]string, schema map[string]*protocol.ConfigFieldSchema) (map[string]string, error) {
	fields := make(map[string]string)
	var refusedErr error
	refuse := func(fieldName, why string) {
		fields[fieldName] = why
		refusal := errors.Newf("%s: %s", fieldName, why)
		if refusedErr == nil {
			refusedErr = refusal
			return
		}
		refusedErr = errors.WithSecondaryError(refusedErr, refusal)
	}

	// A required field is there and holds something, and what it holds is
	// what its type says.
	for fieldName, fieldSchema := range schema {
		if !fieldSchema.Required {
			continue
		}
		value, exists := settings[fieldName]
		if !exists {
			refuse(fieldName, "This field is required")
			continue
		}
		held, err := named.TextOf(fieldName, value)
		if err != nil {
			refuse(fieldName, "This field is required: "+err.Error())
			continue
		}
		if err := checkConfigValue(held.String(), fieldSchema); err != nil {
			refuse(fieldName, err.Error())
		}
	}

	// Every other value is one the schema names, and is what its type says.
	for fieldName, value := range settings {
		// How QNTX builds the plugin, and where it stands, are QNTX's, not the
		// plugin's to validate.
		if strings.HasPrefix(fieldName, buildConfigPrefix) || fieldName == grpcplugin.PluginNamespaceKey {
			continue
		}
		fieldSchema, schemaExists := schema[fieldName]
		if !schemaExists {
			refuse(fieldName, "Unknown configuration field")
			continue
		}
		// Checked above, as the text it holds.
		if fieldSchema.Required {
			continue
		}
		if err := checkConfigValue(value, fieldSchema); err != nil {
			refuse(fieldName, err.Error())
		}
	}

	return fields, refusedErr
}

// checkConfigValue refuses a value that is not what its field's type says, or
// is past a bound the field names. A type the schema names that is none of
// string, integer, number and boolean is refused.
func checkConfigValue(value string, fieldSchema *protocol.ConfigFieldSchema) error {
	switch fieldSchema.Type {
	case "string":
		// Any text is a string.
		return nil
	case "integer":
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return errors.New("Must be a valid integer")
		}
		parse := func(limit string) (int64, error) { return strconv.ParseInt(limit, 10, 64) }
		if err := atLeast(intVal, fieldSchema, parse, "an integer"); err != nil {
			return err
		}
		return atMost(intVal, fieldSchema, parse, "an integer")
	case "number":
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return errors.New("Must be a valid number")
		}
		parse := func(limit string) (float64, error) { return strconv.ParseFloat(limit, 64) }
		if err := atLeast(floatVal, fieldSchema, parse, "a number"); err != nil {
			return err
		}
		return atMost(floatVal, fieldSchema, parse, "a number")
	case "boolean":
		if value != "true" && value != "false" {
			return errors.New("Must be 'true' or 'false'")
		}
		return nil
	}
	return errors.Newf("Unknown field type: %s", fieldSchema.Type)
}

// atLeast refuses a value under the least the field takes. A number field
// naming neither its least value nor that it has none is refused.
func atLeast[T int64 | float64](value T, fieldSchema *protocol.ConfigFieldSchema, parse func(string) (T, error), as string) error {
	switch least := fieldSchema.GetMin().(type) {
	case *protocol.ConfigFieldSchema_MinValue:
		limit, err := parse(least.MinValue)
		if err != nil {
			return errors.Newf("The plugin's schema says min_value %q, which is not %s", least.MinValue, as)
		}
		if value < limit {
			return errors.Newf("Must be at least %s", least.MinValue)
		}
		return nil
	case *protocol.ConfigFieldSchema_UnboundedBelow:
		return nil
	}
	return errors.Newf("The plugin's schema names no min_value for this field, and not that it is unbounded below")
}

// atMost refuses a value over the greatest the field takes. A number field
// naming neither its greatest value nor that it has none is refused.
func atMost[T int64 | float64](value T, fieldSchema *protocol.ConfigFieldSchema, parse func(string) (T, error), as string) error {
	switch most := fieldSchema.GetMax().(type) {
	case *protocol.ConfigFieldSchema_MaxValue:
		limit, err := parse(most.MaxValue)
		if err != nil {
			return errors.Newf("The plugin's schema says max_value %q, which is not %s", most.MaxValue, as)
		}
		if value > limit {
			return errors.Newf("Must be at most %s", most.MaxValue)
		}
		return nil
	case *protocol.ConfigFieldSchema_UnboundedAbove:
		return nil
	}
	return errors.Newf("The plugin's schema names no max_value for this field, and not that it is unbounded above")
}
