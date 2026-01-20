package csv

import (
	"strings"

	"github.com/teranos/QNTX/ats/so"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// Action represents a parsed "so csv" action from an ax query
type Action struct {
	// Filename is the output CSV file path
	Filename string `json:"filename"`

	// Delimiter is the CSV delimiter character (default: ",")
	Delimiter string `json:"delimiter,omitempty"`

	// Headers specifies which fields to include (default: all)
	Headers []string `json:"headers,omitempty"`
}

// ParseAction parses a CSV action from ax filter's SoActions
// Expected formats:
//   - so csv "output.csv"
//   - so csv "output.csv" delimiter ";"
//   - so csv "output.csv" headers "id,subject,predicate"
//
// Returns nil if SoActions doesn't start with "csv"
func ParseAction(filter *types.AxFilter) (*Action, error) {
	if filter == nil || len(filter.SoActions) == 0 {
		return nil, nil
	}

	// Check if first action is "csv"
	if strings.ToLower(filter.SoActions[0]) != "csv" {
		return nil, nil
	}

	action := &Action{
		Delimiter: ",", // Default delimiter
	}

	// Parse remaining tokens
	tokens := filter.SoActions[1:]
	if len(tokens) == 0 {
		return nil, errors.New("csv action requires a filename")
	}

	// State machine for parsing
	state := "filename"
	var filenameParts []string

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		lowerToken := strings.ToLower(token)

		switch lowerToken {
		case "delimiter":
			// "delimiter" introduces delimiter specification
			if i+1 < len(tokens) {
				if state == "filename" && len(filenameParts) > 0 {
					action.Filename = so.JoinTemplate(filenameParts)
					filenameParts = nil
				}
				i++
				action.Delimiter = so.StripQuotes(tokens[i])
				state = "done"
			}
		case "headers":
			// "headers" introduces header specification
			if i+1 < len(tokens) {
				if state == "filename" && len(filenameParts) > 0 {
					action.Filename = so.JoinTemplate(filenameParts)
					filenameParts = nil
				}
				i++
				headerStr := so.StripQuotes(tokens[i])
				action.Headers = strings.Split(headerStr, ",")
				// Trim whitespace from each header
				for j := range action.Headers {
					action.Headers[j] = strings.TrimSpace(action.Headers[j])
				}
				state = "done"
			}
		default:
			if state == "filename" {
				filenameParts = append(filenameParts, token)
			}
		}
	}

	// Finalize filename if not set yet
	if action.Filename == "" && len(filenameParts) > 0 {
		action.Filename = so.JoinTemplate(filenameParts)
	}

	if action.Filename == "" {
		return nil, errors.New("csv action requires a non-empty filename")
	}

	return action, nil
}

// Payload represents the data needed to execute a CSV export
type Payload struct {
	AxFilter  types.AxFilter `json:"ax_filter"`
	Filename  string         `json:"filename"`
	Delimiter string         `json:"delimiter,omitempty"`
	Headers   []string       `json:"headers,omitempty"`
}

// GetAxFilter implements so.Payload interface
func (p *Payload) GetAxFilter() types.AxFilter {
	return p.AxFilter
}

// ToPayload converts an Action to a handler Payload
func (a *Action) ToPayload(filter types.AxFilter) (so.Payload, error) {
	// Clear SoActions from the filter since we've extracted the csv action
	filter.SoActions = nil

	return &Payload{
		AxFilter:  filter,
		Filename:  a.Filename,
		Delimiter: a.Delimiter,
		Headers:   a.Headers,
	}, nil
}

// ToPayloadJSON converts an Action to a JSON-encoded payload for job creation
func (a *Action) ToPayloadJSON(filter types.AxFilter) ([]byte, error) {
	return so.ToPayloadJSON(a, filter)
}

// IsCsvAction checks if a filter has a csv so_action
func IsCsvAction(filter *types.AxFilter) bool {
	return so.IsAction(filter, "csv")
}
