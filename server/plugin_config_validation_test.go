package server

import (
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

func TestValidateConfigAgainstSchema(t *testing.T) {
	tests := []struct {
		name          string
		config        map[string]string
		schema        map[string]*protocol.ConfigFieldSchema
		wantErrors    map[string]string
		wantNoErrors  bool
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
					MinValue:     "1",
					MaxValue:     "16",
					DefaultValue: "4",
				},
				"timeout_secs": {
					Type:         "integer",
					Required:     true,
					MinValue:     "1",
					MaxValue:     "300",
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
				},
			},
			wantErrors: map[string]string{
				"max_workers": "This field is required",
			},
		},
		{
			name: "integer out of range - exceeds max",
			config: map[string]string{
				"max_workers": "999",
			},
			schema: map[string]*protocol.ConfigFieldSchema{
				"max_workers": {
					Type:     "integer",
					MinValue: "1",
					MaxValue: "16",
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
					Type:     "integer",
					MinValue: "1",
					MaxValue: "300",
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
			errors := validateConfigAgainstSchema(tt.config, tt.schema)

			if tt.wantNoErrors {
				if len(errors) > 0 {
					t.Errorf("Expected no errors, got: %v", errors)
				}
				return
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
