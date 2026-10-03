package parity

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// The references a signum can be held to, each pinned in a directory named for
// the reference, its version and the commit it was taken at, with SOURCE
// saying where it came from: umami is umami_v3.3.1_ca661c7, a2a is
// a2a_v1.0.1_3303592, mcp is mcp_2026-07-28_5f5440b, umami-tracker is
// umami-tracker_v3.3.1_ca661c7. A reference is read as it is: a schema.prisma
// as Prisma, a .proto as the descriptors it compiles to, a schema.json as JSON
// Schema, a .d.ts as the TypeScript types it declares.
//
//go:embed */schema.prisma */*.proto */schema.json */openapi.words.json */words */*.d.ts
var pinned embed.FS

// Reference is the schema of the reference named, from the one directory
// pinned for it.
func Reference(name string) (Schema, *protocol.Refusal) {
	entries, err := fs.ReadDir(pinned, ".")
	if err != nil {
		return Schema{}, failed("the pinned references did not read: %v", err)
	}
	var found, held []string
	for _, entry := range entries {
		reference, _, _ := strings.Cut(entry.Name(), "_")
		held = append(held, reference)
		if reference == name {
			found = append(found, entry.Name())
		}
	}
	switch len(found) {
	case 0:
		return Schema{}, &protocol.Refusal{Why: sigil.NotFound, Param: "reference",
			Says: "the node holds no schema for " + name + "; it holds " + strings.Join(held, ", ")}
	case 1:
	default:
		return Schema{}, failed("%s is pinned more than once: %s", name, strings.Join(found, ", "))
	}

	files, err := fs.ReadDir(pinned, found[0])
	if err != nil {
		return Schema{}, failed("%s did not read: %v", found[0], err)
	}
	var protos, declarations []string
	prisma, jsonSchema := false, false
	for _, file := range files {
		switch name := file.Name(); {
		case name == "schema.prisma":
			prisma = true
		case name == "schema.json":
			jsonSchema = true
		case strings.HasSuffix(name, ".d.ts"):
			declarations = append(declarations, name)
		case strings.HasSuffix(name, ".proto"):
			protos = append(protos, name)
		}
	}
	switch {
	case prisma:
		// A record and the types of what is sent to it are one reference:
		// Umami's schema.prisma and its tracker's index.d.ts.
		schema, refused := prismaAt(found[0], "schema.prisma")
		if refused != nil {
			return Schema{}, refused
		}
		return withDeclarations(schema, found[0], declarations)
	case jsonSchema:
		return jsonSchemaAt(path.Join(found[0], "schema.json"))
	case len(declarations) > 0:
		return withDeclarations(Schema{fits: tsFits}, found[0], declarations)
	}
	if len(protos) == 0 {
		return Schema{}, failed("%s holds no schema.prisma, no .proto, no schema.json and no .d.ts", found[0])
	}
	return protoAt(found[0], protos)
}

func prismaAt(dir, file string) (Schema, *protocol.Refusal) {
	schema := path.Join(dir, file)
	raw, err := pinned.ReadFile(schema)
	if err != nil {
		return Schema{}, failed("%s did not read: %v", schema, err)
	}
	models, err := ParsePrisma(schema, raw)
	if err != nil {
		return Schema{}, failed("%v", err)
	}
	if err := wordsFromAPI(dir, models); err != nil {
		return Schema{}, failed("%v", err)
	}
	return Prisma(models), nil
}

// withDeclarations is a schema with the models of the .d.ts files beside it,
// each column held to TypeScript's types and saying it was read there.
func withDeclarations(schema Schema, dir string, files []string) (Schema, *protocol.Refusal) {
	for _, file := range files {
		declarations := path.Join(dir, file)
		raw, err := pinned.ReadFile(declarations)
		if err != nil {
			return Schema{}, failed("%s did not read: %v", declarations, err)
		}
		models, err := ParseTypeScript(declarations, raw)
		if err != nil {
			return Schema{}, failed("%v", err)
		}
		for i := range models {
			m := &models[i]
			if m.Says != "" {
				m.SaysFrom = file + " · " + m.Name
			}
			for j := range m.Columns {
				c := &m.Columns[j]
				c.fits = tsFits
				if c.Says != "" {
					c.SaysFrom = file + " · " + m.Name + "." + c.Name
				}
			}
		}
		schema.Models = append(schema.Models, models...)
	}
	return schema, nil
}

func jsonSchemaAt(schema string) (Schema, *protocol.Refusal) {
	raw, err := pinned.ReadFile(schema)
	if err != nil {
		return Schema{}, failed("%s did not read: %v", schema, err)
	}
	read, err := ParseJSONSchema(schema, raw)
	if err != nil {
		return Schema{}, failed("%v", err)
	}
	return read, nil
}

// protoAt compiles the .proto files of dir. What they import from google/api is
// what this binary links of genproto; google/protobuf is protocompile's own.
func protoAt(dir string, names []string) (Schema, *protocol.Refusal) {
	compiler := protocompile.Compiler{
		// The comments are what the spec says of its own messages and fields.
		SourceInfoMode: protocompile.SourceInfoStandard,
		Resolver: protocompile.WithStandardImports(protocompile.CompositeResolver{
			&protocompile.SourceResolver{Accessor: func(name string) (io.ReadCloser, error) {
				return pinned.Open(path.Join(dir, name))
			}},
			protocompile.ResolverFunc(func(name string) (protocompile.SearchResult, error) {
				file, err := protoregistry.GlobalFiles.FindFileByPath(name)
				if err != nil {
					return protocompile.SearchResult{}, err
				}
				return protocompile.SearchResult{Desc: file}, nil
			}),
		}),
	}
	files, err := compiler.Compile(context.Background(), names...)
	if err != nil {
		return Schema{}, failed("%s did not compile: %v", dir, err)
	}
	var models []Model
	for _, file := range files {
		models = append(models, messagesOf(file.Package(), file.Messages())...)
	}
	return Schema{Models: models, fits: func(kind protoreflect.Kind, column Column) bool {
		return column.Type == kind.String()
	}}, nil
}

// messagesOf is every message, nested ones after their parent, each named
// without its package: AgentSkill, not lf.a2a.v1.AgentSkill.
func messagesOf(pkg protoreflect.FullName, messages protoreflect.MessageDescriptors) []Model {
	var models []Model
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)
		if message.IsMapEntry() {
			continue
		}
		model := Model{Name: strings.TrimPrefix(string(message.FullName()), string(pkg)+"."), Says: saysOf(message)}
		fields := message.Fields()
		for j := 0; j < fields.Len(); j++ {
			field := fields.Get(j)
			kind := field.Kind().String()
			if field.IsMap() {
				kind = "map"
			}
			model.Columns = append(model.Columns, Column{
				Name: string(field.Name()), Type: kind, List: field.IsList(), Required: required(field), Says: saysOf(field),
			})
		}
		models = append(models, model)
		models = append(models, messagesOf(pkg, message.Messages())...)
	}
	return models
}

// required reads google.api.field_behavior off a field. The compiled options
// carry the extension as bytes, so they are read again against the registry
// this binary links, where field_behavior is known.
func required(field protoreflect.FieldDescriptor) bool {
	raw, err := proto.Marshal(field.Options())
	if err != nil {
		return false
	}
	options := &descriptorpb.FieldOptions{}
	if err := proto.Unmarshal(raw, options); err != nil {
		return false
	}
	behaviors, ok := proto.GetExtension(options, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	if !ok {
		return false
	}
	for _, behavior := range behaviors {
		if behavior == annotations.FieldBehavior_REQUIRED {
			return true
		}
	}
	return false
}
