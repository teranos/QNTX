package prompt

import (
	"strings"

	"github.com/teranos/QNTX/ats/so"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// Action represents a parsed "so prompt" action from an ax query
type Action struct {
	// Template is the prompt template with {{field}} placeholders
	Template string `json:"template"`

	// SystemPrompt is an optional system instruction
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Provider specifies which LLM provider to use: "openrouter" or "local"
	Provider string `json:"provider,omitempty"`

	// Model overrides the default model
	Model string `json:"model,omitempty"`

	// ResultPredicate is the predicate for result attestations
	ResultPredicate string `json:"result_predicate,omitempty"`
}

// ParseAction parses a prompt action from ax filter's SoActions
// Expected formats:
//   - so prompt "template text"
//   - so prompt "template" with "system prompt"
//   - so prompt "template" model "gpt-4"
//   - so prompt "template" provider local model "llama2"
//
// The bool answers whether the filter is addressed to prompt at all — a
// filter that is not is a named no, never a nil to forget to check. An error
// means it was a prompt action and failed to parse.
func ParseAction(filter *types.AxFilter) (*Action, bool, error) {
	if filter == nil || len(filter.SoActions) == 0 {
		return nil, false, nil
	}

	// Check if first action is "prompt"
	first, rest := filter.SoActions[0], filter.SoActions[1:]
	if strings.ToLower(first) != "prompt" {
		return nil, false, nil
	}

	action := &Action{}

	// Parse remaining tokens
	tokens := rest
	if len(tokens) == 0 {
		return nil, true, errors.New("prompt action requires a template")
	}

	// State machine for parsing
	state := "template"
	var templateParts []string
	var systemParts []string

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		lowerToken := strings.ToLower(token)

		switch lowerToken {
		case "with":
			// "with" introduces system prompt
			if state == "template" && len(templateParts) > 0 {
				action.Template = so.JoinTemplate(templateParts)
				templateParts = nil
				state = "system"
			} else {
				// Part of template/system
				appendToken(&templateParts, &systemParts, state, token)
			}
		case "model":
			// "model" introduces model specification
			if i+1 < len(tokens) {
				if state == "template" && len(templateParts) > 0 {
					action.Template = so.JoinTemplate(templateParts)
				} else if state == "system" && len(systemParts) > 0 {
					action.SystemPrompt = so.JoinTemplate(systemParts)
				}
				i++
				action.Model = tokens[i]
				state = "done"
			}
		case "provider":
			// "provider" introduces provider specification
			if i+1 < len(tokens) {
				if state == "template" && len(templateParts) > 0 {
					action.Template = so.JoinTemplate(templateParts)
				} else if state == "system" && len(systemParts) > 0 {
					action.SystemPrompt = so.JoinTemplate(systemParts)
				}
				i++
				action.Provider = strings.ToLower(tokens[i])
				state = "done"
			}
		case "predicate":
			// "predicate" introduces result predicate
			if i+1 < len(tokens) {
				if state == "template" && len(templateParts) > 0 {
					action.Template = so.JoinTemplate(templateParts)
				} else if state == "system" && len(systemParts) > 0 {
					action.SystemPrompt = so.JoinTemplate(systemParts)
				}
				i++
				action.ResultPredicate = tokens[i]
				state = "done"
			}
		default:
			appendToken(&templateParts, &systemParts, state, token)
		}
	}

	// Finalize remaining parts
	if state == "template" && len(templateParts) > 0 {
		action.Template = so.JoinTemplate(templateParts)
	} else if state == "system" && len(systemParts) > 0 {
		action.SystemPrompt = so.JoinTemplate(systemParts)
	}

	if action.Template == "" {
		return nil, true, errors.New("prompt action requires a non-empty template")
	}

	// Validate template
	if err := ValidateTemplate(action.Template); err != nil {
		return nil, true, errors.Wrap(err, "invalid prompt template")
	}

	return action, true, nil
}

// appendToken adds a token to the appropriate slice based on state
func appendToken(templateParts, systemParts *[]string, state, token string) {
	switch state {
	case "template":
		*templateParts = append(*templateParts, token)
	case "system":
		*systemParts = append(*systemParts, token)
	}
}

// ToPayload converts an Action to a handler Payload
func (a *Action) ToPayload(filter types.AxFilter) (so.Payload, error) {
	// Clear SoActions from the filter since we've extracted the prompt action
	filter.SoActions = nil

	return &Payload{
		AxFilter:        filter,
		Template:        a.Template,
		SystemPrompt:    a.SystemPrompt,
		Provider:        a.Provider,
		Model:           a.Model,
		ResultPredicate: a.ResultPredicate,
	}, nil
}

// ToPayloadJSON converts an Action to a JSON-encoded payload for job creation
func (a *Action) ToPayloadJSON(filter types.AxFilter) ([]byte, error) {
	return so.ToPayloadJSON(a, filter)
}

// IsPromptAction checks if a filter has a prompt so_action
func IsPromptAction(filter *types.AxFilter) bool {
	return so.IsAction(filter, "prompt")
}
