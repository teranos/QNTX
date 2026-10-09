package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// openapi is the document of what the node serves (ADR-039).

func (s *QNTXServer) openapiSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "openapi",
			Sigils: []*protocol.Sigil{
				{
					Name: "document",
					Does: "The OpenAPI document for this build: every path the reach table names, who reaches it, and what each sigil does there.",
					Gives: []*protocol.Field{
						{Name: "openapi", Says: "The OpenAPI version the document is written in."},
						{Name: "info", Says: "This build's title and version tag."},
						{Name: "paths", Says: "Every path, who reaches it, and each sigil's operation on it."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/openapi.json"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"document": s.openapiDocument},
	}
}

func (s *QNTXServer) openapiDocument(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	document, err := s.openapiServed()
	if err != nil {
		s.logger.Errorw("the OpenAPI document is not servable", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return json.RawMessage(document), nil
}
