package so

import (
	"encoding/json"
	"strings"

	"github.com/teranos/QNTX/ats/types"
)

// Action represents a parsed semantic operation action.
// Each action type (csv, prompt, etc.) implements this interface.
type Action interface {
	// ToPayload converts the action to a payload for job execution
	ToPayload(filter types.AxFilter) (Payload, error)
}

// Payload represents the data needed to execute a semantic operation.
// Each action type defines its own payload structure that implements this interface.
type Payload interface {
	// GetAxFilter returns the query filter for this operation
	GetAxFilter() types.AxFilter
}

// ToPayloadJSON is a helper that converts any Action to JSON-encoded payload.
// This provides the common implementation of ToPayloadJSON for all actions.
func ToPayloadJSON(action Action, filter types.AxFilter) ([]byte, error) {
	payload, err := action.ToPayload(filter)
	if err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

// IsAction checks if a filter has the specified action as its first SoAction token (case-insensitive).
func IsAction(filter *types.AxFilter, actionName string) bool {
	if filter == nil || len(filter.SoActions) == 0 {
		return false
	}
	return strings.ToLower(filter.SoActions[0]) == strings.ToLower(actionName)
}
