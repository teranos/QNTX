package parity

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// skillsAt is where OASF keeps its skill taxonomy, in the pin as upstream.
const skillsAt = "schema/skills"

// OASFSkills is every skill of OASF's taxonomy as pinned, by its path there,
// with the id a Record names it by: its parent's id * 100 + its own uid, up to
// a skill that extends base_skill (oasf server/lib/schema/types.ex, class_uid).
func OASFSkills() (map[string]uint32, *protocol.Refusal) {
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
		if !strings.HasPrefix(file, skillsAt+"/") || path.Ext(file) != ".json" {
			continue
		}
		at := path.Join(dir, file)
		raw, err := fs.ReadFile(pinned, at)
		if err != nil {
			return nil, failed("%s did not read: %v", at, err)
		}
		var c class
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, failed("%s is not a skill: %v", at, err)
		}
		if c.Name == "base_skill" {
			continue
		}
		if _, twice := classes[c.Name]; twice {
			return nil, failed("%s names %s, which another skill in %s names", at, c.Name, dir)
		}
		classes[c.Name] = c
	}
	skills := make(map[string]uint32, len(classes))
	for name := range classes {
		var segments []string
		var id, scale uint32 = 0, 1
		for at := name; at != "base_skill"; {
			c, found := classes[at]
			if !found {
				return nil, failed("%s extends %s, which %s holds no skill for", name, at, dir)
			}
			if len(segments) == len(classes) {
				return nil, failed("%s extends itself", name)
			}
			segments = append([]string{c.Name}, segments...)
			id += c.UID * scale
			scale *= 100
			at = c.Extends
		}
		skills[strings.Join(segments, "/")] = id
	}
	return skills, nil
}
