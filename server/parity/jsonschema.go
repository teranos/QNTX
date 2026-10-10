package parity

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// A JSON Schema read as a reference: each definition under $defs that has
// properties is a model, and each property a column, in the order the schema
// writes them. A property's type is the schema's own word for it; whether a
// field of ours fits is asked of the types it resolves to, every definition it
// refers to read.

// node is the part of a schema a column is read from.
type node struct {
	Type       json.RawMessage `json:"type"`
	Ref        string          `json:"$ref"`
	Items      *node           `json:"items"`
	AnyOf      []node          `json:"anyOf"`
	OneOf      []node          `json:"oneOf"`
	AllOf      []node          `json:"allOf"`
	Properties json.RawMessage `json:"properties"`
	Required   []string        `json:"required"`
	// Title is what an OpenAPI schema is called, when it is.
	Title string `json:"title"`
	// Description is what the schema says of it.
	Description string `json:"description"`
}

// defsRef is how a schema refers to one of its own definitions.
const defsRef = "#/$defs/"

// ParseJSONSchema reads the models of a JSON Schema's $defs; path is only where
// it says the schema came from.
func ParseJSONSchema(path string, raw []byte) (Schema, error) {
	var root struct {
		Defs json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return Schema{}, errors.Wrapf(err, "the JSON Schema at %s did not read", path)
	}
	if len(root.Defs) == 0 {
		return Schema{}, errors.Newf("no $defs in the JSON Schema at %s", path)
	}
	names, raws, err := inOrder(root.Defs)
	if err != nil {
		return Schema{}, errors.Wrapf(err, "the $defs of %s did not read", path)
	}
	defs := map[string]node{}
	for _, name := range names {
		var def node
		if err := json.Unmarshal(raws[name], &def); err != nil {
			return Schema{}, errors.Wrapf(err, "%s in %s did not read", name, path)
		}
		defs[name] = def
	}

	var models []Model
	for _, name := range names {
		def := defs[name]
		if len(def.Properties) == 0 {
			continue
		}
		props, propRaws, err := inOrder(def.Properties)
		if err != nil {
			return Schema{}, errors.Wrapf(err, "the properties of %s in %s did not read", name, path)
		}
		model := Model{Name: name, Says: def.Description}
		for _, prop := range props {
			var property node
			if err := json.Unmarshal(propRaws[prop], &property); err != nil {
				return Schema{}, errors.Wrapf(err, "%s.%s in %s did not read", name, prop, path)
			}
			types, err := typeWords(property.Type)
			if err != nil {
				return Schema{}, errors.Wrapf(err, "%s.%s in %s", name, prop, path)
			}
			value, list := property, slices.Equal(types, []string{"array"})
			if list {
				value = node{}
				if property.Items != nil {
					value = *property.Items
				}
			}
			holds, err := typesOf(value, defs, nil)
			if err != nil {
				return Schema{}, errors.Wrapf(err, "%s.%s in %s", name, prop, path)
			}
			word, err := wordOf(value)
			if err != nil {
				return Schema{}, errors.Wrapf(err, "%s.%s in %s", name, prop, path)
			}
			model.Columns = append(model.Columns, Column{
				Name: prop, Type: word, List: list, Required: slices.Contains(def.Required, prop), holds: holds,
				Says: property.Description,
			})
		}
		models = append(models, model)
	}
	if len(models) == 0 {
		return Schema{}, errors.Newf("no definition with properties in the JSON Schema at %s", path)
	}
	return Schema{Models: models, fits: jsonFits}, nil
}

// inOrder is an object's keys in the order written, and the value at each.
func inOrder(raw json.RawMessage) ([]string, map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	if open != json.Delim('{') {
		return nil, nil, errors.Newf("%v is not an object", open)
	}
	var keys []string
	values := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, nil, errors.Newf("%v is not a key", token)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, errors.Wrapf(err, "the value at %s did not read", key)
		}
		keys = append(keys, key)
		values[key] = value
	}
	return keys, values, nil
}

// refName is the definition a $ref names: under a JSON Schema's $defs or an
// OpenAPI description's components, its JSON Pointer escapes read.
func refName(ref string) string {
	name, ok := strings.CutPrefix(ref, defsRef)
	if !ok {
		name, ok = strings.CutPrefix(ref, componentsRef)
	}
	if !ok {
		return ref
	}
	return strings.ReplaceAll(strings.ReplaceAll(name, "~1", "/"), "~0", "~")
}

// typeWords is what type says: one word, or a list of them.
func typeWords(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, errors.Newf("type is %s, neither a word nor a list of words", raw)
	}
	return many, nil
}

// wordOf is what the schema calls a value: its type, the definition it refers
// to, its branches, or any when it says nothing of it.
func wordOf(n node) (string, error) {
	if n.Ref != "" {
		return refName(n.Ref), nil
	}
	types, err := typeWords(n.Type)
	if err != nil {
		return "", err
	}
	switch {
	case len(types) > 0:
		return strings.Join(types, " | "), nil
	case len(n.AnyOf) > 0:
		return branches(n.AnyOf, " | ")
	case len(n.OneOf) > 0:
		return branches(n.OneOf, " | ")
	case len(n.AllOf) > 0:
		return branches(n.AllOf, " & ")
	}
	return "any", nil
}

func branches(nodes []node, sep string) (string, error) {
	var words []string
	for _, n := range nodes {
		word, err := wordOf(n)
		if err != nil {
			return "", err
		}
		if strings.Contains(word, " ") {
			word = "(" + word + ")"
		}
		words = append(words, word)
	}
	return strings.Join(words, sep), nil
}

// typesOf is the types a value may be, every definition it refers to read:
// any of anyOf's and oneOf's branches, what all of allOf's share, and any when
// the schema says nothing of it.
func typesOf(n node, defs map[string]node, reading []string) ([]string, error) {
	if n.Ref != "" {
		name := refName(n.Ref)
		if name == n.Ref {
			return nil, errors.Newf("refers to %s, which is not under $defs or components", n.Ref)
		}
		def, ok := defs[name]
		if !ok {
			return nil, errors.Newf("refers to %s, which the schema does not have", n.Ref)
		}
		if slices.Contains(reading, name) {
			return nil, errors.Newf("%s refers to itself through %s", name, strings.Join(reading, " → "))
		}
		return typesOf(def, defs, slices.Concat(reading, []string{name}))
	}
	types, err := typeWords(n.Type)
	if err != nil {
		return nil, err
	}
	if len(types) > 0 {
		return types, nil
	}
	union := func(nodes []node) ([]string, error) {
		var all []string
		for _, branch := range nodes {
			types, err := typesOf(branch, defs, reading)
			if err != nil {
				return nil, err
			}
			for _, t := range types {
				if !slices.Contains(all, t) {
					all = append(all, t)
				}
			}
		}
		return all, nil
	}
	switch {
	case len(n.AnyOf) > 0:
		return union(n.AnyOf)
	case len(n.OneOf) > 0:
		return union(n.OneOf)
	case len(n.AllOf) > 0:
		var shared []string
		constrained := false
		for _, branch := range n.AllOf {
			types, err := typesOf(branch, defs, reading)
			if err != nil {
				return nil, err
			}
			if slices.Contains(types, "any") {
				continue
			}
			if !constrained {
				shared, constrained = types, true
				continue
			}
			shared = slices.DeleteFunc(shared, func(t string) bool { return !slices.Contains(types, t) })
		}
		if !constrained {
			return []string{"any"}, nil
		}
		return shared, nil
	}
	return []string{"any"}, nil
}

// jsonKinds is which JSON types a proto kind can be held in.
var jsonKinds = map[protoreflect.Kind][]string{
	protoreflect.StringKind:   {"string"},
	protoreflect.BoolKind:     {"boolean"},
	protoreflect.BytesKind:    {"string"},
	protoreflect.Int32Kind:    {"integer", "number"},
	protoreflect.Sint32Kind:   {"integer", "number"},
	protoreflect.Sfixed32Kind: {"integer", "number"},
	protoreflect.Uint32Kind:   {"integer", "number"},
	protoreflect.Fixed32Kind:  {"integer", "number"},
	protoreflect.Int64Kind:    {"integer", "number"},
	protoreflect.Sint64Kind:   {"integer", "number"},
	protoreflect.Sfixed64Kind: {"integer", "number"},
	protoreflect.Uint64Kind:   {"integer", "number"},
	protoreflect.Fixed64Kind:  {"integer", "number"},
	protoreflect.FloatKind:    {"number"},
	protoreflect.DoubleKind:   {"number"},
	protoreflect.MessageKind:  {"object"},
	protoreflect.EnumKind:     {"string", "integer"},
}

func jsonFits(kind protoreflect.Kind, column Column) bool {
	if slices.Contains(column.holds, "any") {
		return true
	}
	return slices.ContainsFunc(jsonKinds[kind], func(t string) bool { return slices.Contains(column.holds, t) })
}
