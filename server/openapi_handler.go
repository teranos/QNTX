package server

import (
	"encoding/json"
	"net/http"

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

// HandleOpenAPI answers with the OpenAPI document for this build: every path
// server/reach's table names, who reaches it, and what each sigil does there.

// The document served differs from the one in the tree by the version, which
// is this build's tag, and by the sigils' operations.
func (s *QNTXServer) HandleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET is the whole of it")
		return
	}

	document, err := s.openapiServed()
	if err != nil {
		writeWrappedError(w, s.logger, err,
			"the OpenAPI document is not servable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(document); err != nil {
		s.logger.Errorw("could not write the OpenAPI document", "error", err)
	}
}
