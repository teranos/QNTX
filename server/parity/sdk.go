package parity

import (
	_ "embed"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"sync"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// "Replace in parity A2A spec with the SDK latest version"
//
// A reference held by the SDK that speaks it, not by a pinned spec: its
// messages are the ones the SDK compiles in, and what each says of itself is
// what the SDK's generated Go says, which make says reads out of the module.

// fromSDK is each reference the node reads from an SDK it links.
var fromSDK = map[string][]protoreflect.FileDescriptor{
	"a2a": {a2apb.File_a2av1_proto},
}

// A2ASaysFile is where make says writes what a2a-go's generated Go says of
// each message, field, service and method.
const A2ASaysFile = "server/parity/a2a.says.json"

//go:embed a2a.says.json
var a2aSaysJSON []byte

var (
	sdkSays     map[string]string
	sdkSaysOnce sync.Once
	sdkSaysErr  error
)

// sdkSaid is what the SDKs the node links say of their messages, by full name,
// as make says last wrote it.
func sdkSaid() map[string]string {
	sdkSaysOnce.Do(func() {
		sdkSaysErr = json.Unmarshal(a2aSaysJSON, &sdkSays)
	})
	if sdkSaysErr != nil {
		return map[string]string{}
	}
	return sdkSays
}

// sdkReference is a reference an SDK holds: its messages, as the SDK
// compiled them in.
func sdkReference(files []protoreflect.FileDescriptor) (Schema, *protocol.Refusal) {
	if sdkSaid(); sdkSaysErr != nil {
		return Schema{}, failed("a2a.says.json did not read: %v", sdkSaysErr)
	}
	var models []Model
	for _, file := range files {
		models = append(models, messagesOf(file.Package(), file.Messages())...)
	}
	return Schema{Models: models, fits: func(kind protoreflect.Kind, column Column) bool {
		return column.Type == kind.String()
	}}, nil
}

// A2AResolveSays is what a2a-go's card resolver's Resolve says it does, by
// the name a2a.says.json holds it under.
const A2AResolveSays = "a2aclient/agentcard.Resolver.Resolve"

// A2AGoSays is what a2a-go's generated Go says of each message, field,
// service and method of lf.a2a.v1, by full name: protoc-gen-go keeps a .proto's
// comments as Go's. pb is a2av1.pb.go, and grpc a2av1_grpc.pb.go, whose client
// interface carries the service's and each method's. resolver is
// a2aclient/agentcard/resolver.go, whose Resolve says what discovery does.
func A2AGoSays(pb, grpc, resolver []byte) (map[string]string, error) {
	file := a2apb.File_a2av1_proto
	messages := map[string]protoreflect.MessageDescriptor{}
	var named func(protoreflect.MessageDescriptors, string)
	named = func(all protoreflect.MessageDescriptors, parent string) {
		for i := 0; i < all.Len(); i++ {
			message := all.Get(i)
			goName := string(message.Name())
			if parent != "" {
				goName = parent + "_" + goName
			}
			messages[goName] = message
			named(message.Messages(), goName)
		}
	}
	named(file.Messages(), "")

	says := map[string]string{}
	keep := func(name protoreflect.FullName, comment *ast.CommentGroup) {
		if said := words(withoutToolLines(comment.Text())); said != "" {
			says[string(name)] = said
		}
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "a2av1.pb.go", pb, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "a2av1.pb.go did not parse")
	}
	for _, decl := range parsed.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typed := spec.(*ast.TypeSpec)
			fields, isStruct := typed.Type.(*ast.StructType)
			if !isStruct {
				continue
			}
			// A oneof's member is a struct of its own, Message_Member.
			message, held := messages[typed.Name.Name]
			if !held {
				parent, _, wraps := strings.Cut(typed.Name.Name, "_")
				if message, held = messages[parent]; !held || !wraps {
					continue
				}
			} else {
				keep(message.FullName(), gen.Doc)
			}
			for _, field := range fields.Fields.List {
				if field.Tag == nil {
					continue
				}
				name := protoName(field.Tag.Value)
				if name == "" {
					continue
				}
				if descriptor := message.Fields().ByName(protoreflect.Name(name)); descriptor != nil {
					keep(descriptor.FullName(), field.Doc)
				}
			}
		}
	}

	service := file.Services().ByName("A2AService")
	if service == nil {
		return nil, errors.New("a2a-go compiles in no A2AService")
	}
	parsed, err = parser.ParseFile(token.NewFileSet(), "a2av1_grpc.pb.go", grpc, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "a2av1_grpc.pb.go did not parse")
	}
	for _, decl := range parsed.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typed := spec.(*ast.TypeSpec)
			client, isInterface := typed.Type.(*ast.InterfaceType)
			if !isInterface || typed.Name.Name != "A2AServiceClient" {
				continue
			}
			// protoc-gen-go-grpc says what the interface is before the
			// service's own words, a paragraph each.
			paragraphs := strings.Split(strings.TrimSpace(gen.Doc.Text()), "\n\n")
			if said := words(withoutToolLines(paragraphs[len(paragraphs)-1])); said != "" {
				says[string(service.FullName())] = said
			}
			for _, method := range client.Methods.List {
				if len(method.Names) != 1 {
					continue
				}
				if descriptor := service.Methods().ByName(protoreflect.Name(method.Names[0].Name)); descriptor != nil {
					keep(descriptor.FullName(), method.Doc)
				}
			}
		}
	}
	parsed, err = parser.ParseFile(token.NewFileSet(), "resolver.go", resolver, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "resolver.go did not parse")
	}
	for _, decl := range parsed.Decls {
		if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Recv != nil && fn.Name.Name == "Resolve" {
			keep(A2AResolveSays, fn.Doc)
		}
	}
	if says[A2AResolveSays] == "" {
		return nil, errors.New("a2a-go's card resolver says nothing of Resolve")
	}
	return says, nil
}

// withoutToolLines is a comment without the lines the .proto keeps for its tools:
// protolint's directives, and the next field number a message hands out.
func withoutToolLines(comment string) string {
	var kept []string
	for _, line := range strings.Split(comment, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "protolint:") || strings.HasPrefix(trimmed, "Next ID:") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// protoName is the .proto name a generated field's struct tag gives it.
func protoName(literal string) string {
	tag := reflect.StructTag(strings.Trim(literal, "`"))
	for _, part := range strings.Split(tag.Get("protobuf"), ",") {
		if name, isName := strings.CutPrefix(part, "name="); isName {
			return name
		}
	}
	return ""
}

// WriteA2ASays is A2AGoSays as a2a.says.json holds it.
func WriteA2ASays(says map[string]string) ([]byte, error) {
	return WriteProtocolSays(says)
}
