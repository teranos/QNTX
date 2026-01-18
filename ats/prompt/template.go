// Package prompt provides template interpolation for attestation-driven prompts.
// Templates can reference attestation fields using {{field}} syntax:
//   - {{subject}}, {{predicate}}, {{context}}, {{actor}} - string or comma-joined if multiple
//   - {{subjects}}, {{predicates}}, {{contexts}}, {{actors}} - JSON array form
//   - {{temporal}} - ISO8601 timestamp
//   - {{attributes.key}} - access specific attribute
//   - {{attributes}} - full attributes as JSON
//   - {{id}} - attestation ID
package prompt

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// Template represents a parsed prompt template with placeholders for attestation fields
type Template struct {
	raw      string
	segments []segment
}

// segment represents either a literal string or a placeholder
type segment struct {
	literal     bool
	content     string // for literal: the text; for placeholder: the field path
	isAttribute bool   // true if this is an attributes.* path
	attrPath    string // the path after "attributes." if isAttribute
}

// Placeholder patterns
var (
	// Match {{field}} or {{attributes.path}}
	placeholderPattern = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_.]*)\}\}`)
)

// ValidFields lists all valid top-level fields that can be interpolated
var ValidFields = map[string]bool{
	"id":         true,
	"subject":    true, // First or comma-joined
	"subjects":   true, // JSON array
	"predicate":  true, // First or comma-joined
	"predicates": true, // JSON array
	"context":    true, // First or comma-joined
	"contexts":   true, // JSON array
	"actor":      true, // First or comma-joined
	"actors":     true, // JSON array
	"temporal":   true, // ISO8601 timestamp
	"timestamp":  true, // Alias for temporal
	"source":     true,
	"attributes": true, // Full attributes JSON
}

// Parse creates a Template from a raw template string
func Parse(raw string) (*Template, error) {
	if raw == "" {
		return nil, errors.New("empty template")
	}

	t := &Template{raw: raw}

	// Find all placeholder positions
	matches := placeholderPattern.FindAllStringSubmatchIndex(raw, -1)

	if len(matches) == 0 {
		// No placeholders - entire string is literal
		t.segments = []segment{{literal: true, content: raw}}
		return t, nil
	}

	var segments []segment
	lastEnd := 0

	for _, match := range matches {
		// match[0]:match[1] is the full match {{field}}
		// match[2]:match[3] is the captured group (field)
		start, end := match[0], match[1]
		fieldStart, fieldEnd := match[2], match[3]
		field := raw[fieldStart:fieldEnd]

		// Add literal segment before this placeholder
		if start > lastEnd {
			segments = append(segments, segment{
				literal: true,
				content: raw[lastEnd:start],
			})
		}

		// Parse the field
		seg, err := parseField(field)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid placeholder {{%s}}", field)
		}
		segments = append(segments, seg)

		lastEnd = end
	}

	// Add trailing literal if any
	if lastEnd < len(raw) {
		segments = append(segments, segment{
			literal: true,
			content: raw[lastEnd:],
		})
	}

	t.segments = segments
	return t, nil
}

// parseField parses a field reference and validates it
func parseField(field string) (segment, error) {
	// Check for attributes.* path
	if strings.HasPrefix(field, "attributes.") {
		path := strings.TrimPrefix(field, "attributes.")
		if path == "" {
			return segment{}, errors.New("empty attribute path")
		}
		return segment{
			literal:     false,
			content:     field,
			isAttribute: true,
			attrPath:    path,
		}, nil
	}

	// Check if it's a valid top-level field
	if !ValidFields[field] {
		return segment{}, errors.Newf("unknown field '%s'", field)
	}

	return segment{
		literal: false,
		content: field,
	}, nil
}

// Execute interpolates the template with values from an attestation
func (t *Template) Execute(as *types.As) (string, error) {
	if as == nil {
		return "", errors.New("nil attestation")
	}

	var result strings.Builder
	result.Grow(len(t.raw) * 2) // Pre-allocate with some slack

	for _, seg := range t.segments {
		if seg.literal {
			result.WriteString(seg.content)
			continue
		}

		value, err := getFieldValue(as, seg)
		if err != nil {
			return "", errors.Wrapf(err, "failed to get value for {{%s}}", seg.content)
		}
		result.WriteString(value)
	}

	return result.String(), nil
}

// getFieldValue extracts the value for a field from an attestation
func getFieldValue(as *types.As, seg segment) (string, error) {
	if seg.isAttribute {
		return getAttributeValue(as.Attributes, seg.attrPath)
	}

	switch seg.content {
	case "id":
		return as.ID, nil
	case "subject":
		return joinOrFirst(as.Subjects), nil
	case "subjects":
		return toJSON(as.Subjects), nil
	case "predicate":
		return joinOrFirst(as.Predicates), nil
	case "predicates":
		return toJSON(as.Predicates), nil
	case "context":
		return joinOrFirst(as.Contexts), nil
	case "contexts":
		return toJSON(as.Contexts), nil
	case "actor":
		return joinOrFirst(as.Actors), nil
	case "actors":
		return toJSON(as.Actors), nil
	case "temporal", "timestamp":
		return as.Timestamp.Format(time.RFC3339), nil
	case "source":
		return as.Source, nil
	case "attributes":
		return toJSON(as.Attributes), nil
	default:
		return "", errors.Newf("unknown field '%s'", seg.content)
	}
}

// getAttributeValue navigates a dot-separated path in the attributes map
func getAttributeValue(attrs map[string]interface{}, path string) (string, error) {
	if attrs == nil {
		return "", nil // Empty string for missing attributes
	}

	parts := strings.Split(path, ".")
	var current interface{} = attrs

	for i, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return "", nil // Missing key returns empty string
			}
			current = val
		case map[string]string:
			val, ok := v[part]
			if !ok {
				return "", nil
			}
			return val, nil
		default:
			if i < len(parts)-1 {
				return "", errors.Newf("cannot traverse into non-object at '%s'", strings.Join(parts[:i+1], "."))
			}
		}
	}

	// Convert final value to string
	return valueToString(current), nil
}

// valueToString converts any value to a string representation
func valueToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers are float64
		if val == float64(int64(val)) {
			return strings.TrimSuffix(strings.TrimSuffix(
				strings.Replace(toJSON(val), ".0", "", 1), "\""), "\"")
		}
		return toJSON(val)
	case int, int64, int32:
		return toJSON(val)
	default:
		return toJSON(val)
	}
}

// joinOrFirst returns the first element if single, or comma-joined if multiple
func joinOrFirst(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items, ", ")
}

// toJSON marshals a value to JSON string
func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// GetPlaceholders returns all placeholder field names in the template
func (t *Template) GetPlaceholders() []string {
	var placeholders []string
	for _, seg := range t.segments {
		if !seg.literal {
			placeholders = append(placeholders, seg.content)
		}
	}
	return placeholders
}

// Raw returns the original template string
func (t *Template) Raw() string {
	return t.raw
}

// ValidateTemplate checks if a template string is valid without parsing
func ValidateTemplate(raw string) error {
	_, err := Parse(raw)
	return err
}
