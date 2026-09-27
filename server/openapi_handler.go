package server

import (
	"encoding/json"

	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/openapi"
	"github.com/teranos/errors"
)

// openapiServed is the document this node serves: the written one, with this
// build's version in it and the sigils' operations on the paths they answer.

// The written file carries no version: the tag is the only source of one, and
// it does not exist when the file is committed.

// The written file says no operation either. It is the reach table's paths,
// and what is done on a path is a sigil's to say (ADR-039).
func (s *QNTXServer) openapiServed() ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal(openapi.Document(), &document); err != nil {
		return nil, errors.Wrap(err, "the generated OpenAPI document did not parse")
	}
	info, ok := document["info"].(map[string]any)
	if !ok {
		return nil, errors.New("the generated OpenAPI document has no info to name a version in")
	}
	info["version"] = version.VersionTag
	sigilsInto(document, s.checkedSigna())
	return json.Marshal(document)
}
