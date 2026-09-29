package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// make parity prisma {Signum} {Schema.prisma}
// make parity prisma {Signum} {Sigil} {Schema.prisma}
//
// How far a signum is from a reference it follows. Our side is the signum's own
// declaration — which column of the reference each of its fields is — read the
// same way for every signum. Their side is the schema, read as it is. Nothing
// here knows a signum or a reference by name.
//
// A model is a clade and its columns are its items. A model nothing follows is
// one line at 0; a model anything follows opens and lists every column; a model
// at 100 is not shown unless -all is given.

// Column is one scalar field of a Prisma model.
type Column struct {
	Name string
	// Type is Prisma's: String, Int, BigInt, Boolean, DateTime, Decimal, Json,
	// Float, Bytes.
	Type string
	List bool
}

// Model is a Prisma model and its scalar fields, in the order the schema has
// them. Relations are not columns of the record, so they are left out.
type Model struct {
	Name    string
	Columns []Column
}

var prismaScalars = []string{"String", "Int", "BigInt", "Boolean", "DateTime", "Decimal", "Json", "Float", "Bytes"}

// ParsePrisma reads the models of a schema.prisma.
func ParsePrisma(path string) ([]Model, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the schema at %s", path)
	}
	var models []Model
	var cur *Model
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case cur == nil:
			if name, ok := strings.CutPrefix(line, "model "); ok {
				name, _, _ = strings.Cut(name, " ")
				models = append(models, Model{Name: name})
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
				cur.Columns = append(cur.Columns, Column{Name: parts[0], Type: kind, List: list})
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
func departs(field protoreflect.FieldDescriptor, name string, column Column) []string {
	var reasons []string
	if field.IsMap() {
		if column.List {
			reasons = append(reasons, fmt.Sprintf("a list in the schema, and %s is a map", name))
		}
		kind := field.MapValue().Kind()
		if !slices.Contains(kindFits[kind], column.Type) {
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
	if !slices.Contains(kindFits[field.Kind()], column.Type) {
		reasons = append(reasons, fmt.Sprintf("%s in the schema, and %s is %s", column.Type, name, field.Kind()))
	}
	return reasons
}

// Item is one column of a model: whether it is followed, and how it departs.
type Item struct {
	Column   string
	Followed []string
	Departs  []string
}

// Conforms is followed and departs in nothing.
func (i Item) Conforms() bool { return len(i.Followed) > 0 && len(i.Departs) == 0 }

// Clade is one model and its columns.
type Clade struct {
	Model string
	Items []Item
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
	Signum    string
	Sigil     string
	Reference string
	Clades    []Clade
	// Unfollowed is, per message in scope, its fields that follow no column.
	Unfollowed map[string][]string
	// Missing is what the declaration follows into a column the schema lacks.
	Missing []string
}

func findSignum(signa []*protocol.Signum, name string) (*protocol.Signum, error) {
	var names []string
	for _, s := range signa {
		if s.GetName() == name {
			return s, nil
		}
		names = append(names, s.GetName())
	}
	return nil, errors.Newf("no signum %s; the node holds %s", name, strings.Join(names, ", "))
}

// inScope is the messages a sigil carries, and whole when no sigil is named and
// the signum is held entire.
func inScope(signum *protocol.Signum, sigil string) (messages map[string]bool, whole bool, err error) {
	if sigil == "" {
		return map[string]bool{}, true, nil
	}
	var names []string
	for _, s := range signum.GetSigils() {
		names = append(names, s.GetName())
		if s.GetName() != sigil {
			continue
		}
		messages = map[string]bool{}
		for _, f := range s.GetGives() {
			if f.GetMessage() != "" {
				messages[f.GetMessage()] = true
			}
		}
		if len(messages) == 0 {
			return nil, false, errors.Newf("%s %s carries no message, so none of its fields can follow a column", signum.GetName(), sigil)
		}
		return messages, false, nil
	}
	return nil, false, errors.Newf("%s holds no sigil %s; it holds %s", signum.GetName(), sigil, strings.Join(names, ", "))
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

// pick is the reference the schema is: the one whose columns it names most.
func pick(signum *protocol.Signum, columns map[string]Column) (*protocol.Follows, error) {
	var best *protocol.Follows
	bestN, tied := 0, false
	var names []string
	for _, f := range signum.GetFollows() {
		names = append(names, f.GetReference())
		n := resolves(f, columns)
		switch {
		case n > bestN:
			best, bestN, tied = f, n, false
		case n == bestN && n > 0:
			tied = true
		}
	}
	if len(names) == 0 {
		return nil, errors.Newf("%s follows nothing", signum.GetName())
	}
	if bestN == 0 {
		return nil, errors.Newf("%s follows %s, and the schema has none of their columns", signum.GetName(), strings.Join(names, ", "))
	}
	if tied {
		return nil, errors.Newf("%s follows %s, and the schema is more than one of them", signum.GetName(), strings.Join(names, ", "))
	}
	return best, nil
}

// Hold holds a signum, or one sigil of it, to the models of a schema.
func Hold(signum *protocol.Signum, sigil string, models []Model) (Parity, error) {
	scope, whole, err := inScope(signum, sigil)
	if err != nil {
		return Parity{}, err
	}
	columns := map[string]Column{}
	for _, m := range models {
		for _, c := range m.Columns {
			columns[m.Name+"."+c.Name] = c
		}
	}
	follows, err := pick(signum, columns)
	if err != nil {
		return Parity{}, err
	}

	p := Parity{Signum: signum.GetName(), Sigil: sigil, Reference: follows.GetReference(), Unfollowed: map[string][]string{}}
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
			return Parity{}, errors.Wrapf(err, "%s follows %s, and %s is no message", signum.GetName(), c.GetField(), message)
		}
		descriptor := found.Descriptor()
		fd := descriptor.Fields().ByName(protoreflect.Name(field))
		if fd == nil {
			return Parity{}, errors.Newf("%s follows %s, and %s has no field %s", signum.GetName(), c.GetField(), message, field)
		}
		messages[message] = descriptor
		followedFields[c.GetField()] = true

		column, ok := columns[c.GetColumn()]
		if !ok {
			p.Missing = append(p.Missing, c.GetField()+" → "+c.GetColumn())
			continue
		}
		followedBy[c.GetColumn()] = append(followedBy[c.GetColumn()], c.GetField())
		departures[c.GetColumn()] = append(departures[c.GetColumn()], departs(fd, c.GetField(), column)...)
	}
	for message := range scope {
		if _, ok := messages[message]; ok {
			continue
		}
		found, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(message))
		if err != nil {
			return Parity{}, errors.Wrapf(err, "%s %s carries %s, which is no message", signum.GetName(), sigil, message)
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

	for _, m := range models {
		clade := Clade{Model: m.Name}
		for _, c := range m.Columns {
			key := m.Name + "." + c.Name
			clade.Items = append(clade.Items, Item{Column: c.Name, Followed: followedBy[key], Departs: departures[key]})
		}
		p.Clades = append(p.Clades, clade)
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
		fmt.Fprintf(&b, "  %d at 100 not shown, -all shows them\n", hidden)
	}

	if len(p.Unfollowed) == 0 && len(p.Missing) == 0 {
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
	b.WriteString("\n")
	return b.String()
}

// runPrisma is `parity prisma {Signum} [{Sigil}] {Schema.prisma}`.
func runPrisma(args []string, all bool) (string, error) {
	var signumName, sigil, schema string
	switch len(args) {
	case 2:
		signumName, schema = args[0], args[1]
	case 3:
		signumName, sigil, schema = args[0], args[1], args[2]
	default:
		return "", errors.New("usage: make parity prisma {Signum} [{Sigil}] {Schema.prisma}")
	}
	models, err := ParsePrisma(schema)
	if err != nil {
		return "", err
	}
	signum, err := findSignum(server.DeclaredSigna(), signumName)
	if err != nil {
		return "", err
	}
	p, err := Hold(signum, sigil, models)
	if err != nil {
		return "", err
	}
	return p.Render(all), nil
}
