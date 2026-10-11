package parity

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// A message of ours whose comment begins with an HTTP method and a path says it
// is that operation's request: // GET /issues. Each of its fields is the
// parameter or body property of that name, its Response sibling is what the
// operation answers with, field by field, and a field holding a message holds
// the model the column it follows refers to. That is the whole declaration;
// what it follows is read from the messages and the reference, not written
// out by hand.

// Operation is an HTTP method and a path, as a message of ours names it.
type Operation struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// methods are what an operation of ours is named with.
var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// operationOf is the operation a comment begins by naming, if it names one.
func operationOf(says string) (Operation, bool) {
	words := strings.Fields(says)
	if len(words) < 2 || !slices.Contains(methods, words[0]) || !strings.HasPrefix(words[1], "/") {
		return Operation{}, false
	}
	return Operation{Method: words[0], Path: words[1]}, true
}

// naming is each message of ours, of the .proto files under the prefix,
// that names an operation, by full name.
func naming(says map[string]string, files string) (map[string]Operation, error) {
	named := map[string]Operation{}
	for name, said := range says {
		op, ok := operationOf(said)
		if !ok {
			continue
		}
		found, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(name))
		if err != nil {
			return nil, errors.Wrapf(err, "%s names %s %s, and is no message", name, op.Method, op.Path)
		}
		if strings.HasPrefix(found.Descriptor().ParentFile().Path(), protocolDir+files) {
			named[name] = op
		}
	}
	return named, nil
}

// Operations is every operation a message of ours names, of the .proto files
// under the prefix, once each, in order: what a reference's description is
// narrowed to.
func Operations(says map[string]string, files string) ([]Operation, error) {
	named, err := naming(says, files)
	if err != nil {
		return nil, err
	}
	var ops []Operation
	for _, op := range named {
		if !slices.Contains(ops, op) {
			ops = append(ops, op)
		}
	}
	slices.SortFunc(ops, func(a, b Operation) int {
		return strings.Compare(a.Path+" "+a.Method, b.Path+" "+b.Method)
	})
	return ops, nil
}

// WriteOperations is Operations as the nix that narrows a description reads
// it: methods as OpenAPI writes them under a path, in lower case.
func WriteOperations(ops []Operation) ([]byte, error) {
	lower := make([]Operation, len(ops))
	for i, op := range ops {
		lower[i] = Operation{Method: strings.ToLower(op.Method), Path: op.Path}
	}
	body, err := json.MarshalIndent(lower, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "the operations did not marshal")
	}
	return append(body, '\n'), nil
}

// OperationFollows is what the messages of ours that name an operation, of the
// .proto files under the prefix, follow of a reference read from an OpenAPI
// description. A field named in ours is our own and follows nothing: what the
// service adds around the reference's words.
func OperationFollows(reference string, schema Schema, files string, ours ...string) (*protocol.Follows, error) {
	if schema.answers == nil {
		return nil, errors.Newf("%s is not read from an OpenAPI description, so no message of ours names an operation of it", reference)
	}
	columns := map[string]Column{}
	for _, m := range schema.Models {
		for _, c := range m.Columns {
			columns[m.Name+"."+c.Name] = c
		}
	}
	follows := &protocol.Follows{Reference: reference}
	paired := map[string]bool{}
	var pair func(message protoreflect.MessageDescriptor, model string)
	pair = func(message protoreflect.MessageDescriptor, model string) {
		key := string(message.FullName()) + " " + model
		if paired[key] || strings.HasPrefix(string(message.FullName()), "google.protobuf.") {
			return
		}
		paired[key] = true
		fields := message.Fields()
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			name := string(field.Name())
			if slices.Contains(ours, name) {
				continue
			}
			column := model + "." + name
			follows.Columns = append(follows.Columns, &protocol.Corresponds{Field: string(field.FullName()), Column: column})
			if field.Kind() == protoreflect.MessageKind && !field.IsMap() && columns[column].refers != "" {
				pair(field.Message(), columns[column].refers)
			}
		}
	}

	says, err := protocolSaid()
	if err != nil {
		return nil, err
	}
	named, err := naming(says, files)
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		op := named[name]
		request, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(name))
		if err != nil {
			return nil, errors.Wrapf(err, "%s names %s %s, and is no message", name, op.Method, op.Path)
		}
		model := op.Method + " " + op.Path
		pair(request.Descriptor(), model)

		base, ok := strings.CutSuffix(name, "Request")
		answer, answers := schema.answers[model]
		if !ok || !answers {
			continue
		}
		found, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(base + "Response"))
		if err != nil {
			// It answers as another operation does, and says so.
			continue
		}
		response := found.Descriptor()
		if !answer.List {
			pair(response, answer.Model)
			continue
		}
		fields := response.Fields()
		for i := 0; i < fields.Len(); i++ {
			if field := fields.Get(i); field.IsList() && field.Kind() == protoreflect.MessageKind && !slices.Contains(ours, string(field.Name())) {
				pair(field.Message(), answer.Model)
			}
		}
	}
	if len(follows.Columns) == 0 {
		return nil, errors.Newf("no message of ours names an operation of %s", reference)
	}
	return follows, nil
}
