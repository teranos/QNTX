package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// GitHubService through sigils (ADR-048, Its git): what a plugin asks over
// gRPC, asked by operation name, and spent as the App's installation where the
// repository is. Whoever asks holds no GitHub token.

func githubAskSigils() []*protocol.Sigil {
	return []*protocol.Sigil{
		{
			Name: "ask",
			Does: "Asks GitHub one thing through GitHubService, as the App's installation where the repository is: a pull request opened, a branch read, a workflow run listed. github operations lists what can be asked and what each takes.",
			Takes: []*protocol.Param{
				{Name: "operation", Required: true, Says: "The operation, by its name: CreateAPullRequest."},
				{Name: "request", Says: "What the operation takes, as a JSON object with GitHub's own names: {\"owner\": \"...\", \"repo\": \"...\"}."},
			},
			Gives: []*protocol.Field{
				{Name: "answer", Says: "What GitHub answered, as the operation gives it."},
			},
			Http: &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/ask"},
		},
		{
			Name:   "operations",
			Does:   "Everything github ask can be asked: each operation's name, where it goes on GitHub and the fields it takes.",
			Answer: "protocol.GitHubOperations",
			Http:   &protocol.Endpoint{Method: http.MethodGet, Path: githubPath + "/operations"},
		},
	}
}

func (s *QNTXServer) githubAsk(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	operation := sent["operation"]
	raw, err := s.gitHubService().Ask(services.AsInstallation(ctx), operation, []byte(sent["request"]))
	var noSuch services.NoSuchOperation
	var notTaken services.NotWhatItTakes
	switch {
	case err == nil:
	case errors.As(err, &noSuch):
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "operation", Says: err.Error() + "; github operations lists what it has"}
	case errors.As(err, &notTaken):
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "request", Says: err.Error()}
	default:
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}

	// success and error are GitHubService's own: a failure is the refusal, and
	// what is given is GitHub's answer alone.
	var said struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	var answered map[string]any
	for _, into := range []any{&said, &answered} {
		if err := json.Unmarshal(raw, into); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what " + operation + " answered did not read: " + err.Error()}
		}
	}
	if !said.Success {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: operation + ": " + said.Error}
	}
	delete(answered, "success")
	delete(answered, "error")
	return map[string]any{"answer": answered}, nil
}

func (s *QNTXServer) githubOperations(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	return &protocol.GitHubOperations{Operations: services.GitHubOperations()}, nil
}
