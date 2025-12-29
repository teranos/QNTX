package openrouter

import (
	"math"
	"testing"
)

func TestCalculateCost_KnownModels(t *testing.T) {
	tests := []struct {
		name             string
		model            string
		promptTokens     int
		completionTokens int
		expectedCost     float64
		tolerance        float64
	}{
		{
			name:             "GPT-4o mini - small request",
			model:            "openai/gpt-4o-mini",
			promptTokens:     1000,
			completionTokens: 500,
			// ($0.15 * 1000/1M) + ($0.60 * 500/1M) = $0.00015 + $0.0003 = $0.00045
			expectedCost: 0.00045,
			tolerance:    0.0000001,
		},
		{
			name:             "GPT-4o - medium request",
			model:            "openai/gpt-4o",
			promptTokens:     5000,
			completionTokens: 2000,
			// ($2.50 * 5000/1M) + ($10.00 * 2000/1M) = $0.0125 + $0.02 = $0.0325
			expectedCost: 0.0325,
			tolerance:    0.0000001,
		},
		{
			name:             "Claude 3.5 Sonnet - large request",
			model:            "anthropic/claude-3.5-sonnet",
			promptTokens:     10000,
			completionTokens: 5000,
			// ($3.00 * 10000/1M) + ($15.00 * 5000/1M) = $0.03 + $0.075 = $0.105
			expectedCost: 0.105,
			tolerance:    0.0000001,
		},
		{
			name:             "Grok Beta - typical request",
			model:            "x-ai/grok-beta",
			promptTokens:     3000,
			completionTokens: 1500,
			// ($5.00 * 3000/1M) + ($15.00 * 1500/1M) = $0.015 + $0.0225 = $0.0375
			expectedCost: 0.0375,
			tolerance:    0.0000001,
		},
		{
			name:             "Llama 3.1 8B - cheap request",
			model:            "meta-llama/llama-3.1-8b-instruct",
			promptTokens:     2000,
			completionTokens: 2000,
			// ($0.055 * 2000/1M) + ($0.055 * 2000/1M) = $0.00011 + $0.00011 = $0.00022
			expectedCost: 0.00022,
			tolerance:    0.0000001,
		},
		{
			name:             "Zero tokens",
			model:            "openai/gpt-4o-mini",
			promptTokens:     0,
			completionTokens: 0,
			expectedCost:     0.0,
			tolerance:        0.0000001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CalculateCost(tt.model, tt.promptTokens, tt.completionTokens)

			if math.Abs(cost-tt.expectedCost) > tt.tolerance {
				t.Errorf("CalculateCost() = %v, want %v (tolerance %v)", cost, tt.expectedCost, tt.tolerance)
			}
		})
	}
}

func TestCalculateCost_UnknownModel_UsesFallback(t *testing.T) {
	unknownModels := []string{
		"some-random-model",
		"vendor/unknown-model-v2",
		"",
	}

	for _, model := range unknownModels {
		t.Run("Unknown model: "+model, func(t *testing.T) {
			cost := CalculateCost(model, 1000, 500)

			if cost != DefaultPricingFallback {
				t.Errorf("CalculateCost() for unknown model = %v, want fallback %v",
					cost, DefaultPricingFallback)
			}
		})
	}
}

func TestCalculateCost_FallbackIs1Cent(t *testing.T) {
	if DefaultPricingFallback != 0.01 {
		t.Errorf("DefaultPricingFallback = %v, want $0.01", DefaultPricingFallback)
	}
}

func TestGetPricing_KnownModel(t *testing.T) {
	pricing, found := GetPricing("openai/gpt-4o-mini")

	if !found {
		t.Fatal("GetPricing() returned not found for known model")
	}

	if pricing.PromptPrice != 0.15 {
		t.Errorf("PromptPrice = %v, want 0.15", pricing.PromptPrice)
	}

	if pricing.CompletionPrice != 0.60 {
		t.Errorf("CompletionPrice = %v, want 0.60", pricing.CompletionPrice)
	}
}

func TestGetPricing_UnknownModel(t *testing.T) {
	_, found := GetPricing("unknown/model")

	if found {
		t.Error("GetPricing() returned found for unknown model")
	}
}

func TestCalculateCost_LargeTokenCounts(t *testing.T) {
	// Test with 100K tokens to ensure precision
	cost := CalculateCost("openai/gpt-4o-mini", 100000, 50000)

	// ($0.15 * 100000/1M) + ($0.60 * 50000/1M) = $0.015 + $0.03 = $0.045
	expected := 0.045
	tolerance := 0.0000001

	if math.Abs(cost-expected) > tolerance {
		t.Errorf("CalculateCost() with large tokens = %v, want %v", cost, expected)
	}
}

// TestCalculateCost_RealWorldScenarios tests scenarios similar to actual qntx usage
func TestCalculateCost_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name             string
		model            string
		promptTokens     int
		completionTokens int
		description      string
	}{
		{
			name:             "Code fix analysis",
			model:            "x-ai/grok-code-fast-1",
			promptTokens:     3500, // Moderate code context
			completionTokens: 800,  // Fix suggestion
			description:      "Typical qntx code fix operation",
		},
		{
			name:             "JD skill extraction",
			model:            "openai/gpt-4o-mini",
			promptTokens:     2000, // Job description
			completionTokens: 300,  // Extracted skills JSON
			description:      "JD ingestion skill extraction",
		},
		{
			name:             "Large code review",
			model:            "anthropic/claude-3.5-sonnet",
			promptTokens:     15000, // Large PR diff
			completionTokens: 5000,  // Detailed review
			description:      "Full PR review with detailed suggestions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CalculateCost(tt.model, tt.promptTokens, tt.completionTokens)

			// Ensure cost is non-negative
			if cost < 0 {
				t.Errorf("CalculateCost() returned negative cost: %v", cost)
			}

			// Log the cost for manual verification
			t.Logf("%s: $%.6f (%d prompt + %d completion tokens)",
				tt.description, cost, tt.promptTokens, tt.completionTokens)
		})
	}
}
