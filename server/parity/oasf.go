package parity

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// OASFSkills is every skill of OASF's taxonomy as pinned, by its path there,
// with the id a Record names it by.
func OASFSkills() (map[string]uint32, *protocol.Refusal) {
	return oasfClasses("schema/skills", "base_skill")
}

// OASFModules is every module of OASF's taxonomy as pinned, by its path
// there, with the id a Record names it by: integration/a2a is 203.
func OASFModules() (map[string]uint32, *protocol.Refusal) {
	return oasfClasses("schema/modules", "base_module")
}

// oasfClasses is every class of one of OASF's taxonomies, by its path there,
// with its id: its parent's id * 100 + its own uid, up to a class that extends
// the base (oasf server/lib/schema/types.ex, class_uid).
func oasfClasses(at, base string) (map[string]uint32, *protocol.Refusal) {
	dir, files, refused := pinnedAt("oasf")
	if refused != nil {
		return nil, refused
	}
	type class struct {
		UID     uint32 `json:"uid"`
		Name    string `json:"name"`
		Extends string `json:"extends"`
	}
	classes := map[string]class{}
	for _, file := range files {
		if !strings.HasPrefix(file, at+"/") || path.Ext(file) != ".json" {
			continue
		}
		read := path.Join(dir, file)
		raw, err := fs.ReadFile(pinned, read)
		if err != nil {
			return nil, failed("%s did not read: %v", read, err)
		}
		var c class
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, failed("%s is not a class: %v", read, err)
		}
		if c.Name == base {
			continue
		}
		if _, twice := classes[c.Name]; twice {
			return nil, failed("%s names %s, which another class in %s names", read, c.Name, path.Join(dir, at))
		}
		classes[c.Name] = c
	}
	ids := make(map[string]uint32, len(classes))
	for name := range classes {
		var segments []string
		var id, scale uint32 = 0, 1
		for up := name; up != base; {
			c, found := classes[up]
			if !found {
				return nil, failed("%s extends %s, which %s holds no class for", name, up, path.Join(dir, at))
			}
			if len(segments) == len(classes) {
				return nil, failed("%s extends itself", name)
			}
			segments = append([]string{c.Name}, segments...)
			id += c.UID * scale
			scale *= 100
			up = c.Extends
		}
		ids[strings.Join(segments, "/")] = id
	}
	return ids, nil
}

// OASFSchemaVersion is the schema version a Record of the pinned OASF names.
func OASFSchemaVersion() (string, *protocol.Refusal) {
	dir, _, refused := pinnedAt("oasf")
	if refused != nil {
		return "", refused
	}
	read := path.Join(dir, "schema/version.json")
	raw, err := fs.ReadFile(pinned, read)
	if err != nil {
		return "", failed("%s did not read: %v", read, err)
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &version); err != nil || version.Version == "" {
		return "", failed("%s says no version: %v", read, err)
	}
	return version.Version, nil
}

// OASFRequired is every attribute OASF requires of an object, by its name, as
// the pinned schema/objects/<object>.json says it.
func OASFRequired(object string) ([]string, *protocol.Refusal) {
	dir, _, refused := pinnedAt("oasf")
	if refused != nil {
		return nil, refused
	}
	read := path.Join(dir, "schema/objects", object+".json")
	raw, err := fs.ReadFile(pinned, read)
	if err != nil {
		return nil, failed("%s did not read: %v", read, err)
	}
	// attributes is an object, and its order is the schema's own.
	var declared struct {
		Attributes json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(raw, &declared); err != nil {
		return nil, failed("%s is not an object: %v", read, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(declared.Attributes)))
	if _, err := decoder.Token(); err != nil {
		return nil, failed("%s has no attributes: %v", read, err)
	}
	var required []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, failed("%s did not read: %v", read, err)
		}
		var attribute struct {
			Requirement string `json:"requirement"`
		}
		if err := decoder.Decode(&attribute); err != nil {
			return nil, failed("%s's %v did not read: %v", read, token, err)
		}
		if attribute.Requirement == "required" {
			required = append(required, token.(string))
		}
	}
	return required, nil
}
