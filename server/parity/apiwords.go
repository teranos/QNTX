package parity

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// A schema that says nothing of its own models may be spoken for by the
// reference's API document: what it says of its schemas pinned beside it as
// openapi.words.json (nix/references/umami.nix, from the document by its hash),
// with words saying which of its schemas speaks for which model, since nothing
// in the reference links them. A column the schema says nothing of takes the words
// of the property of the same name, and says where they were read.

// apiDocument is what an OpenAPI document says of its schemas.
type apiDocument struct {
	Components struct {
		Schemas map[string]struct {
			Description string `json:"description"`
			Properties  map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

// wordsFromAPI gives models the API document's words, where dir pins both an
// openapi.words.json and the words that link its schemas to the models. A dir with
// neither is left as it is; one with one and not the other, or a line naming
// what is not there, is the pin's to fix.
func wordsFromAPI(dir string, models []Model) error {
	links, linksErr := pinned.ReadFile(path.Join(dir, "words"))
	raw, docErr := pinned.ReadFile(path.Join(dir, "openapi.words.json"))
	switch {
	case errors.Is(linksErr, fs.ErrNotExist) && errors.Is(docErr, fs.ErrNotExist):
		return nil
	case linksErr != nil:
		return errors.Wrapf(linksErr, "%s pins openapi.words.json, and its words did not read", dir)
	case docErr != nil:
		return errors.Wrapf(docErr, "%s pins words, and its openapi.words.json did not read: run make says", dir)
	}
	return applyAPIWords(dir, links, raw, models)
}

// applyAPIWords is wordsFromAPI on what was read: the words file and the API
// document.
func applyAPIWords(dir string, links, raw []byte, models []Model) error {
	var doc apiDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return errors.Wrapf(err, "%s/openapi.words.json did not read", dir)
	}
	at := map[string]*Model{}
	for i := range models {
		at[models[i].Name] = &models[i]
	}
	for n, line := range strings.Split(string(links), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return errors.Newf("%s/words line %d is %q, not a model and the schema that speaks for it", dir, n+1, line)
		}
		model, ok := at[parts[0]]
		if !ok {
			return errors.Newf("%s/words line %d names model %s, which the schema does not have", dir, n+1, parts[0])
		}
		schema, ok := doc.Components.Schemas[parts[1]]
		if !ok {
			return errors.Newf("%s/words line %d names %s, which openapi.json does not have", dir, n+1, parts[1])
		}
		if model.Says == "" && schema.Description != "" {
			model.Says, model.SaysFrom = schema.Description, "openapi.json · "+parts[1]
		}
		for i := range model.Columns {
			column := &model.Columns[i]
			property, ok := schema.Properties[column.Name]
			if column.Says != "" || !ok || property.Description == "" {
				continue
			}
			column.Says, column.SaysFrom = property.Description, "openapi.json · "+parts[1]+"."+column.Name
		}
	}
	return nil
}
