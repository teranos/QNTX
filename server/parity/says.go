package parity

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/bufbuild/protocompile"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// "seeing prose from the specs themselves where they have it"
//
// What a spec says of its own models and columns is read with the rest of it:
// a .proto's leading comments, a JSON Schema's descriptions, a schema.prisma's
// /// lines. What our side says of its fields is in the .proto files under
// plugin/grpc/protocol, whose comments the generated Go does not keep, so
// make says writes them to protocol.says.json, which the node embeds.

// saysOf is the comment leading a descriptor in its .proto, as prose.
func saysOf(d protoreflect.Descriptor) string {
	return words(d.ParentFile().SourceLocations().ByDescriptor(d).LeadingComments)
}

// words is a comment as prose: what AIP marks as not for readers, between (--
// and --), left out; its lines joined, and a blank line kept as a paragraph.
func words(comment string) string {
	for {
		start := strings.Index(comment, "(--")
		if start < 0 {
			break
		}
		end := strings.Index(comment[start:], "--)")
		if end < 0 {
			break
		}
		comment = comment[:start] + comment[start+end+len("--)"):]
	}
	var paragraphs, lines []string
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(lines) > 0 {
				paragraphs = append(paragraphs, strings.Join(lines, " "))
				lines = nil
			}
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		paragraphs = append(paragraphs, strings.Join(lines, " "))
	}
	return strings.Join(paragraphs, "\n\n")
}

// protocolDir is where the protocol's .proto files are imported from.
const protocolDir = "plugin/grpc/protocol/"

// ProtocolSays is what every message and field of the .proto files in dir says
// of itself, by full name: protocol.Sigil.does. One that says nothing is left
// out. dir holds plugin/grpc/protocol's files as they are in the repository.
func ProtocolSays(dir fs.FS) (map[string]string, error) {
	entries, err := fs.ReadDir(dir, ".")
	if err != nil {
		return nil, errors.Wrap(err, "the protocol's .proto files did not read")
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".proto") {
			names = append(names, protocolDir+entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, errors.New("no .proto file in the protocol's directory")
	}
	compiler := protocompile.Compiler{
		SourceInfoMode: protocompile.SourceInfoStandard,
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			Accessor: func(name string) (io.ReadCloser, error) {
				file, ok := strings.CutPrefix(name, protocolDir)
				if !ok {
					return nil, fs.ErrNotExist
				}
				return dir.Open(file)
			},
		}),
	}
	files, err := compiler.Compile(context.Background(), names...)
	if err != nil {
		return nil, errors.Wrapf(err, "the protocol's .proto files did not compile")
	}
	says := map[string]string{}
	var walk func(messages protoreflect.MessageDescriptors)
	walk = func(messages protoreflect.MessageDescriptors) {
		for i := 0; i < messages.Len(); i++ {
			message := messages.Get(i)
			if said := saysOf(message); said != "" {
				says[string(message.FullName())] = said
			}
			fields := message.Fields()
			for j := 0; j < fields.Len(); j++ {
				if said := saysOf(fields.Get(j)); said != "" {
					says[string(fields.Get(j).FullName())] = said
				}
			}
			walk(message.Messages())
		}
	}
	for _, file := range files {
		walk(file.Messages())
	}
	return says, nil
}

// WriteProtocolSays is ProtocolSays as protocol.says.json holds it.
func WriteProtocolSays(says map[string]string) ([]byte, error) {
	body, err := json.MarshalIndent(says, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "what the protocol says did not marshal")
	}
	return append(body, '\n'), nil
}

//go:embed protocol.says.json
var protocolSaysJSON []byte

var (
	protocolSays     map[string]string
	protocolSaysOnce sync.Once
	protocolSaysErr  error
)

// protocolSaid is what every message and field of ours says of itself, by
// full name, as make says last wrote it.
func protocolSaid() (map[string]string, error) {
	protocolSaysOnce.Do(func() {
		protocolSaysErr = json.Unmarshal(protocolSaysJSON, &protocolSays)
	})
	if protocolSaysErr != nil {
		return nil, errors.Wrap(protocolSaysErr, "protocol.says.json did not read")
	}
	return protocolSays, nil
}

// OursSay is what one of our messages or fields says of itself, by full name,
// in its .proto's words.
func OursSay(name string) (string, error) {
	return oursSay(name)
}

// oursSay is what one of our messages or fields says of itself, by full name.
func oursSay(name string) (string, error) {
	says, err := protocolSaid()
	if err != nil {
		return "", err
	}
	return says[name], nil
}

// ours is what each message in scope and each of its fields says of itself.
func ours(messages map[string]protoreflect.MessageDescriptor) (map[string]string, error) {
	said := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(messages)) {
		message := messages[name]
		names := []string{name}
		fields := message.Fields()
		for i := 0; i < fields.Len(); i++ {
			names = append(names, string(fields.Get(i).FullName()))
		}
		for _, full := range names {
			words, err := oursSay(full)
			if err != nil {
				return nil, err
			}
			said[full] = words
		}
	}
	return said, nil
}

// ProtocolSaysFile is protocol.says.json's place in the repository, where
// make says writes it.
const ProtocolSaysFile = "server/parity/protocol.says.json"
