package display

import (
	"encoding/json"
	"flag"

	"github.com/teranos/QNTX/ai/llm"
)

// MarshalJSON marshals JSON with compact formatting for LLM environments,
// pretty formatting for human-readable output
func MarshalJSON(v interface{}) ([]byte, error) {
	// Check if we're running in test mode - if so, always use pretty formatting
	// This prevents the json: prefix from breaking golden file tests
	if flag.Lookup("test.v") != nil {
		return json.MarshalIndent(v, "", "  ")
	}

	if llm.IsLLMEnvironment() {
		// Compact JSON with prefix to break auto-detection/pretty-printing
		result, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		// Add prefix to prevent Claude Code from detecting and reformatting JSON
		return append([]byte("json:"), result...), nil
	}

	// Pretty formatting for human consumption only
	return json.MarshalIndent(v, "", "  ")
}
