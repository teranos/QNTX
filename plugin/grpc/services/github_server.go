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

	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
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
	// rate-limit state is kept per credential. An error refuses the call.
	Token(ctx context.Context, namespace string) (token, key string, err error)
}

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

	mu      sync.Mutex
	baseURL string
	limits  map[string]GitHubLimit // By credential key.
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
				if req.Context().Value(githubNoFollow{}) != nil {
					return http.ErrUseLastResponse
				}
				if len(via) >= 10 {
					return errors.Newf("stopped after %d redirects at %s", len(via), req.URL)
				}
				return nil
			},
		},
		baseURL: "https://api.github.com",
		limits:  map[string]GitHubLimit{},
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
	if token == "" {
		return nil, errors.Newf("namespace %q has an empty GitHub token", namespace)
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/tarball/" + url.PathEscape(ref)
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
		raw, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		sqlclose.Log(httpResp.Body.Close(), s.logger, "the GitHub response body")
		return nil, errors.Newf("GitHub GET %s answered %d: %s", path, httpResp.StatusCode, githubMessage(raw))
	}
	// The archive is the caller's to read and close.
	return httpResp.Body, nil
}

// githubRoute is where one RPC goes on GitHub. Path parameters are the {names}
// in path; query and body name the request fields sent there. Field names are
// GitHub's parameter names: the protos were written from the same pages.
type githubRoute struct {
	method string
	path   string
	query  []string
	body   []string
	// slashed are path parameters that are a path in a repository: each
	// segment is escaped and the slashes between them stay slashes.
	slashed []string
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
	namespace := msg.Get(msg.Descriptor().Fields().ByName("namespace")).String()
	token, key, err := s.creds.Token(ctx, namespace)
	if err != nil {
		githubFail(resp, fmt.Sprintf("no GitHub credential for namespace %q: %v", namespace, err))
		return key, false
	}
	if token == "" {
		githubFail(resp, fmt.Sprintf("namespace %q has an empty GitHub token", namespace))
		return key, false
	}

	path, err := githubPath(route, msg)
	if err != nil {
		githubFail(resp, fmt.Sprintf("GitHubService %s: %v", rpc, err))
		return key, false
	}
	body, err := githubBody(route, msg)
	if err != nil {
		githubFail(resp, fmt.Sprintf("GitHubService %s: %v", rpc, err))
		return key, false
	}

	s.mu.Lock()
	target := s.baseURL + path
	s.mu.Unlock()
	if query := githubQuery(route, msg); query != "" {
		target += "?" + query
	}

	wantsLocation := resp.ProtoReflect().Descriptor().Fields().ByName("location") != nil
	if wantsLocation {
		ctx = context.WithValue(ctx, githubNoFollow{}, true)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, route.method, target, reader)
	if err != nil {
		githubFail(resp, fmt.Sprintf("failed to build GitHub request %s %s: %v", route.method, path, err))
		return key, false
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		s.logger.Warnw("GitHub request failed", "namespace", namespace, "method", route.method, "path", path, "error", err)
		githubFail(resp, fmt.Sprintf("GitHub %s %s failed: %v", route.method, path, err))
		return key, false
	}
	defer func() { sqlclose.Log(httpResp.Body.Close(), s.logger, "the GitHub response body") }()
	s.recordHeaders(key, httpResp.Header)

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		githubFail(resp, fmt.Sprintf("failed to read GitHub %s %s answer (%d): %v", route.method, path, httpResp.StatusCode, err))
		return key, false
	}

	if wantsLocation && httpResp.StatusCode == http.StatusFound {
		location := httpResp.Header.Get("Location")
		if location == "" {
			githubFail(resp, fmt.Sprintf("GitHub %s %s answered %d without a Location", route.method, path, httpResp.StatusCode))
			return key, false
		}
		answered := resp.ProtoReflect()
		answered.Set(answered.Descriptor().Fields().ByName("location"), protoreflect.ValueOfString(location))
		githubSucceed(resp)
		return key, true
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		s.logger.Debugw("GitHub refused", "namespace", namespace, "method", route.method, "path", path, "status", httpResp.StatusCode)
		githubFail(resp, fmt.Sprintf("GitHub %s %s answered %d: %s", route.method, path, httpResp.StatusCode, githubMessage(raw)))
		return key, false
	}

	if err := githubDecode(raw, resp); err != nil {
		githubFail(resp, fmt.Sprintf("failed to decode GitHub %s %s answer (%d): %v", route.method, path, httpResp.StatusCode, err))
		return key, false
	}
	githubSucceed(resp)
	return key, true
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
		fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return "", errors.Newf("path %s names %s, which %s does not have", route.path, name, msg.Descriptor().FullName())
		}
		if !msg.Has(fd) {
			return "", errors.Newf("%s is required for %s %s", name, route.method, route.path)
		}
		value := githubScalar(msg.Get(fd), fd)
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
func githubQuery(route githubRoute, msg protoreflect.Message) string {
	values := url.Values{}
	for _, name := range route.query {
		fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
		if fd == nil || !msg.Has(fd) {
			continue
		}
		values.Set(name, githubScalar(msg.Get(fd), fd))
	}
	return values.Encode()
}

// githubScalar is a path or query value as GitHub reads it.
func githubScalar(v protoreflect.Value, fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return strconv.FormatBool(v.Bool())
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		return strconv.FormatInt(v.Int(), 10)
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return strconv.FormatUint(v.Uint(), 10)
	default:
		return v.String()
	}
}

// githubBody is the JSON body of the route's body fields that are set; nil
// for a route that sends no body.
func githubBody(route githubRoute, msg protoreflect.Message) ([]byte, error) {
	if len(route.body) == 0 {
		return nil, nil
	}
	body := map[string]any{}
	for _, name := range route.body {
		fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return nil, errors.Newf("body field %s is not in %s", name, msg.Descriptor().FullName())
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
		return nil, errors.Wrapf(err, "failed to encode the body of %s %s", route.method, route.path)
	}
	return encoded, nil
}

// githubJSON is a field's value as GitHub's JSON. protojson writes int64 as a
// string where GitHub wants a number, so it is used only for google.protobuf
// Struct/Value/ListValue, which are JSON already.
func githubJSON(v protoreflect.Value, fd protoreflect.FieldDescriptor) (any, error) {
	switch {
	case fd.IsList():
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
	case fd.IsMap():
		out := map[string]any{}
		var failed error
		v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
			item, err := githubJSONOne(mv, fd.MapValue())
			if err != nil {
				failed = err
				return false
			}
			out[k.String()] = item
			return true
		})
		return out, failed
	default:
		return githubJSONOne(v, fd)
	}
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
		if ev := fd.Enum().Values().ByNumber(v.Enum()); ev != nil {
			return string(ev.Name()), nil
		}
		return int32(v.Enum()), nil
	default:
		return v.Interface(), nil
	}
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

// githubDecode fills resp from a 2xx body. No body (204) is an answer with
// nothing in it; an array is a list, carried in the response's items.
func githubDecode(raw []byte, resp proto.Message) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		if resp.ProtoReflect().Descriptor().Fields().ByName("items") == nil {
			return errors.Newf("GitHub answered a list, and %s has no items", resp.ProtoReflect().Descriptor().FullName())
		}
		wrapped := make([]byte, 0, len(trimmed)+10)
		wrapped = append(wrapped, `{"items":`...)
		wrapped = append(wrapped, trimmed...)
		wrapped = append(wrapped, '}')
		trimmed = wrapped
	}
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(trimmed, resp)
}

// githubMessage is what GitHub said in a refusal: its message, or the body.
func githubMessage(raw []byte) string {
	var said struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &said); err == nil && said.Message != "" {
		return said.Message
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "no message"
	}
	if len(text) > 500 {
		text = text[:500]
	}
	return text
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
