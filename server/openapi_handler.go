package server

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/openapi"
	"github.com/teranos/errors"
)

// openapiDocument is the generated document with this build's version in it.
//
// The written file carries no version: the tag is the only source of one, and
// it does not exist when the file is committed. This is the first moment there
// is a build to name, so this is where it is named. Done once — the answer
// cannot change while the process runs.
var openapiDocument = sync.OnceValues(func() ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal(openapi.Document(), &document); err != nil {
		return nil, errors.Wrap(err, "the generated OpenAPI document did not parse")
	}
	info, ok := document["info"].(map[string]any)
	if !ok {
		return nil, errors.New("the generated OpenAPI document has no info to name a version in")
	}
	info["version"] = version.VersionTag
	return json.Marshal(document)
})

// HandleOpenAPI answers with the OpenAPI document for this build: every path
// server/reach's table names, who reaches it, and which Go function answers.
//
// The version is this build's tag, stamped in at first ask — the written file
// carries none, because the tag does not exist when the file is committed. So
// the document served and the document in the tree differ by that one field,
// and only that one.
func (s *QNTXServer) HandleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET is the whole of it")
		return
	}

	document, err := openapiDocument()
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
