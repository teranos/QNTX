package services

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// An agent the node hosts, on GitHub (ADR-048): it holds no token and stands
// in no namespace's shoes. What it asks is spent as the App's installation
// where the repository is, and GitHubService is asked by operation name.

type githubAsInstallation struct{}

// githubStatus is where a caller asks to be told the status GitHub answered
// with: a *int in the context, left as it is when GitHub did not answer.
type githubStatus struct{}

// AsInstallation is ctx for a call spent as the App's installation where the
// repository the call names is, in place of a namespace's token.
func AsInstallation(ctx context.Context) context.Context {
	return context.WithValue(ctx, githubAsInstallation{}, true)
}

// githubInstallationSkew is how long a token has to have left to be spent
// again: one that runs out mid-call is refused by GitHub.
const githubInstallationSkew = 5 * time.Minute

// InstallationToken is what the App minted for one repository.
type InstallationToken struct {
	Token string
	// ExpiresAt is when GitHub stops taking it, as GitHub said it.
	ExpiresAt string
	// App is the App whose installation it is of, by its slug.
	App string
	// Contents is what it may do with the repository's contents.
	Contents string

	until time.Time
}

// NoInstallation is the App not being installed where a repository is, in
// GitHub's words. GitHub says the same of a repository that does not exist.
type NoInstallation struct {
	Owner, Repo, Said string
}

func (e NoInstallation) Error() string {
	return "no installation of the App was found for " + e.Owner + "/" + e.Repo + ": " + e.Said
}

// InstallationToken is a token of the App's installation where owner/repo is,
// narrowed to that repository. One minted before and still good is given again.
func (s *GitHubServer) InstallationToken(ctx context.Context, owner, repo string) (InstallationToken, error) {
	key := owner + "/" + repo
	s.mu.Lock()
	held, minted := s.installations[key]
	s.mu.Unlock()
	if minted && time.Until(held.until) > githubInstallationSkew {
		return held, nil
	}

	// Only GitHub answering 404 is the App not being installed there. A node
	// that cannot sign as the App, and a GitHub that did not answer, are not.
	answeredWith := 0
	under, err := s.GetARepositoryInstallationForTheAuthenticatedApp(context.WithValue(ctx, githubStatus{}, &answeredWith),
		&protocol.GitHubGetARepositoryInstallationForTheAuthenticatedAppRequest{Owner: owner, Repo: repo})
	if err != nil {
		return InstallationToken{}, errors.Wrapf(err, "GitHub was not asked which installation %s is under", key)
	}
	if !under.GetSuccess() && answeredWith == http.StatusNotFound {
		return InstallationToken{}, NoInstallation{Owner: owner, Repo: repo, Said: under.GetError()}
	}
	if !under.GetSuccess() {
		return InstallationToken{}, errors.Newf("which installation %s is under was not learned: %s", key, under.GetError())
	}
	answered, err := s.CreateAnInstallationAccessTokenForAnApp(ctx,
		&protocol.GitHubCreateAnInstallationAccessTokenForAnAppRequest{InstallationId: under.GetId(), Repositories: []string{repo}})
	if err != nil {
		return InstallationToken{}, errors.Wrapf(err, "GitHub was not asked for a token for %s", key)
	}
	if !answered.GetSuccess() {
		return InstallationToken{}, errors.Newf("GitHub minted no token for %s: %s", key, answered.GetError())
	}
	until, err := time.Parse(time.RFC3339, answered.GetExpiresAt())
	if err != nil {
		return InstallationToken{}, errors.Wrapf(err, "GitHub minted a token for %s and when it expires (%q) did not read", key, answered.GetExpiresAt())
	}
	token := InstallationToken{
		Token: answered.GetToken(), ExpiresAt: answered.GetExpiresAt(),
		App: under.GetAppSlug(), Contents: answered.GetPermissions().GetContents(), until: until,
	}
	s.mu.Lock()
	s.installations[key] = token
	s.mu.Unlock()
	return token, nil
}

type githubInstallationID struct{}

// AsInstallationOf is ctx for a call spent as one installation of the App,
// named by its id, over every repository it holds: what that installation
// reaches, asked without naming a repository.
func AsInstallationOf(ctx context.Context, installationID int64) context.Context {
	return context.WithValue(ctx, githubInstallationID{}, installationID)
}

// installationTokenOf is a token of the installation over all it holds. One
// minted before and still good is given again.
func (s *GitHubServer) installationTokenOf(ctx context.Context, installationID int64) (string, string, error) {
	key := "installation#" + strconv.FormatInt(installationID, 10)
	s.mu.Lock()
	held, minted := s.installations[key]
	s.mu.Unlock()
	if minted && time.Until(held.until) > githubInstallationSkew {
		return held.Token, key, nil
	}
	answered, err := s.CreateAnInstallationAccessTokenForAnApp(ctx,
		&protocol.GitHubCreateAnInstallationAccessTokenForAnAppRequest{InstallationId: installationID})
	if err != nil {
		return "", key, errors.Wrapf(err, "GitHub was not asked for a token of installation %d", installationID)
	}
	if !answered.GetSuccess() {
		return "", key, errors.Newf("GitHub minted no token of installation %d: %s", installationID, answered.GetError())
	}
	until, err := time.Parse(time.RFC3339, answered.GetExpiresAt())
	if err != nil {
		return "", key, errors.Wrapf(err, "GitHub minted a token of installation %d and when it expires (%q) did not read", installationID, answered.GetExpiresAt())
	}
	s.mu.Lock()
	s.installations[key] = InstallationToken{Token: answered.GetToken(), ExpiresAt: answered.GetExpiresAt(), Contents: answered.GetPermissions().GetContents(), until: until}
	s.mu.Unlock()
	return answered.GetToken(), key, nil
}

// asInstallation is what a call marked AsInstallation spends: the token of the
// installation where the repository it names is.
func (s *GitHubServer) asInstallation(ctx context.Context, msg protoreflect.Message) (token, key string, err error) {
	// An owner or repo left unset is refused where the installation is asked
	// for, as every path parameter is.
	named, ofARepository := msg.Interface().(interface {
		GetOwner() string
		GetRepo() string
	})
	if !ofARepository {
		return "", "", errors.Newf("%s is asked as the App's installation, which is found by a repository, and it names no repository",
			msg.Descriptor().Name())
	}
	owner, repo := named.GetOwner(), named.GetRepo()
	minted, err := s.InstallationToken(ctx, owner, repo)
	if err != nil {
		return "", "installation:" + owner, err
	}
	return minted.Token, "installation:" + owner, nil
}

// NoSuchOperation is GitHubService asked for an operation it does not have.
type NoSuchOperation struct{ Operation string }

func (e NoSuchOperation) Error() string {
	return "GitHubService has no operation " + e.Operation
}

// NotWhatItTakes is a request that is not what the operation takes, and why.
type NotWhatItTakes struct{ Operation, Why string }

func (e NotWhatItTakes) Error() string {
	return "what was sent is not what " + e.Operation + " takes: " + e.Why
}

// githubService is GitHubService as its .proto describes it.
func githubService() protoreflect.ServiceDescriptor {
	return protocol.File_plugin_grpc_protocol_github_proto.Services().ByName("GitHubService")
}

// Ask is one operation of GitHubService by its name: what it takes as JSON,
// and what it answered as JSON, success and error among it. An operation it
// does not have, and a field the operation does not take, are an error.
func (s *GitHubServer) Ask(ctx context.Context, operation string, request []byte) ([]byte, error) {
	method := githubService().Methods().ByName(protoreflect.Name(operation))
	route, routed := githubRoutes[operation]
	if method == nil || !routed {
		return nil, NoSuchOperation{Operation: operation}
	}
	if route.mints {
		return nil, NotWhatItTakes{Operation: operation, Why: "what it answers is a credential, which is the node's own to ask for and is handed to nobody by name"}
	}
	takes, err := protoregistry.GlobalTypes.FindMessageByName(method.Input().FullName())
	if err != nil {
		return nil, errors.Wrapf(err, "what %s takes is not a message this build holds", operation)
	}
	gives, err := protoregistry.GlobalTypes.FindMessageByName(method.Output().FullName())
	if err != nil {
		return nil, errors.Wrapf(err, "what %s gives is not a message this build holds", operation)
	}
	req := takes.New().Interface()
	if len(request) > 0 {
		if err := protojson.Unmarshal(request, req); err != nil {
			return nil, NotWhatItTakes{Operation: operation, Why: err.Error()}
		}
	}
	resp := gives.New().Interface()
	s.call(ctx, operation, req, resp)
	answered, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resp)
	return answered, errors.Wrapf(err, "what %s answered did not marshal", operation)
}

// GitHubOperations is everything GitHubService can be asked, by name.
// namespace is left out: it is GitHubService's own, and not GitHub's.
func GitHubOperations() []*protocol.GitHubOperation {
	methods := githubService().Methods()
	operations := []*protocol.GitHubOperation{}
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		route, routed := githubRoutes[string(method.Name())]
		if !routed || route.mints {
			continue
		}
		takes := []string{}
		fields := method.Input().Fields()
		for j := 0; j < fields.Len(); j++ {
			if name := string(fields.Get(j).Name()); name != "namespace" {
				takes = append(takes, name)
			}
		}
		operations = append(operations, &protocol.GitHubOperation{Operation: string(method.Name()), Method: route.method, Path: route.path, Takes: takes})
	}
	slices.SortFunc(operations, func(a, b *protocol.GitHubOperation) int {
		if a.GetOperation() < b.GetOperation() {
			return -1
		}
		if a.GetOperation() > b.GetOperation() {
			return 1
		}
		return 0
	})
	return operations
}
