package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
)

// What carries a push from this node (ADR-048, Its git): a token the node
// mints as the GitHub App, for one repository, when git asks for it.

// gitCredentialUser is the username GitHub takes an installation token under
// over HTTPS.
const gitCredentialUser = "x-access-token"

func githubCredentialSigil() *protocol.Sigil {
	return &protocol.Sigil{
		Name: "credential",
		Does: "Mints what carries a push to one repository: a short-lived token of the App's installation there, narrowed to that repository. It is what git is handed when it asks, and nothing keeps it.",
		Takes: []*protocol.Param{
			{Name: "owner", Required: true, Says: "The account the repository is under."},
			{Name: "repo", Required: true, Says: "The repository."},
		},
		Gives: []*protocol.Field{
			{Name: "username", Says: "The username git presents with the token."},
			{Name: "password", Says: "The token, as git's password."},
			{Name: "expires_at", Says: "When GitHub stops taking it."},
			{Name: "app", Says: "The App whose installation it is of."},
			{Name: "contents", Says: "What the token may do with the repository's contents: write carries a push, read does not."},
		},
		Http: &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/credential"},
	}
}

func (s *QNTXServer) githubCredential(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	minted, err := s.gitHubService().InstallationToken(ctx, sent["owner"], sent["repo"])
	switch err := err.(type) {
	case nil:
	case services.NoInstallation:
		// GitHub answers 404 both where the App is not installed and where
		// there is no such repository: its words are given as they came.
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "repo", Says: err.Error()}
	default:
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]any{
		"username":   gitCredentialUser,
		"password":   minted.Token,
		"expires_at": minted.ExpiresAt,
		"app":        minted.App,
		"contents":   minted.Contents,
	}, nil
}
