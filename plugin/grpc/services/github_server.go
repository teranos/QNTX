package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// githubAPIVersion is the REST API version asked for; the route table's doc URLs are this version's.
const githubAPIVersion = "2026-03-10"

// "to disable it on the Node entirely"
const githubDisabled = "GitHub is disabled on this node"

// GitHubCredentials answers which GitHub token a namespace spends.
type GitHubCredentials interface {
	// Token is the token namespace spends, and key names that credential so
	// rate-limit state is kept per credential. An error refuses the call; a
	// token handed is spent as handed, and GitHub refuses one that is empty.
	Token(ctx context.Context, namespace string) (token, key string, err error)
}

// GitHubApp is a node that holds the App's private key (ADR-043). A route that
// is the App's own spends what it signs, never a namespace's token.
type GitHubApp interface {
	// AppToken is a JWT the App's private key signs. An error refuses the call.
	AppToken() (string, error)
}

// githubAppKey names the App's own credential, so its rate limit is kept apart
// from every namespace's.
const githubAppKey = "app"

// GitHubLimit is GitHub's rate-limit state for one credential, as GitHub last reported it.
type GitHubLimit struct {
	Limit     int64
	Remaining int64
	Used      int64
	Reset     time.Time
	Resource  string // X-RateLimit-Resource: which of GitHub's limits the numbers are for.
	SeenAt    time.Time
}

// GitHubServer is the GitHubService gRPC server (ADR-043).
// "because of the GitHubService the ratelimit is kept in one place"
// A plugin names a namespace and holds no token of its own.
type GitHubServer struct {
	protocol.UnimplementedGitHubServiceServer
	creds   GitHubCredentials
	enabled func() bool
	logger  *zap.SugaredLogger
	client  *http.Client

	mu            sync.Mutex
	baseURL       string
	limits        map[string]GitHubLimit       // By credential key.
	installations map[string]InstallationToken // By owner/repo: what the App minted and is still good.
}

// NewGitHubServer creates the GitHub service. enabled is asked on every call.
func NewGitHubServer(creds GitHubCredentials, enabled func() bool, logger *zap.SugaredLogger) *GitHubServer {
	return &GitHubServer{
		creds:   creds,
		enabled: enabled,
		logger:  logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
			// A route whose answer carries a location asks where GitHub points:
			// that redirect is the answer. Any other (a renamed repository) is followed.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if noFollow, marked := req.Context().Value(githubNoFollow{}).(bool); marked && noFollow {
					return http.ErrUseLastResponse
				}
				if len(via) >= 10 {
					return errors.Newf("stopped after %d redirects at %s", len(via), req.URL)
				}
				return nil
			},
		},
		baseURL:       "https://api.github.com",
		limits:        map[string]GitHubLimit{},
		installations: map[string]InstallationToken{},
	}
}

// githubNoFollow marks a request whose redirect is its answer.
type githubNoFollow struct{}

// SetBaseURL points the server at another API root (tests).
func (s *GitHubServer) SetBaseURL(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseURL = strings.TrimSuffix(u, "/")
}

// Limits is the last rate-limit state seen for a credential key. False when
// GitHub has not yet reported one for it.
func (s *GitHubServer) Limits(key string) (GitHubLimit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, found := s.limits[key]
	return l, found
}

// RateLimit asks GitHub for every limit of the namespace's credential; its
// core limit becomes what Limits reports for that credential.
func (s *GitHubServer) RateLimit(ctx context.Context, req *protocol.GitHubRateLimitRequest) (*protocol.GitHubRateLimitResponse, error) {
	resp := &protocol.GitHubRateLimitResponse{}
	key, ok := s.call(ctx, "RateLimit", req, resp)
	if core, found := resp.Resources["core"]; ok && found {
		s.recordLimit(key, GitHubLimit{
			Limit:     core.Limit,
			Remaining: core.Remaining,
			Used:      core.Used,
			Reset:     time.Unix(core.Reset_, 0),
			Resource:  "core",
			SeenAt:    time.Now(),
		})
	}
	return resp, nil
}

// Tarball is a repository at ref as GitHub archives it, read with the
// namespace's credential, so a private repository comes the way a public one
// does. GitHub answers at a signed codeload URL, which the client follows.
func (s *GitHubServer) Tarball(ctx context.Context, namespace, owner, repo, ref string) (io.ReadCloser, error) {
	if !s.enabled() {
		return nil, errors.New(githubDisabled)
	}
	token, key, err := s.creds.Token(ctx, namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "no GitHub credential for namespace %q", namespace)
	}
	return s.download(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/tarball/"+url.PathEscape(ref), token, key)
}

// Artifact is a workflow run's artifact as GitHub zips it, read as the App's
// installation where owner/repo is. GitHub answers at a signed URL, which the
// client follows.
func (s *GitHubServer) Artifact(ctx context.Context, owner, repo string, artifactID int64) (io.ReadCloser, error) {
	if !s.enabled() {
		return nil, errors.New(githubDisabled)
	}
	minted, err := s.InstallationToken(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/actions/artifacts/" + strconv.FormatInt(artifactID, 10) + "/zip"
	return s.download(ctx, path, minted.Token, "installation:"+owner)
}

// download is what GitHub answers at path, read with token and its rate limit
// kept under key, following where GitHub points.
func (s *GitHubServer) download(ctx context.Context, path, token, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	target := s.baseURL + path
	s.mu.Unlock()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build GitHub request GET %s", path)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, errors.Wrapf(err, "GitHub GET %s failed", path)
	}
	s.recordHeaders(key, httpResp.Header)
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		raw, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		sqlclose.Log(httpResp.Body.Close(), s.logger, "the GitHub response body")
		if readErr != nil {
			return nil, errors.Wrapf(readErr, "GitHub GET %s answered %d, and what it said did not read", path, httpResp.StatusCode)
		}
		return nil, errors.Newf("GitHub GET %s answered %d: %s", path, httpResp.StatusCode, githubMessage(raw))
	}
	// What GitHub answered is the caller's to read and close.
	return httpResp.Body, nil
}

// githubRoute is where one RPC goes on GitHub. Path parameters are the {names}
// in path; query and body name the request fields sent there. Field names are
// GitHub's parameter names: the protos were written from the same pages.
type githubRoute struct {
	method string
	path   string
	query  []string
	body   githubPayload
	// slashed are path parameters that are a path in a repository: each
	// segment is escaped and the slashes between them stay slashes.
	slashed []string
	// asApp is a route GitHub takes only from the App itself, with its JWT.
	asApp bool
	// mints is a route whose answer is a credential. It is the node's own to
	// ask: whoever asks GitHubService by name is not handed one.
	mints bool
}

// pathFields are the request fields named in the path template, in order.
func (r githubRoute) pathFields() []string {
	var names []string
	rest := r.path
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			return names
		}
		shut := strings.Index(rest[open:], "}")
		if shut < 0 {
			return names
		}
		names = append(names, rest[open+1:open+shut])
		rest = rest[open+shut+1:]
	}
}

// githubAnswer serves an RPC through its route. The failure is in resp, never the error.
func githubAnswer[Resp proto.Message](s *GitHubServer, ctx context.Context, rpc string, req proto.Message, resp Resp) (Resp, error) {
	s.call(ctx, rpc, req, resp)
	return resp, nil
}

// call sends req to GitHub by rpc's route and fills resp from the answer.
// Every failure travels in resp as success=false and error. It returns the
// credential key spent and whether GitHub answered with success.
func (s *GitHubServer) call(ctx context.Context, rpc string, req, resp proto.Message) (string, bool) {
	if !s.enabled() {
		githubFail(resp, githubDisabled)
		return "", false
	}
	route, found := githubRoutes[rpc]
	if !found {
		githubFail(resp, fmt.Sprintf("GitHubService %s has no route", rpc))
		return "", false
	}

	msg := req.ProtoReflect()
	token, key, namespace, err := s.credential(ctx, route, msg)
	if err != nil {
		githubFail(resp, err.Error())
		return key, false
	}

	path, err := githubPath(route, msg)
	if err != nil {
		githubFail(resp, fmt.Sprintf("GitHubService %s: %v", rpc, err))
		return key, false
	}
	query, err := githubQuery(route, msg)
	if err != nil {
		githubFail(resp, fmt.Sprintf("GitHubService %s: %v", rpc, err))
		return key, false
	}

	s.mu.Lock()
	target := s.baseURL + path
	s.mu.Unlock()

	// A response that carries a location is answered by where GitHub points.
	_, wantsLocation := resp.(interface{ GetLocation() string })
	if wantsLocation {
		ctx = context.WithValue(ctx, githubNoFollow{}, true)
	}

	httpReq, err := route.body.request(ctx, route.method, target, msg)
	if err != nil {
		githubFail(resp, fmt.Sprintf("GitHubService %s: failed to build GitHub request %s %s: %v", rpc, route.method, path, err))
		return key, false
	}
	// The query is what the route's set fields say; none set is none sent.
	httpReq.URL.RawQuery = query
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		githubFail(resp, fmt.Sprintf("GitHub %s %s failed: %v", route.method, path, err))
		s.logger.Warnw("GitHub request failed", "namespace", namespace, "method", route.method, "path", path, "error", err)
		return key, false
	}
	defer func() { sqlclose.Log(httpResp.Body.Close(), s.logger, "the GitHub response body") }()
	s.recordHeaders(key, httpResp.Header)
	if status, wanted := ctx.Value(githubStatus{}).(*int); wanted {
		*status = httpResp.StatusCode
	}

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		githubFail(resp, fmt.Sprintf("failed to read GitHub %s %s answer (%d): %v", route.method, path, httpResp.StatusCode, err))
		return key, false
	}

	if wantsLocation && httpResp.StatusCode == http.StatusFound {
		location, err := named.TextOf(fmt.Sprintf("the Location GitHub %s %s answered %d with", route.method, path, httpResp.StatusCode), httpResp.Header.Get("Location"))
		if err != nil {
			githubFail(resp, err.Error())
			return key, false
		}
		answered := resp.ProtoReflect()
		field, err := githubField(answered.Descriptor(), "location")
		if err != nil {
			githubFail(resp, err.Error())
			return key, false
		}
		answered.Set(field, protoreflect.ValueOfString(location.String()))
		githubSucceed(resp)
		return key, true
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		s.logger.Debugw("GitHub refused", "namespace", namespace, "method", route.method, "path", path, "status", httpResp.StatusCode)
		githubFail(resp, fmt.Sprintf("GitHub %s %s answered %d: %s", route.method, path, httpResp.StatusCode, githubMessage(raw)))
		return key, false
	}

	listed, err := githubDecode(raw, resp)
	if err != nil {
		githubFail(resp, fmt.Sprintf("failed to decode GitHub %s %s answer (%d): %v", route.method, path, httpResp.StatusCode, err))
		return key, false
	}
	// A contents answer says which GitHub answered with: a directory is
	// answered by its entries, a list, and anything else by itself.
	if _, contents := resp.(interface {
		GetAnswered() protocol.GitHubReposContentsAnswer
	}); contents {
		answered := resp.ProtoReflect()
		field, err := githubField(answered.Descriptor(), "answered")
		if err != nil {
			githubFail(resp, err.Error())
			return key, false
		}
		kind := protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_ONE
		if listed {
			kind = protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_DIRECTORY
		}
		answered.Set(field, protoreflect.ValueOfEnum(kind.Number()))
	}
	// A list GitHub pages by cursor says where the next page is in this header.
	if _, paged := resp.(interface{ GetLink() string }); paged {
		answered := resp.ProtoReflect()
		field, err := githubField(answered.Descriptor(), "link")
		if err != nil {
			githubFail(resp, err.Error())
			return key, false
		}
		answered.Set(field, protoreflect.ValueOfString(httpResp.Header.Get("Link")))
	}
	githubSucceed(resp)
	return key, true
}

// credential is what a call spends and the key its rate limit is kept under:
// the App's JWT for a route that is the App's own, else the token of the
// namespace the request names, which is returned for the logs.
func (s *GitHubServer) credential(ctx context.Context, route githubRoute, msg protoreflect.Message) (token, key, namespace string, err error) {
	if route.asApp {
		app, ok := s.creds.(GitHubApp)
		if !ok {
			return "", githubAppKey, "", errors.New("this node cannot act as the GitHub App: am.toml's [auth.provider.github] names no private_key")
		}
		token, err := app.AppToken()
		if err != nil {
			return "", githubAppKey, "", errors.Wrap(err, "the GitHub App's JWT was not signed")
		}
		return token, githubAppKey, "", nil
	}
	if id, of := ctx.Value(githubInstallationID{}).(int64); of {
		token, key, err = s.installationTokenOf(ctx, id)
		return token, key, "", err
	}
	if asInstallation, marked := ctx.Value(githubAsInstallation{}).(bool); marked && asInstallation {
		token, key, err = s.asInstallation(ctx, msg)
		return token, key, "", err
	}
	namespace = msg.Get(msg.Descriptor().Fields().ByName("namespace")).String()
	token, key, err = s.creds.Token(ctx, namespace)
	if err != nil {
		return "", key, namespace, errors.Newf("no GitHub credential for namespace %q: %v", namespace, err)
	}
	return token, key, namespace, nil
}

// githubPath fills the route's path template from the request. A path
// parameter left zero is refused rather than sent as an empty segment.
func githubPath(route githubRoute, msg protoreflect.Message) (string, error) {
	var b strings.Builder
	rest := route.path
	for _, name := range route.pathFields() {
		open := strings.Index(rest, "{")
		shut := strings.Index(rest, "}")
		b.WriteString(rest[:open])
		fd, err := githubField(msg.Descriptor(), name)
		if err != nil {
			return "", errors.Wrapf(err, "path %s", route.path)
		}
		if !msg.Has(fd) {
			return "", errors.Newf("%s is required for %s %s", name, route.method, route.path)
		}
		value, err := githubScalar(msg.Get(fd), fd)
		if err != nil {
			return "", err
		}
		if slices.Contains(route.slashed, name) {
			segments := strings.Split(value, "/")
			for i, segment := range segments {
				segments[i] = url.PathEscape(segment)
			}
			b.WriteString(strings.Join(segments, "/"))
		} else {
			b.WriteString(url.PathEscape(value))
		}
		rest = rest[shut+1:]
	}
	b.WriteString(rest)
	return b.String(), nil
}

// githubQuery encodes the route's query fields that are set.
func githubQuery(route githubRoute, msg protoreflect.Message) (string, error) {
	values := url.Values{}
	for _, name := range route.query {
		fd, err := githubField(msg.Descriptor(), name)
		if err != nil {
			return "", errors.Wrapf(err, "the query of %s %s", route.method, route.path)
		}
		if !msg.Has(fd) {
			continue
		}
		value, err := githubScalar(msg.Get(fd), fd)
		if err != nil {
			return "", err
		}
		values.Set(name, value)
	}
	return values.Encode(), nil
}

// githubScalar is a path or query value as GitHub reads it. A field of a kind
// GitHub takes no path or query value as is refused, not written as Go prints it.
func githubScalar(v protoreflect.Value, fd protoreflect.FieldDescriptor) (string, error) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return v.String(), nil
	case protoreflect.BoolKind:
		return strconv.FormatBool(v.Bool()), nil
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		return strconv.FormatInt(v.Int(), 10), nil
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return strconv.FormatUint(v.Uint(), 10), nil
	}
	return "", errors.Newf("%s is a %s, which is no path or query value", fd.FullName(), fd.Kind())
}

// githubPayload is what a route sends GitHub besides its path and query.
type githubPayload interface {
	// request is the request to target, carrying what the payload takes from msg.
	request(ctx context.Context, method, target string, msg protoreflect.Message) (*http.Request, error)
	// fields are the request fields the payload is written from.
	fields() []string
}

// noBody is a route that sends GitHub no body.
type noBody struct{}

func (noBody) request(ctx context.Context, method, target string, _ protoreflect.Message) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, target, http.NoBody)
}

func (noBody) fields() []string { return []string{} }

// jsonBody is a route whose body is the JSON of these request fields, those
// that are set.
type jsonBody []string

func (b jsonBody) request(ctx context.Context, method, target string, msg protoreflect.Message) (*http.Request, error) {
	body := map[string]any{}
	for _, name := range b {
		fd, err := githubField(msg.Descriptor(), name)
		if err != nil {
			return nil, errors.Wrap(err, "the body")
		}
		if !msg.Has(fd) {
			continue
		}
		v, err := githubJSON(msg.Get(fd), fd)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to encode body field %s", name)
		}
		body[githubJSONName(fd)] = v
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to encode the body of %s %s", method, target)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (b jsonBody) fields() []string { return b }

// githubField is the field of a message by its name, or that the message has
// none by that name.
func githubField(message protoreflect.MessageDescriptor, name string) (protoreflect.FieldDescriptor, error) {
	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		if string(fields.Get(i).Name()) == name {
			return fields.Get(i), nil
		}
	}
	return nil, errors.Newf("%s has no field %s", message.FullName(), name)
}

// githubJSON is a field's value as GitHub's JSON. protojson writes int64 as a
// string where GitHub wants a number, so it is used only for google.protobuf
// Struct/Value/ListValue, which are JSON already.
func githubJSON(v protoreflect.Value, fd protoreflect.FieldDescriptor) (any, error) {
	switch fd.Cardinality() {
	case protoreflect.Repeated:
		return githubJSONRepeated(v, fd)
	case protoreflect.Optional, protoreflect.Required:
		return githubJSONOne(v, fd)
	}
	return nil, errors.Newf("%s has cardinality %s, which is not one GitHub's JSON is written from", fd.FullName(), fd.Cardinality())
}

// githubJSONRepeated is a repeated field as GitHub's JSON: a map is an object,
// and a list an array.
func githubJSONRepeated(v protoreflect.Value, fd protoreflect.FieldDescriptor) (any, error) {
	if fd.IsList() {
		list := v.List()
		out := make([]any, 0, list.Len())
		for i := 0; i < list.Len(); i++ {
			item, err := githubJSONOne(list.Get(i), fd)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	// A repeated field that is no list is a map.
	out := map[string]any{}
	var rangeErr error
	v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
		item, err := githubJSONOne(mv, fd.MapValue())
		if err != nil {
			rangeErr = err
			return false
		}
		out[k.String()] = item
		return true
	})
	return out, rangeErr
}

func githubJSONOne(v protoreflect.Value, fd protoreflect.FieldDescriptor) (any, error) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		m := v.Message()
		if strings.HasPrefix(string(m.Descriptor().FullName()), "google.protobuf.") {
			raw, err := protojson.Marshal(m.Interface())
			if err != nil {
				return nil, errors.Wrapf(err, "failed to encode %s", m.Descriptor().FullName())
			}
			return json.RawMessage(raw), nil
		}
		out := map[string]any{}
		var failed error
		m.Range(func(inner protoreflect.FieldDescriptor, iv protoreflect.Value) bool {
			item, err := githubJSON(iv, inner)
			if err != nil {
				failed = errors.Wrapf(err, "field %s", inner.Name())
				return false
			}
			out[githubJSONName(inner)] = item
			return true
		})
		return out, failed
	case protoreflect.EnumKind:
		// A number the enum does not declare is refused, not sent as a number.
		values := fd.Enum().Values()
		for i := 0; i < values.Len(); i++ {
			if values.Get(i).Number() == v.Enum() {
				return string(values.Get(i).Name()), nil
			}
		}
		return nil, errors.Newf("%s is %d, which %s does not declare", fd.FullName(), v.Enum(), fd.Enum().FullName())
	case protoreflect.BoolKind, protoreflect.StringKind, protoreflect.BytesKind,
		protoreflect.FloatKind, protoreflect.DoubleKind,
		protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return v.Interface(), nil
	}
	return nil, errors.Newf("%s is a %s, which GitHub's JSON is not written from", fd.FullName(), fd.Kind())
}

// githubJSONName is the key GitHub knows a field by: its json_name where the
// proto sets one ("+1"), else its proto name, which is GitHub's snake_case.
// protoc fills json_name for every field, so a set one is one protoc did not derive.
func githubJSONName(fd protoreflect.FieldDescriptor) string {
	name := string(fd.Name())
	if fd.JSONName() != protocCamel(name) {
		return fd.JSONName()
	}
	return name
}

// protocCamel is the json_name protoc derives: each underscore dropped and the
// letter after it upper-cased.
func protocCamel(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		if r == '_' {
			upper = true
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upper = false
		b.WriteRune(r)
	}
	return b.String()
}

// githubDecode fills resp from a 2xx body, and says whether GitHub answered
// with a list. No body (204) is an answer with nothing in it: the JSON decoder
// finds no value before the end. An array is a list, carried in the
// response's items.
func githubDecode(raw []byte, resp proto.Message) (bool, error) {
	var answer json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&answer); errors.Is(err, io.EOF) {
		return false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	listed := bytes.HasPrefix(trimmed, []byte("["))
	if listed {
		items, err := githubField(resp.ProtoReflect().Descriptor(), "items")
		if err != nil {
			return listed, errors.Wrap(err, "GitHub answered a list")
		}
		wrapped := make([]byte, 0, len(trimmed)+len(items.Name())+4)
		wrapped = append(wrapped, `{"`+string(items.Name())+`":`...)
		wrapped = append(wrapped, trimmed...)
		wrapped = append(wrapped, '}')
		trimmed = wrapped
	}
	return listed, protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(trimmed, resp)
}

// githubMessage is what GitHub said in a refusal: its message, or, for a body
// that is not JSON or names no message, the body as it came, quoted, so a body
// that said nothing reads as "".
func githubMessage(raw []byte) string {
	var said map[string]any
	if err := json.Unmarshal(raw, &said); err != nil {
		return bodyAsItCame(raw)
	}
	message, given := said["message"].(string)
	if !given {
		return bodyAsItCame(raw)
	}
	return message
}

// bodyAsItCame is a body GitHub sent, quoted, its first 500 bytes.
func bodyAsItCame(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 500 {
		text = text[:500]
	}
	return strconv.Quote(text)
}

func githubFail(resp proto.Message, reason string) {
	m := resp.ProtoReflect()
	fields := m.Descriptor().Fields()
	m.Set(fields.ByName("success"), protoreflect.ValueOfBool(false))
	m.Set(fields.ByName("error"), protoreflect.ValueOfString(reason))
}

func githubSucceed(resp proto.Message) {
	m := resp.ProtoReflect()
	fields := m.Descriptor().Fields()
	m.Set(fields.ByName("success"), protoreflect.ValueOfBool(true))
	m.Clear(fields.ByName("error"))
}

// recordHeaders keeps the X-RateLimit-* headers of an answer under the
// credential that spent it. An answer without them changes nothing.
func (s *GitHubServer) recordHeaders(key string, h http.Header) {
	limit, err := strconv.ParseInt(h.Get("X-RateLimit-Limit"), 10, 64)
	if err != nil {
		return
	}
	// A header that does not read is said, never kept as a zero that reads as spent.
	read := map[string]int64{}
	for _, name := range []string{"X-RateLimit-Remaining", "X-RateLimit-Used", "X-RateLimit-Reset"} {
		n, err := strconv.ParseInt(h.Get(name), 10, 64)
		if err != nil {
			s.logger.Warnw("GitHub's rate-limit header does not read; the limit is not recorded",
				"credential", key, "header", name, "value", h.Get(name), "error", err)
			return
		}
		read[name] = n
	}
	remaining, used, reset := read["X-RateLimit-Remaining"], read["X-RateLimit-Used"], read["X-RateLimit-Reset"]
	s.recordLimit(key, GitHubLimit{
		Limit:     limit,
		Remaining: remaining,
		Used:      used,
		Reset:     time.Unix(reset, 0),
		Resource:  h.Get("X-RateLimit-Resource"),
		SeenAt:    time.Now(),
	})
}

func (s *GitHubServer) recordLimit(key string, l GitHubLimit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits[key] = l
}
