package display

import (
	"fmt"

	"github.com/teranos/QNTX/ai/llm"
	"github.com/spf13/cobra"
)

// ShouldOutputJSON determines if a command should output JSON based on flags and LLM detection
func ShouldOutputJSON(cmd *cobra.Command) bool {
	// Handle nil command gracefully (e.g., when called from result rendering without command context)
	if cmd == nil {
		// If no command context, check LLM environment only
		return llm.IsLLMEnvironment()
	}

	// Check if --json flag was explicitly set
	if cmd.Flags().Changed("json") {
		if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
			return true
		}
		return false
	}

	// Check global --json flag
	if globalFlag, _ := cmd.Root().PersistentFlags().GetBool("json"); globalFlag {
		return true
	}

	// If no explicit flag and we're in LLM environment, default to JSON
	return llm.IsLLMEnvironment()
}

// OutputJSON marshals and prints JSON using display.MarshalJSON
func OutputJSON(v interface{}) error {
	data, err := MarshalJSON(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
