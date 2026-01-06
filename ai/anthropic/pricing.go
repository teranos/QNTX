package anthropic

// ModelPricing contains per-token pricing information for Anthropic models
// Prices are in USD per million tokens
type ModelPricing struct {
	InputPrice  float64 // USD per 1M input tokens
	OutputPrice float64 // USD per 1M output tokens
}

// modelPricing contains pricing for Anthropic Claude models
// Updated: January 2025
// Source: https://www.anthropic.com/pricing
var modelPricing = map[string]ModelPricing{
	// Claude 4 models (2025)
	"claude-sonnet-4-20250514": {
		InputPrice:  3.00,  // $3.00 per 1M input tokens
		OutputPrice: 15.00, // $15.00 per 1M output tokens
	},
	"claude-opus-4-20250514": {
		InputPrice:  15.00, // $15.00 per 1M input tokens
		OutputPrice: 75.00, // $75.00 per 1M output tokens
	},

	// Claude 3.5 models
	"claude-3-5-sonnet-20241022": {
		InputPrice:  3.00,  // $3.00 per 1M input tokens
		OutputPrice: 15.00, // $15.00 per 1M output tokens
	},
	"claude-3-5-sonnet-latest": {
		InputPrice:  3.00,
		OutputPrice: 15.00,
	},
	"claude-3-5-haiku-20241022": {
		InputPrice:  0.80, // $0.80 per 1M input tokens
		OutputPrice: 4.00, // $4.00 per 1M output tokens
	},
	"claude-3-5-haiku-latest": {
		InputPrice:  0.80,
		OutputPrice: 4.00,
	},

	// Claude 3 models
	"claude-3-opus-20240229": {
		InputPrice:  15.00, // $15.00 per 1M input tokens
		OutputPrice: 75.00, // $75.00 per 1M output tokens
	},
	"claude-3-opus-latest": {
		InputPrice:  15.00,
		OutputPrice: 75.00,
	},
	"claude-3-sonnet-20240229": {
		InputPrice:  3.00,  // $3.00 per 1M input tokens
		OutputPrice: 15.00, // $15.00 per 1M output tokens
	},
	"claude-3-haiku-20240307": {
		InputPrice:  0.25, // $0.25 per 1M input tokens
		OutputPrice: 1.25, // $1.25 per 1M output tokens
	},

	// Aliases for convenience
	"claude-sonnet-4": {
		InputPrice:  3.00,
		OutputPrice: 15.00,
	},
	"claude-opus-4": {
		InputPrice:  15.00,
		OutputPrice: 75.00,
	},
}

// DefaultPricingFallback is the fallback cost per request when model pricing is unknown
// Set to $0.01 (1 cent) per request as a conservative estimate
const DefaultPricingFallback = 0.01

// CalculateCost computes the cost of an API call based on token usage
// Returns cost in USD
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	pricing, found := modelPricing[model]

	if !found {
		// Unknown model - use fallback pricing
		return DefaultPricingFallback
	}

	// Calculate cost: (tokens / 1,000,000) * price_per_million
	inputCost := (float64(inputTokens) / 1_000_000.0) * pricing.InputPrice
	outputCost := (float64(outputTokens) / 1_000_000.0) * pricing.OutputPrice

	return inputCost + outputCost
}

// GetPricing returns pricing information for a model, if available
func GetPricing(model string) (ModelPricing, bool) {
	pricing, found := modelPricing[model]
	return pricing, found
}
