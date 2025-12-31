package openrouter

// ModelPricing contains per-token pricing information for OpenRouter models
// Prices are in USD per million tokens
type ModelPricing struct {
	PromptPrice     float64 // USD per 1M prompt tokens
	CompletionPrice float64 // USD per 1M completion tokens
}

// modelPricing contains hardcoded pricing for common OpenRouter models
// TODO: Replace with dynamic pricing - periodically pull from OpenRouter API and store as attestations
var modelPricing = map[string]ModelPricing{
	// OpenAI models via OpenRouter
	"openai/gpt-4o": {
		PromptPrice:     2.50,  // $2.50 per 1M prompt tokens
		CompletionPrice: 10.00, // $10.00 per 1M completion tokens
	},
	"openai/gpt-4o-mini": {
		PromptPrice:     0.15, // $0.15 per 1M prompt tokens
		CompletionPrice: 0.60, // $0.60 per 1M completion tokens
	},
	"openai/gpt-4-turbo": {
		PromptPrice:     10.00, // $10.00 per 1M prompt tokens
		CompletionPrice: 30.00, // $30.00 per 1M completion tokens
	},
	"openai/gpt-3.5-turbo": {
		PromptPrice:     0.50, // $0.50 per 1M prompt tokens
		CompletionPrice: 1.50, // $1.50 per 1M completion tokens
	},

	// Anthropic models via OpenRouter
	"anthropic/claude-3.5-sonnet": {
		PromptPrice:     3.00,  // $3.00 per 1M prompt tokens
		CompletionPrice: 15.00, // $15.00 per 1M completion tokens
	},
	"anthropic/claude-3-opus": {
		PromptPrice:     15.00, // $15.00 per 1M prompt tokens
		CompletionPrice: 75.00, // $75.00 per 1M completion tokens
	},
	"anthropic/claude-3-sonnet": {
		PromptPrice:     3.00,  // $3.00 per 1M prompt tokens
		CompletionPrice: 15.00, // $15.00 per 1M completion tokens
	},
	"anthropic/claude-3-haiku": {
		PromptPrice:     0.25, // $0.25 per 1M prompt tokens
		CompletionPrice: 1.25, // $1.25 per 1M completion tokens
	},

	// X.AI models via OpenRouter
	"x-ai/grok-beta": {
		PromptPrice:     5.00,  // $5.00 per 1M prompt tokens
		CompletionPrice: 15.00, // $15.00 per 1M completion tokens
	},
	"x-ai/grok-code-fast-1": {
		PromptPrice:     0.50, // $0.50 per 1M prompt tokens (estimate)
		CompletionPrice: 1.50, // $1.50 per 1M completion tokens (estimate)
	},

	// Google models via OpenRouter
	"google/gemini-pro-1.5": {
		PromptPrice:     1.25, // $1.25 per 1M prompt tokens
		CompletionPrice: 5.00, // $5.00 per 1M completion tokens
	},
	"google/gemini-flash-1.5": {
		PromptPrice:     0.075, // $0.075 per 1M prompt tokens
		CompletionPrice: 0.30,  // $0.30 per 1M completion tokens
	},

	// Meta models via OpenRouter
	"meta-llama/llama-3.1-405b-instruct": {
		PromptPrice:     2.70, // $2.70 per 1M prompt tokens
		CompletionPrice: 2.70, // $2.70 per 1M completion tokens
	},
	"meta-llama/llama-3.1-70b-instruct": {
		PromptPrice:     0.52, // $0.52 per 1M prompt tokens
		CompletionPrice: 0.75, // $0.75 per 1M completion tokens
	},
	"meta-llama/llama-3.1-8b-instruct": {
		PromptPrice:     0.055, // $0.055 per 1M prompt tokens
		CompletionPrice: 0.055, // $0.055 per 1M completion tokens
	},
}

// DefaultPricingFallback is the fallback cost per request when model pricing is unknown
// Set to $0.01 (1 cent) per request as a conservative estimate
const DefaultPricingFallback = 0.01

// CalculateCost computes the cost of an API call based on token usage
// Returns cost in USD
func CalculateCost(model string, promptTokens, completionTokens int) float64 {
	pricing, found := modelPricing[model]

	if !found {
		// Unknown model - use fallback pricing
		return DefaultPricingFallback
	}

	// Calculate cost: (tokens / 1,000,000) * price_per_million
	promptCost := (float64(promptTokens) / 1_000_000.0) * pricing.PromptPrice
	completionCost := (float64(completionTokens) / 1_000_000.0) * pricing.CompletionPrice

	return promptCost + completionCost
}

// GetPricing returns pricing information for a model, if available
func GetPricing(model string) (ModelPricing, bool) {
	pricing, found := modelPricing[model]
	return pricing, found
}
