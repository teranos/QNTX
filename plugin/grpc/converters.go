package grpc

import (
	"encoding/json"

	"github.com/teranos/QNTX/errors"
)

// attributesFromJSON unmarshals JSON string into a map of attributes.
// Returns an empty map if the JSON string is empty.
func attributesFromJSON(jsonStr string) (map[string]interface{}, error) {
	var attributes map[string]interface{}
	if jsonStr == "" {
		return attributes, nil
	}

	if err := json.Unmarshal([]byte(jsonStr), &attributes); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal attributes")
	}
	return attributes, nil
}

// attributesToJSON marshals a map of attributes into a JSON string.
// Returns an empty string if the attributes map is empty.
func attributesToJSON(attributes map[string]interface{}) (string, error) {
	if len(attributes) == 0 {
		return "", nil
	}

	bytes, err := json.Marshal(attributes)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal attributes")
	}
	return string(bytes), nil
}
