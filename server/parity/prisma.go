// Package parity holds a signum, or one sigil of it, to a reference it follows.
//
// "parity the sigil is what an Agent should deal with through MCP." "Make
// parity would just be for the storage backend specifically."
//
// How far a signum is from a reference it follows. Our side is the signum's own
// declaration — which column of the reference each of its fields is — read the
// same way for every signum. Their side is the schema, read as it is. Nothing
// here knows a signum or a reference by name.
//
// A model is a clade and its columns are its items. A model nothing follows is
// one line at 0; a model anything follows opens and lists every column; a model
// at 100 is not shown unless all is asked for.
package parity

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Column is one field of a model of a reference: a scalar of a Prisma model,
// a field of a proto message, or a property of a JSON Schema definition.
type Column struct {
	Name string
	// Type is the reference's own word for it. Prisma's: String, Int, BigInt,
	// Boolean, DateTime, Decimal, Json, Float, Bytes. Proto's: its kind, or map.
	// JSON Schema's: its type, the definition it refers to, its branches, or
	// any.
	Type string
	List bool
	// Required is the reference saying a value must be there.
	Required bool
	// Says is what the reference says of it, in its own words, when it does.
	Says string
	// holds is, for JSON Schema, the types a value of it may be once every
	// definition it refers to is read: what Type names, resolved.
	holds []string
}

// Model is a Prisma model, a proto message or a JSON Schema definition and its
// columns, in the order the schema has them. A Prisma relation is not a column
// of the record, so it is left out.
type Model struct {
	Name string
	// Says is what the reference says of the model, when it does.
	Says    string
	Columns []Column
}

// Schema is a reference as it is read: its models, and which column a field
// of ours of one kind can be held in.
type Schema struct {
	Models []Model
	fits   func(kind protoreflect.Kind, column Column) bool
}

// Prisma is a Prisma schema's models as a reference.
func Prisma(models []Model) Schema {
	return Schema{Models: models, fits: func(kind protoreflect.Kind, column Column) bool {
		return slices.Contains(kindFits[kind], column.Type)
	}}
}

var prismaScalars = []string{"String", "Int", "BigInt", "Boolean", "DateTime", "Decimal", "Json", "Float", "Bytes"}

// ParsePrisma reads the models of a schema.prisma; path is only where it says
// the schema came from. A /// line is Prisma's documentation comment, what the
// schema says of the model or field below it.
func ParsePrisma(path string, raw []byte) ([]Model, error) {
	var models []Model
	var cur *Model
	var doc []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if said, ok := strings.CutPrefix(line, "///"); ok {
			doc = append(doc, said)
			continue
		}
		says := words(strings.Join(doc, "\n"))
		doc = nil
		switch {
		case cur == nil:
			if name, ok := strings.CutPrefix(line, "model "); ok {
				name, _, _ = strings.Cut(name, " ")
				models = append(models, Model{Name: name, Says: says})
				cur = &models[len(models)-1]
			}
		case line == "}":
			cur = nil
		case line == "", strings.HasPrefix(line, "//"), strings.HasPrefix(line, "@@"):
		default:
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			kind := strings.TrimSuffix(parts[1], "?")
			list := strings.HasSuffix(kind, "[]")
			kind = strings.TrimSuffix(kind, "[]")
			if slices.Contains(prismaScalars, kind) {
				cur.Columns = append(cur.Columns, Column{Name: parts[0], Type: kind, List: list, Says: says})
			}
		}
	}
	if len(models) == 0 {
		return nil, errors.Newf("no model found in the schema at %s", path)
	}
	return models, nil
}

// kindFits is which Prisma types a proto kind can be held in.
var kindFits = map[protoreflect.Kind][]string{
	protoreflect.StringKind:   {"String"},
	protoreflect.BoolKind:     {"Boolean"},
	protoreflect.BytesKind:    {"Bytes"},
	protoreflect.Int32Kind:    {"Int", "BigInt", "Decimal"},
	protoreflect.Sint32Kind:   {"Int", "BigInt", "Decimal"},
	protoreflect.Sfixed32Kind: {"Int", "BigInt", "Decimal"},
	protoreflect.Uint32Kind:   {"Int", "BigInt", "Decimal"},
	protoreflect.Fixed32Kind:  {"Int", "BigInt", "Decimal"},
	protoreflect.Int64Kind:    {"BigInt", "Decimal"},
	protoreflect.Sint64Kind:   {"BigInt", "Decimal"},
	protoreflect.Sfixed64Kind: {"BigInt", "Decimal"},
	protoreflect.Uint64Kind:   {"BigInt", "Decimal"},
	protoreflect.Fixed64Kind:  {"BigInt", "Decimal"},
	protoreflect.FloatKind:    {"Float", "Decimal"},
	protoreflect.DoubleKind:   {"Float", "Decimal"},
	protoreflect.MessageKind:  {"Json"},
	protoreflect.EnumKind:     {"Int", "String"},
}

// departs says how a field's kind and cardinality differ from the column it is
// followed into. A map is held as one row per entry, so it may be followed into
// a model's single-valued columns.
func departs(field protoreflect.FieldDescriptor, name string, column Column, fits func(protoreflect.Kind, Column) bool) []string {
	var reasons []string
	if field.IsMap() {
		if column.List {
			reasons = append(reasons, fmt.Sprintf("a list in the schema, and %s is a map", name))
		}
		kind := field.MapValue().Kind()
		if !fits(kind, column) {
			reasons = append(reasons, fmt.Sprintf("%s in the schema, and %s holds %s", column.Type, name, kind))
		}
		return reasons
	}
	switch {
	case field.IsList() && !column.List:
		reasons = append(reasons, fmt.Sprintf("one value in the schema, and %s is repeated", name))
	case !field.IsList() && column.List:
		reasons = append(reasons, fmt.Sprintf("a list in the schema, and %s is one value", name))
	}
	if !fits(field.Kind(), column) {
		reasons = append(reasons, fmt.Sprintf("%s in the schema, and %s is %s", column.Type, name, field.Kind()))
	}
	return reasons
}

// Item is one column of a model: whether it is followed, and how it departs,
// with what the reference says of it and whether it requires it.
type Item struct {
	Column   string
	Followed []string
	Departs  []string
	Says     string
	Required bool
}

// Conforms is followed and departs in nothing.
func (i Item) Conforms() bool { return len(i.Followed) > 0 && len(i.Departs) == 0 }

// MarshalJSON is an item as the sigil gives it: 100 when it conforms, 0 when
// not, as the picture reads it.
func (i Item) MarshalJSON() ([]byte, error) {
	score := 0
	if i.Conforms() {
		score = 100
	}
	return json.Marshal(map[string]any{
		"column": i.Column, "score": score,
		"followed": nonNil(i.Followed), "departs": nonNil(i.Departs),
		"says": i.Says, "required": i.Required,
	})
}

// Clade is one model and its columns, with what the reference says of it.
type Clade struct {
	Model string
	Says  string
	Items []Item
}

// MarshalJSON is a clade as the sigil gives it, with its score.
func (c Clade) MarshalJSON() ([]byte, error) {
	items := c.Items
	if items == nil {
		items = []Item{}
	}
	return json.Marshal(map[string]any{"model": c.Model, "says": c.Says, "score": c.Score(), "items": items})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Score is 0 to 100: the share of the model's columns that are followed and
// depart in nothing. It reads 100 only when every one does.
func (c Clade) Score() int {
	if len(c.Items) == 0 {
		return 0
	}
	n := 0
	for _, i := range c.Items {
		if i.Conforms() {
			n++
		}
	}
	return n * 100 / len(c.Items)
}

func (c Clade) followed() bool {
	for _, i := range c.Items {
		if len(i.Followed) > 0 {
			return true
		}
	}
	return false
}

// Parity is one signum, or one sigil of it, held to one schema.
type Parity struct {
	Signum    string  `json:"signum"`
	Sigil     string  `json:"sigil"`
	Reference string  `json:"reference"`
	Clades    []Clade `json:"clades"`
	// Unfollowed is, per message in scope, its fields that follow no column.
	Unfollowed map[string][]string `json:"unfollowed"`
	// Missing is what the declaration follows into a column the schema lacks.
	Missing []string `json:"missing"`
	// Required is, of the models anything follows, each column the reference
	// requires and nothing follows.
	Required []string `json:"required"`
	// Ours is what each message in scope and each of its fields says of
	// itself, in its own .proto's words, by full name: protocol.Sigil.does.
	Ours map[string]string `json:"ours"`
}

// inScope is the messages a sigil carries, and whole when no sigil is named and
// the signum is held entire.
func inScope(signum *protocol.Signum, named string) (messages map[string]bool, whole bool, refused *protocol.Refusal) {
	if named == "" {
		return map[string]bool{}, true, nil
	}
	var names []string
	for _, s := range signum.GetSigils() {
		names = append(names, s.GetName())
		if s.GetName() != named {
			continue
		}
		messages = map[string]bool{}
		for _, f := range s.GetGives() {
			if f.GetMessage() != "" {
				messages[f.GetMessage()] = true
			}
		}
		if len(messages) == 0 {
			return nil, false, &protocol.Refusal{Why: sigil.Invalid, Param: "sigil",
				Says: fmt.Sprintf("%s %s carries no message, so none of its fields can follow a column", signum.GetName(), named)}
		}
		return messages, false, nil
	}
	return nil, false, &protocol.Refusal{Why: sigil.NotFound, Param: "sigil",
		Says: fmt.Sprintf("%s holds no sigil %s; it holds %s", signum.GetName(), named, strings.Join(names, ", "))}
}

func splitField(full string) (string, string) {
	i := strings.LastIndex(full, ".")
	if i < 0 {
		return "", full
	}
	return full[:i], full[i+1:]
}

// resolves is how many of a reference's columns name a column of the schema.
func resolves(follows *protocol.Follows, columns map[string]Column) int {
	n := 0
	for _, c := range follows.GetColumns() {
		if _, ok := columns[c.GetColumn()]; ok {
			n++
		}
	}
	return n
}

// following is the reference by its name, as the signum declares it follows.
func following(signum *protocol.Signum, reference string) (*protocol.Follows, *protocol.Refusal) {
	var names []string
	for _, f := range signum.GetFollows() {
		if f.GetReference() == reference {
			return f, nil
		}
		names = append(names, f.GetReference())
	}
	if len(names) == 0 {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "signum", Says: signum.GetName() + " follows nothing"}
	}
	return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "reference",
		Says: fmt.Sprintf("%s does not follow %s; it follows %s", signum.GetName(), reference, strings.Join(names, ", "))}
}

// Hold holds a signum, or one sigil of it, to the models of the schema of a
// reference it follows. What is wrong with what was asked is refused as the
// caller's; what is wrong with a declaration or a schema is the node's, and
// refused as failed.
func Hold(signum *protocol.Signum, named, reference string, schema Schema) (Parity, *protocol.Refusal) {
	models := schema.Models
	scope, whole, refused := inScope(signum, named)
	if refused != nil {
		return Parity{}, refused
	}
	follows, refused := following(signum, reference)
	if refused != nil {
		return Parity{}, refused
	}
	columns := map[string]Column{}
	for _, m := range models {
		for _, c := range m.Columns {
			columns[m.Name+"."+c.Name] = c
		}
	}
	if resolves(follows, columns) == 0 {
		return Parity{}, failed("%s follows %s, and the schema has none of its columns", signum.GetName(), reference)
	}

	p := Parity{Signum: signum.GetName(), Sigil: named, Reference: reference, Unfollowed: map[string][]string{}, Missing: []string{}, Required: []string{}}
	followedBy := map[string][]string{}
	departures := map[string][]string{}
	followedFields := map[string]bool{}
	messages := map[string]protoreflect.MessageDescriptor{}

	for _, c := range follows.GetColumns() {
		message, field := splitField(c.GetField())
		if !whole && !scope[message] {
			continue
		}
		found, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(message))
		if err != nil {
			return Parity{}, failed("%s follows %s, and %s is no message", signum.GetName(), c.GetField(), message)
		}
		descriptor := found.Descriptor()
		fd := descriptor.Fields().ByName(protoreflect.Name(field))
		if fd == nil {
			return Parity{}, failed("%s follows %s, and %s has no field %s", signum.GetName(), c.GetField(), message, field)
		}
		messages[message] = descriptor
		followedFields[c.GetField()] = true

		column, ok := columns[c.GetColumn()]
		if !ok {
			p.Missing = append(p.Missing, c.GetField()+" → "+c.GetColumn())
			continue
		}
		followedBy[c.GetColumn()] = append(followedBy[c.GetColumn()], c.GetField())
		departures[c.GetColumn()] = append(departures[c.GetColumn()], departs(fd, c.GetField(), column, schema.fits)...)
	}
	for message := range scope {
		if _, ok := messages[message]; ok {
			continue
		}
		found, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(message))
		if err != nil {
			return Parity{}, failed("%s %s carries %s, which is no message", signum.GetName(), named, message)
		}
		messages[message] = found.Descriptor()
	}

	for message, descriptor := range messages {
		fields := descriptor.Fields()
		for i := 0; i < fields.Len(); i++ {
			name := string(fields.Get(i).Name())
			if !followedFields[message+"."+name] {
				p.Unfollowed[message] = append(p.Unfollowed[message], name)
			}
		}
		slices.Sort(p.Unfollowed[message])
	}
	said, err := ours(messages)
	if err != nil {
		return Parity{}, failed("%v", err)
	}
	p.Ours = said

	for _, m := range models {
		clade := Clade{Model: m.Name, Says: m.Says}
		for _, c := range m.Columns {
			key := m.Name + "." + c.Name
			clade.Items = append(clade.Items, Item{Column: c.Name, Followed: followedBy[key], Departs: departures[key], Says: c.Says, Required: c.Required})
		}
		p.Clades = append(p.Clades, clade)
		if !clade.followed() {
			continue
		}
		for _, c := range m.Columns {
			if c.Required && len(followedBy[m.Name+"."+c.Name]) == 0 {
				p.Required = append(p.Required, m.Name+"."+c.Name)
			}
		}
	}
	return p, nil
}

// Render prints what Hold found.
func (p Parity) Render(all bool) string {
	var b strings.Builder
	scope := p.Signum
	if p.Sigil != "" {
		scope += " " + p.Sigil
	}
	fmt.Fprintf(&b, "\n  %s follows %s\n", scope, p.Reference)

	width := 0
	for _, c := range p.Clades {
		width = max(width, len(c.Model))
		for _, i := range c.Items {
			width = max(width, len(i.Column)+2)
		}
	}

	hidden := 0
	for _, c := range p.Clades {
		switch {
		case !c.followed():
			fmt.Fprintf(&b, "  %-*s  %3d\n", width, c.Model, 0)
		case c.Score() == 100 && !all:
			hidden++
		default:
			fmt.Fprintf(&b, "  %-*s  %3d\n", width, c.Model, c.Score())
			for _, i := range c.Items {
				mark := 0
				if i.Conforms() {
					mark = 100
				}
				line := fmt.Sprintf("    %-*s  %3d  %s", width-2, i.Column, mark, strings.Join(i.Followed, ", "))
				b.WriteString(strings.TrimRight(line, " ") + "\n")
				for _, reason := range i.Departs {
					fmt.Fprintf(&b, "      %s\n", reason)
				}
			}
		}
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "  %d at 100 not shown, all shows them\n", hidden)
	}

	if len(p.Unfollowed) == 0 && len(p.Missing) == 0 && len(p.Required) == 0 {
		b.WriteString("\n  out of spec: none\n\n")
		return b.String()
	}
	b.WriteString("\n  out of spec\n")
	var messages []string
	for m := range p.Unfollowed {
		if len(p.Unfollowed[m]) > 0 {
			messages = append(messages, m)
		}
	}
	slices.Sort(messages)
	for _, m := range messages {
		fmt.Fprintf(&b, "    %s follows no column with %s\n", m, strings.Join(p.Unfollowed[m], ", "))
	}
	for _, missing := range p.Missing {
		fmt.Fprintf(&b, "    %s, which the schema does not have\n", missing)
	}
	for _, required := range p.Required {
		fmt.Fprintf(&b, "    %s is required, and nothing follows it\n", required)
	}
	b.WriteString("\n")
	return b.String()
}

// failed is a refusal that is the node's own: its declaration or its schema.
func failed(format string, args ...any) *protocol.Refusal {
	return &protocol.Refusal{Why: sigil.Failed, Says: fmt.Sprintf(format, args...)}
}
