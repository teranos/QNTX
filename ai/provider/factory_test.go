package provider

import (
	"testing"

	"github.com/teranos/QNTX/am"
)

func TestDetermineProvider(t *testing.T) {
	tests := []struct {
		name             string
		config           *am.Config
		explicitProvider string
		expected         ProviderType
	}{
		{
			name: "explicit provider overrides config",
			config: &am.Config{
				LocalInference: am.LocalInferenceConfig{
					Enabled: true,
					BaseURL: "http://localhost:11434",
				},
			},
			explicitProvider: "openrouter",
			expected:         ProviderTypeOpenRouter,
		},
		{
			name: "local enabled and configured",
			config: &am.Config{
				LocalInference: am.LocalInferenceConfig{
					Enabled: true,
					BaseURL: "http://localhost:11434",
				},
			},
			explicitProvider: "",
			expected:         ProviderTypeLocal,
		},
		{
			name: "local enabled but no base URL",
			config: &am.Config{
				LocalInference: am.LocalInferenceConfig{
					Enabled: true,
					BaseURL: "",
				},
			},
			explicitProvider: "",
			expected:         ProviderTypeOpenRouter,
		},
		{
			name: "local disabled",
			config: &am.Config{
				LocalInference: am.LocalInferenceConfig{
					Enabled: false,
					BaseURL: "http://localhost:11434",
				},
			},
			explicitProvider: "",
			expected:         ProviderTypeOpenRouter,
		},
		{
			name: "default to OpenRouter",
			config: &am.Config{
				LocalInference: am.LocalInferenceConfig{
					Enabled: false,
					BaseURL: "",
				},
			},
			explicitProvider: "",
			expected:         ProviderTypeOpenRouter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineProvider(tt.config, tt.explicitProvider)
			if got != tt.expected {
				t.Errorf("DetermineProvider() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestProviderConfig_GetActiveProvider(t *testing.T) {
	tests := []struct {
		name   string
		config *ProviderConfig
		want   ProviderType
	}{
		{
			name: "returns enabled default provider",
			config: &ProviderConfig{
				DefaultProvider: ProviderTypeLocal,
				ProviderPriority: []ProviderType{
					ProviderTypeLocal,
					ProviderTypeOpenRouter,
				},
				Providers: map[ProviderType]bool{
					ProviderTypeLocal:      true,
					ProviderTypeOpenRouter: true,
				},
			},
			want: ProviderTypeLocal,
		},
		{
			name: "falls back to priority when default disabled",
			config: &ProviderConfig{
				DefaultProvider: ProviderTypeLocal,
				ProviderPriority: []ProviderType{
					ProviderTypeLocal,
					ProviderTypeOpenRouter,
				},
				Providers: map[ProviderType]bool{
					ProviderTypeLocal:      false,
					ProviderTypeOpenRouter: true,
				},
			},
			want: ProviderTypeOpenRouter,
		},
		{
			name: "returns first enabled in priority",
			config: &ProviderConfig{
				DefaultProvider: ProviderTypeAnthropic, // not in providers map
				ProviderPriority: []ProviderType{
					ProviderTypeLocal,
					ProviderTypeOpenRouter,
				},
				Providers: map[ProviderType]bool{
					ProviderTypeLocal:      false,
					ProviderTypeOpenRouter: true,
				},
			},
			want: ProviderTypeOpenRouter,
		},
		{
			name: "fallback to OpenRouter when nothing configured",
			config: &ProviderConfig{
				DefaultProvider:  ProviderTypeLocal,
				ProviderPriority: []ProviderType{},
				Providers:        map[ProviderType]bool{},
			},
			want: ProviderTypeOpenRouter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetActiveProvider(); got != tt.want {
				t.Errorf("GetActiveProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}