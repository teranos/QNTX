package parity

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/teranos/errors"
)

// An OpenAPI description read as a reference: each operation is a model named
// as our .proto names it, METHOD path, its columns the operation's path and
// query parameters and the properties of its JSON body; each schema under
// components that has properties is a model, and so is each object written
// inline in one, named for where it is: issue.pull_request. A schema's columns
// are its properties and those of every branch of it. What an operation
// answers with is the model its 2xx JSON body is, or holds a list of.

// componentsRef is how an OpenAPI description refers to one of its schemas.
const componentsRef = "#/components/schemas/"

type openAPIParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Schema      node   `json:"schema"`
}

type openAPIBody struct {
	Description string `json:"description"`
	Content     map[string]struct {
		Schema *node `json:"schema"`
	} `json:"content"`
}

type openAPIOperation struct {
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Parameters  []openAPIParameter     `json:"parameters"`
	RequestBody *openAPIBody           `json:"requestBody"`
	Responses   map[string]openAPIBody `json:"responses"`
}

// Answers is what an operation answers with: a model, or a list of it.
type Answers struct {
	Model string
	List  bool
}

// ParseOpenAPI reads the models of an OpenAPI description; path is only where
// it says the description came from.
func ParseOpenAPI(path string, raw []byte) (Schema, error) {
	var doc struct {
		Paths      map[string]map[string]openAPIOperation `json:"paths"`
		Components struct {
			Schemas json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Schema{}, errors.Wrapf(err, "the OpenAPI description at %s did not read", path)
	}
	if len(doc.Paths) == 0 {
		return Schema{}, errors.Newf("no paths in the OpenAPI description at %s", path)
	}
	r := openAPIReader{path: path, defs: map[string]node{}}
	var names []string
	if len(doc.Components.Schemas) > 0 {
		var raws map[string]json.RawMessage
		var err error
		names, raws, err = inOrder(doc.Components.Schemas)
		if err != nil {
			return Schema{}, errors.Wrapf(err, "the schemas of %s did not read", path)
		}
		for _, name := range names {
			var def node
			if err := json.Unmarshal(raws[name], &def); err != nil {
				return Schema{}, errors.Wrapf(err, "%s in %s did not read", name, path)
			}
			r.defs[name] = def
		}
	}

	for _, name := range names {
		if err := r.object(name, r.defs[name], nil); err != nil {
			return Schema{}, err
		}
	}
	answers := map[string]Answers{}
	for _, p := range slices.Sorted(maps.Keys(doc.Paths)) {
		for _, method := range slices.Sorted(maps.Keys(doc.Paths[p])) {
			op := doc.Paths[p][method]
			name := strings.ToUpper(method) + " " + p
			if err := r.operation(name, op); err != nil {
				return Schema{}, err
			}
			answer, err := r.answers(name, op)
			if err != nil {
				return Schema{}, err
			}
			if answer.Model != "" {
				answers[name] = answer
			}
		}
	}

	// A column refers to a model only when the reference has one by that name:
	// a schema without properties is a type, not a model.
	modelled := map[string]bool{}
	for _, m := range r.models {
		modelled[m.Name] = true
	}
	for i := range r.models {
		for j := range r.models[i].Columns {
			if c := &r.models[i].Columns[j]; !modelled[c.refers] {
				c.refers = ""
			}
		}
	}
	for name, answer := range answers {
		if !modelled[answer.Model] {
			delete(answers, name)
		}
	}
	return Schema{Models: r.models, fits: jsonFits, answers: answers}, nil
}

type openAPIReader struct {
	path   string
	defs   map[string]node
	models []Model
}

// operation is the model of one operation: its path and query parameters,
// then what its JSON body has.
func (r *openAPIReader) operation(name string, op openAPIOperation) error {
	says := op.Summary
	if op.Description != "" {
		says = strings.TrimSpace(says + "\n\n" + op.Description)
	}
	// A model comes before the ones written inline in it.
	at := len(r.models)
	r.models = append(r.models, Model{Name: name, Says: says})
	var model Model
	for _, p := range op.Parameters {
		if p.In != "path" && p.In != "query" {
			continue
		}
		column, err := r.column(name, p.Name, p.Schema, p.Required)
		if err != nil {
			return err
		}
		column.Says = p.Description
		model.Columns = append(model.Columns, column)
	}
	if op.RequestBody != nil {
		if body, ok := op.RequestBody.Content["application/json"]; ok && body.Schema != nil {
			columns, err := r.columns(name, *body.Schema, nil)
			if err != nil {
				return err
			}
			model.Columns = append(model.Columns, columns...)
		}
	}
	r.models[at].Columns = model.Columns
	return nil
}

// answers is the model an operation's first 2xx JSON body is, or holds a list
// of. A body written inline is a model of its own: GET /x 200.
func (r *openAPIReader) answers(name string, op openAPIOperation) (Answers, error) {
	for _, code := range slices.Sorted(maps.Keys(op.Responses)) {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		body, ok := op.Responses[code].Content["application/json"]
		if !ok || body.Schema == nil {
			return Answers{}, nil
		}
		value, list := *body.Schema, false
		types, err := typeWords(value.Type)
		if err != nil {
			return Answers{}, errors.Wrapf(err, "what %s answers with in %s", name, r.path)
		}
		if slices.Equal(types, []string{"array"}) && value.Items != nil {
			value, list = *value.Items, true
		}
		if refers := r.refers(value); refers != "" {
			return Answers{Model: refers, List: list}, nil
		}
		inline := name + " " + code
		if err := r.object(inline, value, nil); err != nil {
			return Answers{}, err
		}
		return Answers{Model: inline, List: list}, nil
	}
	return Answers{}, nil
}

// object is the model of a schema, when it has columns, and of every object
// written inline in it.
func (r *openAPIReader) object(name string, n node, reading []string) error {
	if slices.ContainsFunc(r.models, func(m Model) bool { return m.Name == name }) {
		return nil
	}
	says := n.Description
	if says == "" {
		says = n.Title
	}
	// A model comes before the ones written inline in it.
	at := len(r.models)
	r.models = append(r.models, Model{Name: name, Says: says})
	columns, err := r.columns(name, n, reading)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		r.models = slices.Delete(r.models, at, at+1)
		return nil
	}
	r.models[at].Columns = columns
	return nil
}

// columns is a schema's properties and those of every branch of it, each
// once: what allOf's branches all have, and what anyOf's and oneOf's may.
// Only the schema's own required, and allOf's, are required.
func (r *openAPIReader) columns(name string, n node, reading []string) ([]Column, error) {
	var columns []Column
	add := func(more []Column) {
		for _, c := range more {
			if !slices.ContainsFunc(columns, func(have Column) bool { return have.Name == c.Name }) {
				columns = append(columns, c)
			}
		}
	}
	if len(n.Properties) > 0 {
		props, raws, err := inOrder(n.Properties)
		if err != nil {
			return nil, errors.Wrapf(err, "the properties of %s in %s did not read", name, r.path)
		}
		for _, prop := range props {
			var property node
			if err := json.Unmarshal(raws[prop], &property); err != nil {
				return nil, errors.Wrapf(err, "%s.%s in %s did not read", name, prop, r.path)
			}
			column, err := r.column(name, prop, property, slices.Contains(n.Required, prop))
			if err != nil {
				return nil, err
			}
			column.Says = property.Description
			add([]Column{column})
		}
	}
	branch := func(b node, required bool) error {
		// What a branch that refers to a schema writes inline is that
		// schema's: issue.pull_request, wherever issue is all of another.
		at := name
		if b.Ref != "" {
			ref := refName(b.Ref)
			if slices.Contains(reading, ref) {
				return nil
			}
			def, ok := r.defs[ref]
			if !ok {
				return errors.Newf("%s in %s refers to %s, which its schemas do not have", name, r.path, b.Ref)
			}
			b, reading, at = def, slices.Concat(reading, []string{ref}), ref
		}
		more, err := r.columns(at, b, reading)
		if err != nil {
			return err
		}
		if !required {
			for i := range more {
				more[i].Required = false
			}
		}
		add(more)
		return nil
	}
	for _, b := range n.AllOf {
		if err := branch(b, true); err != nil {
			return nil, err
		}
	}
	for _, b := range slices.Concat(n.AnyOf, n.OneOf) {
		if err := branch(b, false); err != nil {
			return nil, err
		}
	}
	return columns, nil
}

// column is one property of a model; an object written inline in it is a
// model of its own, named for where it is.
func (r *openAPIReader) column(model, name string, property node, required bool) (Column, error) {
	types, err := typeWords(property.Type)
	if err != nil {
		return Column{}, errors.Wrapf(err, "%s.%s in %s", model, name, r.path)
	}
	value, list := property, slices.Equal(types, []string{"array"})
	if list {
		value = node{}
		if property.Items != nil {
			value = *property.Items
		}
	}
	holds, err := r.typesOf(value)
	if err != nil {
		return Column{}, errors.Wrapf(err, "%s.%s in %s", model, name, r.path)
	}
	word, err := wordOf(value)
	if err != nil {
		return Column{}, errors.Wrapf(err, "%s.%s in %s", model, name, r.path)
	}
	column := Column{Name: name, Type: word, List: list, Required: required, holds: holds, refers: r.refers(value)}
	if column.refers == "" && r.inline(value) {
		column.refers = model + "." + name
		if err := r.object(column.refers, value, nil); err != nil {
			return Column{}, err
		}
	}
	return column, nil
}

// refers is the one schema a value is, when it is one: by $ref, or as the one
// branch of it that refers to anything.
func (r *openAPIReader) refers(n node) string {
	if n.Ref != "" {
		return refName(n.Ref)
	}
	var found []string
	for _, b := range slices.Concat(n.AllOf, n.AnyOf, n.OneOf) {
		if refers := r.refers(b); refers != "" && !slices.Contains(found, refers) {
			found = append(found, refers)
		}
	}
	if len(found) == 1 && !r.inline(n) {
		return found[0]
	}
	return ""
}

// inline is a value written as an object in place: with properties, or with
// branches that have them.
func (r *openAPIReader) inline(n node) bool {
	if n.Ref != "" {
		return false
	}
	if len(n.Properties) > 0 {
		return true
	}
	return slices.ContainsFunc(slices.Concat(n.AllOf, n.AnyOf, n.OneOf), func(b node) bool {
		return b.Ref == "" && r.inline(b)
	})
}

// typesOf is the types a value may be, every schema it refers to read.
func (r *openAPIReader) typesOf(n node) ([]string, error) {
	return typesOf(n, r.defs, nil)
}
