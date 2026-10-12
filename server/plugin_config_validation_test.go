package server

import (
	"testing"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

func TestTheBuildKeysAndTheNamespaceAreNotThePluginsToValidate(t *testing.T) {
	errs, refused := validateConfigAgainstSchema(map[string]string{
		buildCore:                     "teranos/datapunt@main",
		buildCommand:                  "dub build",
		grpcplugin.PluginNamespaceKey: "Clean",
		"typo":                        "x",
	}, map[string]*protocol.ConfigFieldSchema{})
	if len(errs) != 1 || errs["typo"] == "" || refused == nil {
		t.Fatalf("errors: %v (%v)", errs, refused)
	}
}

// What is validated is the config the record holds after the PUT: a required
// key the body does not name is still there, and one the body removes is not.
func TestValidationSeesTheConfigTheRecordHoldsAfter(t *testing.T) {
	schema := map[string]*protocol.ConfigFieldSchema{
		"bucket": {Type: "string", Required: true},
		"region": {Type: "string"},
	}
	held := map[string]string{"bucket": "media", buildCore: "teranos/staands@main"}

	if errs, refused := validateConfigAgainstSchema(configAfter(held, map[string]string{"region": "eu"}), schema); refused != nil {
		t.Fatalf("a PUT naming only region: %v (%v)", errs, refused)
	}
	if errs, refused := validateConfigAgainstSchema(configAfter(held, map[string]string{"region": ""}), schema); refused != nil {
		t.Fatalf("a PUT removing region: %v (%v)", errs, refused)
	}
	errs, refused := validateConfigAgainstSchema(configAfter(held, map[string]string{"bucket": ""}), schema)
	if refused == nil || errs["bucket"] != "This field is required" {
		t.Fatalf("a PUT removing bucket: %v (%v)", errs, refused)
	}
}

func TestValidateConfigAgainstSchema(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]string
		schema       map[string]*protocol.ConfigFieldSchema
		wantErrors   map[string]string
		wantNoErrors bool
	}{
		{
			name: "valid config with all fields",
			config: map[string]string{
				"max_workers":  "4",
				"timeout_secs": "30",
				"enable_debug": "false",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"max_workers": {
					Type:         "integer",
					Required:     true,
					Min:          &protocol.ConfigFieldSchema_MinValue{MinValue: "1"},
					Max:          &protocol.ConfigFieldSchema_MaxValue{MaxValue: "16"},
					DefaultValue: "4",
				},
				"timeout_secs": {
					Type:         "integer",
					Required:     true,
					Min:          &protocol.ConfigFieldSchema_MinValue{MinValue: "1"},
					Max:          &protocol.ConfigFieldSchema_MaxValue{MaxValue: "300"},
					DefaultValue: "30",
				},
				"enable_debug": {
					Type:         "boolean",
					Required:     false,
					DefaultValue: "false",
				},
			},
			wantNoErrors: true,
		},
		{
			name: "missing required field",
			config: map[string]string{
				"timeout_secs": "30",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"max_workers": {
					Type:     "integer",
					Required: true,
				},
				"timeout_secs": {
					Type:     "integer",
					Required: true,
					Min:      &protocol.ConfigFieldSchema_UnboundedBelow{UnboundedBelow: &protocol.Unbounded{}},
					Max:      &protocol.ConfigFieldSchema_UnboundedAbove{UnboundedAbove: &protocol.Unbounded{}},
				},
			},
			wantErrors: map[string]string{
				"max_workers": "This field is required",
			},
		},
		{
			name: "required field holding nothing",
			config: map[string]string{
				"api_key": "",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"api_key": {
					Type:     "string",
					Required: true,
				},
			},
			wantErrors: map[string]string{
				"api_key": "This field is required: api_key names nothing",
			},
		},
		{
			name: "number field naming no bounds",
			config: map[string]string{
				"ratio": "0.5",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"ratio": {
					Type: "number",
					Max:  &protocol.ConfigFieldSchema_UnboundedAbove{UnboundedAbove: &protocol.Unbounded{}},
				},
			},
			wantErrors: map[string]string{
				"ratio": "The plugin's schema names no min_value for this field, and not that it is unbounded below",
			},
		},
		{
			name: "number field unbounded on both sides",
			config: map[string]string{
				"ratio": "12345.5",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"ratio": {
					Type: "number",
					Min:  &protocol.ConfigFieldSchema_UnboundedBelow{UnboundedBelow: &protocol.Unbounded{}},
					Max:  &protocol.ConfigFieldSchema_UnboundedAbove{UnboundedAbove: &protocol.Unbounded{}},
				},
			},
			wantNoErrors: true,
		},
		{
			name: "integer out of range - exceeds max",
			config: map[string]string{
				"max_workers": "999",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"max_workers": {
					Type: "integer",
					Min:  &protocol.ConfigFieldSchema_MinValue{MinValue: "1"},
					Max:  &protocol.ConfigFieldSchema_MaxValue{MaxValue: "16"},
				},
			},
			wantErrors: map[string]string{
				"max_workers": "Must be at most 16",
			},
		},
		{
			name: "integer out of range - below min",
			config: map[string]string{
				"timeout_secs": "-1",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"timeout_secs": {
					Type: "integer",
					Min:  &protocol.ConfigFieldSchema_MinValue{MinValue: "1"},
					Max:  &protocol.ConfigFieldSchema_MaxValue{MaxValue: "300"},
				},
			},
			wantErrors: map[string]string{
				"timeout_secs": "Must be at least 1",
			},
		},
		{
			name: "invalid integer type",
			config: map[string]string{
				"max_workers": "not-a-number",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"max_workers": {
					Type: "integer",
				},
			},
			wantErrors: map[string]string{
				"max_workers": "Must be a valid integer",
			},
		},
		{
			name: "invalid boolean value",
			config: map[string]string{
				"enable_debug": "maybe",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"enable_debug": {
					Type: "boolean",
				},
			},
			wantErrors: map[string]string{
				"enable_debug": "Must be 'true' or 'false'",
			},
		},
		{
			name: "unknown field",
			config: map[string]string{
				"unknown_field": "value",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"max_workers": {
					Type: "integer",
				},
			},
			wantErrors: map[string]string{
				"unknown_field": "Unknown configuration field",
			},
		},
		{
			name: "valid boolean values",
			config: map[string]string{
				"enable_debug": "true",
				"verbose":      "false",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"enable_debug": {
					Type: "boolean",
				},
				"verbose": {
					Type: "boolean",
				},
			},
			wantNoErrors: true,
		},
		{
			name: "valid string field",
			config: map[string]string{
				"log_level": "info",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"log_level": {
					Type:         "string",
					DefaultValue: "warn",
				},
			},
			wantNoErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, refused := validateConfigAgainstSchema(tt.config, tt.schema)

			if tt.wantNoErrors {
				if len(errors) > 0 || refused != nil {
					t.Errorf("Expected no errors, got: %v (%v)", errors, refused)
				}
				return
			}
			if refused == nil {
				t.Errorf("Expected the config to be refused, fields: %v", errors)
			}

			// Check that we got the expected errors
			for field, expectedMsg := range tt.wantErrors {
				actualMsg, exists := errors[field]
				if !exists {
					t.Errorf("Expected error for field %q, but none found", field)
					continue
				}
				if actualMsg != expectedMsg {
					t.Errorf("Field %q: expected error %q, got %q", field, expectedMsg, actualMsg)
				}
			}

			// Check for unexpected errors
			for field := range errors {
				if _, expected := tt.wantErrors[field]; !expected {
					t.Errorf("Unexpected error for field %q: %s", field, errors[field])
				}
			}
		})
	}
}
