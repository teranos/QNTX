package qntxopenrouter

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
	"openai/gpt-4o":         {PromptPrice: 2.50, CompletionPrice: 10.00},
	"openai/gpt-4o-mini":    {PromptPrice: 0.15, CompletionPrice: 0.60},
	"openai/gpt-4-turbo":    {PromptPrice: 10.00, CompletionPrice: 30.00},
	"openai/gpt-3.5-turbo":  {PromptPrice: 0.50, CompletionPrice: 1.50},

	// Anthropic models via OpenRouter
	"anthropic/claude-3.5-sonnet": {PromptPrice: 3.00, CompletionPrice: 15.00},
	"anthropic/claude-3-opus":     {PromptPrice: 15.00, CompletionPrice: 75.00},
	"anthropic/claude-3-sonnet":   {PromptPrice: 3.00, CompletionPrice: 15.00},
	"anthropic/claude-3-haiku":    {PromptPrice: 0.25, CompletionPrice: 1.25},

	// X.AI models via OpenRouter
	"x-ai/grok-beta":        {PromptPrice: 5.00, CompletionPrice: 15.00},
	"x-ai/grok-code-fast-1": {PromptPrice: 0.50, CompletionPrice: 1.50},

	// Google models via OpenRouter
	"google/gemini-pro-1.5":   {PromptPrice: 1.25, CompletionPrice: 5.00},
	"google/gemini-flash-1.5": {PromptPrice: 0.075, CompletionPrice: 0.30},

	// Meta models via OpenRouter
	"meta-llama/llama-3.1-405b-instruct": {PromptPrice: 2.70, CompletionPrice: 2.70},
	"meta-llama/llama-3.1-70b-instruct":  {PromptPrice: 0.52, CompletionPrice: 0.75},
	"meta-llama/llama-3.1-8b-instruct":   {PromptPrice: 0.055, CompletionPrice: 0.055},
}

// DefaultPricingFallback is the fallback cost per request when model pricing is unknown
const DefaultPricingFallback = 0.01

// CalculateCost computes the cost of an API call based on token usage (USD)
func CalculateCost(model string, promptTokens, completionTokens int) float64 {
	pricing, found := modelPricing[model]
	if !found {
		return DefaultPricingFallback
	}

	promptCost := (float64(promptTokens) / 1_000_000.0) * pricing.PromptPrice
	completionCost := (float64(completionTokens) / 1_000_000.0) * pricing.CompletionPrice
	return promptCost + completionCost
}

// GetPricing returns pricing information for a model, if available
func GetPricing(model string) (ModelPricing, bool) {
	pricing, found := modelPricing[model]
	return pricing, found
}
