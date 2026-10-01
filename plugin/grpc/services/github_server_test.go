package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

// gardenTokens hands each namespace its own token, keyed by the credential
// it names. A namespace it does not hold is refused.
type gardenTokens map[string][2]string

func (g gardenTokens) Token(_ context.Context, namespace string) (string, string, error) {
	held, found := g[namespace]
	if !found {
		return "", "", errors.Newf("namespace %q has no GitHub token", namespace)
	}
	return held[0], held[1], nil
}

var gardenCreds = gardenTokens{
	"garden":  {"ghp_garden", "garden-pat"},
	"orchard": {"ghp_orchard", "orchard-pat"},
}

// seenRequest is what the fake GitHub was asked.
type seenRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
	Header http.Header
}

// fakeGitHub answers every request with answer and keeps what it was asked.
func fakeGitHub(t *testing.T, answer http.HandlerFunc) (*GitHubServer, *[]seenRequest) {
	t.Helper()
	var mu sync.Mutex
	seen := &[]seenRequest{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*seen = append(*seen, seenRequest{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Body: string(body), Header: r.Header.Clone()})
		mu.Unlock()
		answer(w, r)
	}))
	t.Cleanup(ts.Close)
	s := NewGitHubServer(gardenCreds, func() bool { return true }, zap.NewNop().Sugar())
	s.SetBaseURL(ts.URL)
	return s, seen
}

func answerJSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestGitHubGetWithPathAndQuery(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(200, `[]`))

	resp, err := s.ListCommits(context.Background(), &protocol.GitHubListCommitsRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", Sha: "main", Path: "docs/adr", PerPage: 5,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	require.Len(t, *seen, 1)
	got := (*seen)[0]
	assert.Equal(t, http.MethodGet, got.Method)
	assert.Equal(t, "/repos/teranos/QNTX/commits", got.Path)
	// Unset fields (author, committer, since, until, page) are not sent.
	assert.Equal(t, "path=docs%2Fadr&per_page=5&sha=main", got.Query)
	assert.Equal(t, "Bearer ghp_garden", got.Header.Get("Authorization"))
	assert.Equal(t, "application/vnd.github+json", got.Header.Get("Accept"))
	assert.Equal(t, "2026-03-10", got.Header.Get("X-GitHub-Api-Version"))
	assert.Empty(t, got.Body)
}

func TestGitHubPostWithJSONBody(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(201, `{
		"id": 1, "number": 42, "state": "open", "title": "Ground",
		"user": {"login": "sbvh", "id": 7},
		"head": {"ref": "ground", "sha": "abc"},
		"_links": {"html": {"href": "https://github.com/teranos/QNTX/pull/42"}},
		"not_in_proto": {"deep": [1, 2]}
	}`))

	resp, err := s.CreateAPullRequest(context.Background(), &protocol.GitHubCreateAPullRequestRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", Title: "Ground", Head: "ground", Base: "main", Draft: true,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	got := (*seen)[0]
	assert.Equal(t, http.MethodPost, got.Method)
	assert.Equal(t, "/repos/teranos/QNTX/pulls", got.Path)
	assert.Equal(t, "application/json", got.Header.Get("Content-Type"))
	// Zero-valued fields (head_repo, body, maintainer_can_modify, issue) are not sent;
	// path fields are not repeated in the body.
	assert.JSONEq(t, `{"title":"Ground","head":"ground","base":"main","draft":true}`, got.Body)

	assert.Equal(t, int64(42), resp.Number)
	assert.Equal(t, "sbvh", resp.GetUser().GetLogin())
	assert.Equal(t, "ground", resp.GetHead().GetRef())
	assert.Equal(t, "https://github.com/teranos/QNTX/pull/42", resp.GetXLinks().GetHtml().GetHref())
}

func TestGitHubBodyNestedAndNull(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(200, `{"id": 9, "number": 3}`))

	resp, err := s.UpdateAnIssue(context.Background(), &protocol.GitHubUpdateAnIssueRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", IssueNumber: 3,
		Milestone: structpb.NewNullValue(),
		Labels:    []*structpb.Value{structpb.NewStringValue("bug")},
		IssueFieldValues: []*protocol.GitHubUpdateAnIssueRequest_IssueFieldValues{
			{FieldId: 12, Value: structpb.NewNumberValue(3)},
		},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	assert.Equal(t, http.MethodPatch, (*seen)[0].Method)
	// A Value set to null is sent as null: that is how a milestone is cleared.
	// Integers inside nested messages stay JSON numbers.
	assert.JSONEq(t, `{"milestone":null,"labels":["bug"],"issue_field_values":[{"field_id":12,"value":3}]}`, (*seen)[0].Body)
}

func TestGitHubReactionsDecode(t *testing.T) {
	s, _ := fakeGitHub(t, answerJSON(200, `{"id": 5, "body": "yes", "updated_at": null, "reactions": {"total_count": 4, "+1": 3, "-1": 1}}`))

	resp, err := s.GetAnIssueComment(context.Background(), &protocol.GitHubGetAnIssueCommentRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", CommentId: 5,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	assert.Equal(t, int64(3), resp.GetReactions().GetPlus_1())
	assert.Equal(t, int64(1), resp.GetReactions().GetMinus_1())
}

func TestGitHubArrayResponse(t *testing.T) {
	s, _ := fakeGitHub(t, answerJSON(200, `[{"number": 1, "title": "one"}, {"number": 2, "title": "two"}]`))

	resp, err := s.ListPullRequests(context.Background(), &protocol.GitHubListPullRequestsRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", State: "open",
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	require.Len(t, resp.Items, 2)
	assert.Equal(t, int64(2), resp.Items[1].Number)
	assert.Equal(t, "two", resp.Items[1].Title)
}

func TestGitHubNoContent(t *testing.T) {
	s, seen := fakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	resp, err := s.CheckIfAPullRequestHasBeenMerged(context.Background(), &protocol.GitHubCheckIfAPullRequestHasBeenMergedRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", PullNumber: 42,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, resp.Error)
	assert.Equal(t, "/repos/teranos/QNTX/pulls/42/merge", (*seen)[0].Path)
}

func TestGitHubLocationResponse(t *testing.T) {
	s, _ := fakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://pipelines.actions.githubusercontent.com/artifact.zip?sig=x")
		w.WriteHeader(http.StatusFound)
	})

	resp, err := s.DownloadAnArtifact(context.Background(), &protocol.GitHubDownloadAnArtifactRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", ArtifactId: 77, ArchiveFormat: "zip",
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	// The redirect is answered, not followed: the caller gets where the file is.
	assert.Equal(t, "https://pipelines.actions.githubusercontent.com/artifact.zip?sig=x", resp.Location)
}

func TestGitHubTarballIsReadAsTheNamespace(t *testing.T) {
	s, seen := fakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		_, _ = w.Write([]byte("gzip bytes"))
	})

	body, err := s.Tarball(context.Background(), "garden", "abcd-nl", "clean", "c0ffee")
	require.NoError(t, err)
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.Equal(t, "gzip bytes", string(got))

	require.Len(t, *seen, 1)
	assert.Equal(t, "/repos/abcd-nl/clean/tarball/c0ffee", (*seen)[0].Path)
	// The credential is what lets a private repository answer at all.
	assert.Equal(t, "Bearer ghp_garden", (*seen)[0].Header.Get("Authorization"))
}

func TestGitHubTarballRefusalNamesTheCall(t *testing.T) {
	s, _ := fakeGitHub(t, answerJSON(404, `{"message":"Not Found"}`))

	_, err := s.Tarball(context.Background(), "garden", "abcd-nl", "clean", "c0ffee")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GET /repos/abcd-nl/clean/tarball/c0ffee")
	assert.Contains(t, err.Error(), "404")
	assert.Contains(t, err.Error(), "Not Found")
}

func TestGitHubRefusalNamesTheCall(t *testing.T) {
	s, _ := fakeGitHub(t, answerJSON(404, `{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`))

	resp, err := s.GetAPullRequest(context.Background(), &protocol.GitHubGetAPullRequestRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", PullNumber: 42,
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "GET /repos/teranos/QNTX/pulls/42")
	assert.Contains(t, resp.Error, "404")
	assert.Contains(t, resp.Error, "Not Found")
}

func TestGitHubDisabledNode(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(200, `{}`))
	s.enabled = func() bool { return false }

	resp, err := s.GetAPullRequest(context.Background(), &protocol.GitHubGetAPullRequestRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", PullNumber: 42,
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Equal(t, "GitHub is disabled on this node", resp.Error)
	assert.Empty(t, *seen, "a disabled node asks GitHub nothing")
}

func TestGitHubCredentialRefused(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(200, `{}`))

	resp, err := s.GetAPullRequest(context.Background(), &protocol.GitHubGetAPullRequestRequest{
		Namespace: "wilderness", Owner: "teranos", Repo: "QNTX", PullNumber: 42,
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, `"wilderness"`)
	assert.Empty(t, *seen)
}

func TestGitHubRateLimitHeadersPerCredential(t *testing.T) {
	s, _ := fakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4990")
		w.Header().Set("X-RateLimit-Used", "10")
		w.Header().Set("X-RateLimit-Reset", "1790000000")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.WriteHeader(http.StatusNoContent)
	})

	_, found := s.Limits("garden-pat")
	assert.False(t, found)

	_, err := s.CheckIfAPullRequestHasBeenMerged(context.Background(), &protocol.GitHubCheckIfAPullRequestHasBeenMergedRequest{
		Namespace: "garden", Owner: "teranos", Repo: "QNTX", PullNumber: 1,
	})
	require.NoError(t, err)

	lim, found := s.Limits("garden-pat")
	require.True(t, found)
	assert.Equal(t, int64(5000), lim.Limit)
	assert.Equal(t, int64(4990), lim.Remaining)
	assert.Equal(t, int64(10), lim.Used)
	assert.Equal(t, time.Unix(1790000000, 0), lim.Reset)
	assert.Equal(t, "core", lim.Resource)
	assert.False(t, lim.SeenAt.IsZero())

	_, found = s.Limits("orchard-pat")
	assert.False(t, found, "one credential's spending is not another's")
}

func TestGitHubRateLimitCall(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(200, `{
		"resources": {
			"core": {"limit": 5000, "remaining": 4321, "reset": 1790000000, "used": 679},
			"search": {"limit": 30, "remaining": 30, "reset": 1790000060, "used": 0}
		},
		"rate": {"limit": 5000, "remaining": 4321, "reset": 1790000000, "used": 679}
	}`))

	resp, err := s.RateLimit(context.Background(), &protocol.GitHubRateLimitRequest{Namespace: "orchard"})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	assert.Equal(t, "/rate_limit", (*seen)[0].Path)
	assert.Equal(t, "Bearer ghp_orchard", (*seen)[0].Header.Get("Authorization"))
	require.Contains(t, resp.Resources, "search")
	assert.Equal(t, int64(30), resp.Resources["search"].Limit)

	lim, found := s.Limits("orchard-pat")
	require.True(t, found)
	assert.Equal(t, int64(4321), lim.Remaining)
	assert.Equal(t, "core", lim.Resource)
}

// Every RPC the service declares is routed, every request field goes
// somewhere, and calling the RPC reaches GitHub at its route.
func TestGitHubEveryRPCRouted(t *testing.T) {
	desc := protocol.GitHubService_ServiceDesc
	require.Len(t, githubRoutes, len(desc.Methods), "one route per RPC, and none extra")

	for _, m := range desc.Methods {
		t.Run(m.MethodName, func(t *testing.T) {
			route, found := githubRoutes[m.MethodName]
			require.True(t, found, "no route for %s", m.MethodName)

			s, seen := fakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			var sent protoreflect.Message
			dec := func(v any) error {
				msg := v.(proto.Message).ProtoReflect()
				fields := msg.Descriptor().Fields()
				msg.Set(fields.ByName("namespace"), protoreflect.ValueOfString("garden"))
				for _, name := range route.pathFields() {
					fd := fields.ByName(protoreflect.Name(name))
					require.NotNil(t, fd, "path field %s is not in %s", name, msg.Descriptor().FullName())
					if fd.Kind() == protoreflect.StringKind {
						msg.Set(fd, protoreflect.ValueOfString("p"))
					} else {
						msg.Set(fd, protoreflect.ValueOfInt64(1))
					}
				}
				sent = msg
				return nil
			}
			out, err := m.Handler(s, context.Background(), dec, nil)
			require.NoError(t, err)

			// Every request field is routed; none is silently dropped.
			routed := map[string]bool{"namespace": true}
			for _, name := range append(append(route.pathFields(), route.query...), route.body...) {
				require.NotNil(t, sent.Descriptor().Fields().ByName(protoreflect.Name(name)), "route names %s, not in %s", name, sent.Descriptor().FullName())
				routed[name] = true
			}
			fields := sent.Descriptor().Fields()
			for i := 0; i < fields.Len(); i++ {
				name := string(fields.Get(i).Name())
				if m.MethodName == "RateLimit" && (name == "auth_token" || name == "source") {
					continue
				}
				assert.True(t, routed[name], "%s.%s is not routed", sent.Descriptor().FullName(), name)
			}

			answered := out.(proto.Message).ProtoReflect()
			success := answered.Get(answered.Descriptor().Fields().ByName("success")).Bool()
			assert.True(t, success, answered.Get(answered.Descriptor().Fields().ByName("error")).String())
			require.Len(t, *seen, 1)
			assert.Equal(t, route.method, (*seen)[0].Method)
			assert.Equal(t, filledPath(route.path, sent), (*seen)[0].Path)
			if len(route.body) > 0 {
				assert.True(t, json.Valid([]byte((*seen)[0].Body)), "body is JSON: %s", (*seen)[0].Body)
			}
		})
	}
}

// filledPath is template with each parameter written out from msg, by hand,
// so the executor's own path building is not what checks it.
func filledPath(template string, msg protoreflect.Message) string {
	var b strings.Builder
	for {
		open := strings.Index(template, "{")
		if open < 0 {
			b.WriteString(template)
			return b.String()
		}
		shut := strings.Index(template[open:], "}") + open
		b.WriteString(template[:open])
		fd := msg.Descriptor().Fields().ByName(protoreflect.Name(template[open+1 : shut]))
		if fd.Kind() == protoreflect.StringKind {
			b.WriteString(msg.Get(fd).String())
		} else {
			b.WriteString(strconv.FormatInt(msg.Get(fd).Int(), 10))
		}
		template = template[shut+1:]
	}
}
