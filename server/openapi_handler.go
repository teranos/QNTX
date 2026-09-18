package server

import (
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/openapi"
	"github.com/teranos/errors"
)

// openapiServed is the document this node serves: the generated one, with
// this build's version in it and the sigils laid over the paths they answer.
//
// The written file carries no version: the tag is the only source of one, and
// it does not exist when the file is committed. This is the first moment there
// is a build to name, so this is where it is named.
//
// The written file cannot say what a sigil does either. Its generator reads
// source, and a route offered from a sigil is not a literal there, so what a
// sigil says is laid in here from the sigil itself (ADR-039).
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
// server/reach's table names, who reaches it, and what answers there.
//
// The document served differs from the one in the tree in two ways, and only
// those: the version, which is this build's tag, and the paths a sigil
// answers, which say what the sigil says.
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
