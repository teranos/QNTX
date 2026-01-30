package prompt

import (
	"strings"

	"github.com/teranos/QNTX/ats/so"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
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
// Returns nil if SoActions doesn't start with "prompt"
func ParseAction(filter *types.AxFilter) (*Action, error) {
	if filter == nil || len(filter.SoActions) == 0 {
		return nil, nil
	}

	// Check if first action is "prompt"
	if strings.ToLower(filter.SoActions[0]) != "prompt" {
		return nil, nil
	}

	action := &Action{}

	// Parse remaining tokens
	tokens := filter.SoActions[1:]
	if len(tokens) == 0 {
		return nil, errors.New("prompt action requires a template")
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
		return nil, errors.New("prompt action requires a non-empty template")
	}

	// Validate template
	if err := ValidateTemplate(action.Template); err != nil {
		return nil, errors.Wrap(err, "invalid prompt template")
	}

	return action, nil
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

// ToPayload converts an Action to a handler Payload, clearing SoActions from
// the filter copy since the action has been extracted from them
func (a *Action) ToPayload(filter types.AxFilter) (so.Payload, error) {
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
