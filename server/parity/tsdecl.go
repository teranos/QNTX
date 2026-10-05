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
	// on is the types a model is written on top of: Base in Base & { … }.
	on := map[string][]string{}
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
			if name, bases, ok := objectDeclared(trimmed); ok {
				models = append(models, Model{Name: name, Says: says})
				cur = &models[len(models)-1]
				on[name] = bases
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
	own := map[string][]Column{}
	for _, m := range models {
		own[m.Name] = m.Columns
	}
	for i := range models {
		models[i].Columns = withBases(models[i].Name, own, on, nil)
	}
	models = slices.DeleteFunc(models, func(m Model) bool { return len(m.Columns) == 0 })
	if len(models) == 0 {
		return nil, errors.Newf("no exported type with fields in the declarations at %s", path)
	}
	return models, nil
}

// withBases is a model's columns with those of the types it is written on top
// of before its own. A column it writes again is its own.
func withBases(name string, own map[string][]Column, on map[string][]string, reading []string) []Column {
	mine := own[name]
	var columns []Column
	for _, base := range on[name] {
		if base == name || slices.Contains(reading, base) {
			continue
		}
		for _, c := range withBases(base, own, on, append(reading, name)) {
			again := func(held Column) bool { return held.Name == c.Name }
			if slices.ContainsFunc(mine, again) || slices.ContainsFunc(columns, again) {
				continue
			}
			columns = append(columns, c)
		}
	}
	return append(columns, mine...)
}

// objectDeclared is the name an exported type or interface written as an
// object is declared under, and the types it is written on top of: export type
// Asked = Base & {. A declaration file's `declare` says nothing more.
func objectDeclared(line string) (string, []string, bool) {
	if rest, ok := strings.CutPrefix(line, "export declare "); ok {
		line = "export " + rest
	}
	if !strings.HasSuffix(line, "{") {
		return "", nil, false
	}
	if rest, ok := strings.CutPrefix(line, "export interface "); ok {
		name, extends, _ := strings.Cut(strings.TrimSuffix(rest, "{"), " extends ")
		var bases []string
		for _, base := range strings.Split(extends, ",") {
			if base = typeName(base); base != "" {
				bases = append(bases, base)
			}
		}
		return typeName(name), bases, true
	}
	rest, ok := strings.CutPrefix(line, "export type ")
	if !ok {
		return "", nil, false
	}
	name, value, ok := strings.Cut(rest, "=")
	if !ok {
		return "", nil, false
	}
	// What stands before the object: nothing, or the types it is intersected with.
	before := strings.TrimLeft(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "{")), "(")
	var bases []string
	for _, part := range strings.Split(before, "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !isTypeName(part) {
			return "", nil, false
		}
		bases = append(bases, part)
	}
	return typeName(name), bases, true
}

// typeName is a declared name without what it is generic over: Bag<T> is Bag.
func typeName(declared string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(declared), "<")
	return strings.TrimSpace(name)
}

// isTypeName reports whether a word names one type and is nothing more: no
// union, no call, no literal.
func isTypeName(word string) bool {
	for _, r := range word {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (r < '0' || r > '9') && r != '_' && r != '$' && r != '.' {
			return false
		}
	}
	return word != ""
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
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "readonly "))
	optional := strings.HasSuffix(name, "?")
	name = strings.Trim(strings.TrimSuffix(name, "?"), `'"`)
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

// Shape is models written back as declarations: each a type, each column a
// field, and none of what the declarations they were read from say.
func Shape(models []Model) []byte {
	var shape strings.Builder
	for _, model := range models {
		shape.WriteString("export type " + model.Name + " = {\n")
		for _, column := range model.Columns {
			shape.WriteString("  " + column.Name)
			if !column.Required {
				shape.WriteString("?")
			}
			shape.WriteString(": " + column.Type)
			if column.List {
				shape.WriteString("[]")
			}
			shape.WriteString(";\n")
		}
		shape.WriteString("};\n")
	}
	return []byte(shape.String())
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
