package parity

import (
	"slices"
	"strings"

	"github.com/teranos/errors"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// A TypeScript declaration file read as a reference: each exported type or
// interface written as an object is a model, each of its members a column,
// and the JSDoc above either is what it says of itself. A member that is not
// a field — an index or call signature — is not a column.

// ParseTypeScript reads the models of a .d.ts; path is only where it says the
// declarations came from.
func ParseTypeScript(path string, raw []byte) ([]Model, error) {
	var models []Model
	var cur *Model
	var doc []string
	inDoc := false
	depth := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if inDoc || strings.HasPrefix(trimmed, "/**") {
			inner := strings.TrimPrefix(trimmed, "/**")
			closed := strings.HasSuffix(inner, "*/")
			inner = strings.TrimSpace(strings.TrimSuffix(inner, "*/"))
			if !inDoc {
				doc = nil
			}
			inDoc = !closed
			inner = strings.TrimSpace(strings.TrimPrefix(inner, "*"))
			doc = append(doc, inner)
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		says := jsdoc(doc)
		doc = nil

		switch {
		case depth == 0:
			if name, ok := objectDeclared(trimmed); ok {
				models = append(models, Model{Name: name, Says: says})
				cur = &models[len(models)-1]
			}
		case depth == 1 && cur != nil:
			if column, ok := member(trimmed); ok {
				column.Says = says
				cur.Columns = append(cur.Columns, column)
			}
		}
		depth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
		if depth == 0 {
			cur = nil
		}
	}
	models = slices.DeleteFunc(models, func(m Model) bool { return len(m.Columns) == 0 })
	if len(models) == 0 {
		return nil, errors.Newf("no exported type with fields in the declarations at %s", path)
	}
	return models, nil
}

// objectDeclared is the name an exported type or interface written as an
// object is declared under: export type TrackedProperties = {.
func objectDeclared(line string) (string, bool) {
	if rest, ok := strings.CutPrefix(line, "export interface "); ok {
		name, _, _ := strings.Cut(rest, " ")
		return name, strings.HasSuffix(line, "{")
	}
	rest, ok := strings.CutPrefix(line, "export type ")
	if !ok {
		return "", false
	}
	name, value, ok := strings.Cut(rest, "=")
	if !ok || !strings.HasPrefix(strings.TrimSpace(value), "{") {
		return "", false
	}
	return strings.TrimSpace(name), true
}

// member is a field of an object type: name?: type. A member written as an
// object is one, of type object; an index or call signature is none.
func member(line string) (Column, bool) {
	if strings.HasPrefix(line, "[") || strings.HasPrefix(line, "(") || strings.HasPrefix(line, "}") {
		return Column{}, false
	}
	name, kind, ok := strings.Cut(line, ":")
	if !ok {
		return Column{}, false
	}
	name = strings.TrimSpace(name)
	optional := strings.HasSuffix(name, "?")
	name = strings.TrimSuffix(name, "?")
	if name == "" || strings.ContainsAny(name, " (<") {
		return Column{}, false
	}
	kind = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(kind), ";"))
	if open, ok := strings.CutSuffix(kind, "{"); ok {
		kind = strings.TrimSpace(strings.TrimSpace(open) + " object")
	}
	list := strings.HasSuffix(kind, "[]")
	return Column{Name: name, Type: strings.TrimSuffix(kind, "[]"), List: list, Required: !optional}, true
}

// jsdoc is a JSDoc block as prose: its text, then what @description says, then
// each @example as an example. Other tags are not what it says of itself.
func jsdoc(lines []string) string {
	var text []string
	tag := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "@") {
			tag, line, _ = strings.Cut(line, " ")
			switch tag {
			case "@description":
				text = append(text, "", line)
			case "@example":
				text = append(text, "", "e.g. "+line)
			}
			continue
		}
		if tag != "" && tag != "@description" && tag != "@example" {
			continue
		}
		text = append(text, line)
	}
	return words(strings.Join(text, "\n"))
}

// tsPrimitives are the TypeScript types a proto scalar can be held in.
var tsPrimitives = map[protoreflect.Kind][]string{
	protoreflect.StringKind:   {"string"},
	protoreflect.BoolKind:     {"boolean"},
	protoreflect.BytesKind:    {"string"},
	protoreflect.Int32Kind:    {"number"},
	protoreflect.Sint32Kind:   {"number"},
	protoreflect.Sfixed32Kind: {"number"},
	protoreflect.Uint32Kind:   {"number"},
	protoreflect.Fixed32Kind:  {"number"},
	protoreflect.Int64Kind:    {"number"},
	protoreflect.Sint64Kind:   {"number"},
	protoreflect.Sfixed64Kind: {"number"},
	protoreflect.Uint64Kind:   {"number"},
	protoreflect.Fixed64Kind:  {"number"},
	protoreflect.FloatKind:    {"number"},
	protoreflect.DoubleKind:   {"number"},
	protoreflect.EnumKind:     {"string", "number"},
}

// tsFits is whether a field of a kind can be held in a column of a TypeScript
// type, any branch of a union: a scalar in its primitive, a message in any
// type that is not one.
func tsFits(kind protoreflect.Kind, column Column) bool {
	for _, branch := range strings.Split(column.Type, "|") {
		branch = strings.TrimSpace(branch)
		if kind == protoreflect.MessageKind {
			if !slices.Contains([]string{"string", "number", "boolean", "null", "undefined"}, branch) {
				return true
			}
			continue
		}
		if slices.Contains(tsPrimitives[kind], branch) {
			return true
		}
	}
	return false
}
