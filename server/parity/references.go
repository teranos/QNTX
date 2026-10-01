package parity

import (
	"embed"
	"io/fs"
	"path"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// The references a signum can be held to, each pinned in a directory named for
// the reference, its version and the commit it was taken at, with SOURCE
// saying where it came from: umami is umami_v3.3.1_ca661c7.
//
//go:embed */schema.prisma
var pinned embed.FS

// Reference is the models of the reference named, from the one directory
// pinned for it.
func Reference(name string) ([]Model, *protocol.Refusal) {
	entries, err := fs.ReadDir(pinned, ".")
	if err != nil {
		return nil, failed("the pinned references did not read: %v", err)
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
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "reference",
			Says: "the node holds no schema for " + name + "; it holds " + strings.Join(held, ", ")}
	case 1:
	default:
		return nil, failed("%s is pinned more than once: %s", name, strings.Join(found, ", "))
	}
	schema := path.Join(found[0], "schema.prisma")
	raw, err := pinned.ReadFile(schema)
	if err != nil {
		return nil, failed("%s did not read: %v", schema, err)
	}
	models, err := ParsePrisma(schema, raw)
	if err != nil {
		return nil, failed("%v", err)
	}
	return models, nil
}
