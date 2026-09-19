package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// "a plugin hands the node a Signum to say what it can do" (sigil.proto). It
// arrives with Initialize, holding no answers: what answers for it is the
// plugin, asked through HandleHTTP on the path each sigil is bound to.

// signaHolder is a plugin that handed the node signa, and the one way it is
// asked. grpc.ExternalDomainProxy is one.
type signaHolder interface {
	GetSigna() []*protocol.Signum
	AnswerHTTP(ctx context.Context, req *protocol.HTTPRequest) (*protocol.HTTPResponse, error)
}

// Who is asking, as the node admitted them, on every request a sigil hands a
// plugin. The node sets them and nothing a caller sends does (proxyHTTPRequest).
const (
	// HeaderAsker is the identity that admitted the caller: an account URL or a
	// did:key. A token carries the identity that minted it.
	HeaderAsker = "X-Qntx-Asker"
	// HeaderAskerDID is the token's own did:key, when a token made the request.
	HeaderAskerDID = "X-Qntx-Asker-Did"
)

// pluginSigna is the signa every ready plugin handed the node. A plugin's
// signum is named after the plugin and binds only paths under /api/{plugin}/,
// because a reach line names a signum by its name and a sigil by its path: a
// plugin naming another's would be reached by whoever reaches that one. A
// signum that does not is said in the log and served nowhere.
func (s *QNTXServer) pluginSigna() []sigil.Signum {
	if s.pluginRegistry == nil {
		return nil
	}
	var signa []sigil.Signum
	for _, name := range s.pluginRegistry.ListEnabled() {
		if !s.pluginRegistry.IsReady(name) {
			continue
		}
		p, ok := s.pluginRegistry.Get(name)
		if !ok {
			continue
		}
		holder, holds := p.(signaHolder)
		if !holds {
			continue
		}
		for _, handed := range holder.GetSigna() {
			if err := boundUnder(name, handed); err != nil {
				if s.logger != nil {
					s.logger.Errorw("a plugin's signum is not served", "plugin", name, "signum", handed.GetName(), "error", err)
				}
				continue
			}
			answers := map[string]sigil.Answer{}
			for _, held := range handed.GetSigils() {
				answers[held.GetName()] = s.pluginAnswer(name, held)
			}
			signa = append(signa, sigil.Signum{Signum: handed, Answers: answers})
		}
	}
	return signa
}

// boundUnder refuses a signum that is not the plugin's own: named otherwise, or
// bound to a path outside /api/{plugin}/, or to a path naming a segment, which
// no surface fills yet.
func boundUnder(plugin string, handed *protocol.Signum) error {
	if handed.GetName() != plugin {
		return errors.Newf("the plugin %s handed a signum named %s; a plugin's signum is named after the plugin", plugin, handed.GetName())
	}
	under := "/api/" + plugin + "/"
	for _, held := range handed.GetSigils() {
		path := held.GetHttp().GetPath()
		if !strings.HasPrefix(path, under) {
			return errors.Newf("the sigil %s of %s is bound to %s, which is not under %s", held.GetName(), plugin, path, under)
		}
		if path == under {
			return errors.Newf("the sigil %s of %s is bound to %s itself and names no path under it", held.GetName(), plugin, path)
		}
		if strings.ContainsAny(path, "{}") {
			return errors.Newf("the sigil %s of %s is bound to %s, and a plugin's path names no segment", held.GetName(), plugin, path)
		}
	}
	return nil
}

// pluginAnswer is a plugin's sigil answered by the plugin. What was sent goes
// to HandleHTTP on the sigil's path, below /api/{plugin} as every plugin path
// arrives: in the query for a GET or a DELETE, as a JSON object otherwise, each
// value as text. The plugin is looked up on every asking, because a restart is
// a new process and a new connection.
func (s *QNTXServer) pluginAnswer(plugin string, held *protocol.Sigil) sigil.Answer {
	return func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
		failed := func(err error) (any, *protocol.Refusal) {
			if s.logger != nil {
				s.logger.Errorw("a plugin's sigil failed", "plugin", plugin, "sigil", held.GetName(), "error", err)
			}
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: held.GetName() + " of " + plugin + " failed"}
		}

		p, ok := s.pluginRegistry.Get(plugin)
		if !ok || !s.pluginRegistry.IsReady(plugin) {
			return failed(errors.Newf("%s is not loaded", plugin))
		}
		holder, holds := p.(signaHolder)
		if !holds {
			return failed(errors.Newf("%s cannot be asked a sigil", plugin))
		}

		req, err := forwarded(plugin, held, sent, ctx)
		if err != nil {
			return failed(err)
		}
		resp, err := holder.AnswerHTTP(ctx, req)
		if err != nil {
			return failed(errors.Wrapf(err, "%s %s did not answer", req.GetMethod(), req.GetPath()))
		}

		if resp.GetStatusCode() >= 200 && resp.GetStatusCode() < 300 {
			if err := sigil.Holds(held, resp.GetBody()); err != nil {
				return failed(err)
			}
			return json.RawMessage(resp.GetBody()), nil
		}
		refusal, err := refusedBy(resp)
		if err != nil {
			return failed(errors.Wrapf(err, "%s %s answered %d", req.GetMethod(), req.GetPath(), resp.GetStatusCode()))
		}
		return nil, refusal
	}
}

// forwarded is the request a plugin is handed for one asking.
func forwarded(plugin string, held *protocol.Sigil, sent sigil.Sent, ctx context.Context) (*protocol.HTTPRequest, error) {
	method := held.GetHttp().GetMethod()
	req := &protocol.HTTPRequest{
		Method: method,
		Path:   strings.TrimPrefix(held.GetHttp().GetPath(), "/api/"+plugin),
	}
	if carriesBody(method) {
		body, err := json.Marshal(sent)
		if err != nil {
			return nil, errors.Wrapf(err, "what was sent to %s did not marshal", held.GetName())
		}
		req.Body = body
		req.Headers = append(req.Headers, &protocol.HTTPHeader{Name: "Content-Type", Values: []string{"application/json"}})
	} else if len(sent) > 0 {
		query := url.Values{}
		for name, value := range sent {
			query.Set(name, value)
		}
		req.Path += "?" + query.Encode()
	}

	// A node with no login admits nobody by name, and says nobody.
	if admitted, gated := auth.AdmissionFrom(ctx); gated {
		if admitted.Identity != "" {
			req.Headers = append(req.Headers, &protocol.HTTPHeader{Name: HeaderAsker, Values: []string{admitted.Identity}})
		}
		if admitted.Grant != nil && admitted.Grant.DID != "" {
			req.Headers = append(req.Headers, &protocol.HTTPHeader{Name: HeaderAskerDID, Values: []string{admitted.Grant.DID}})
		}
	}
	return req, nil
}

// refusedBy is a plugin saying no, in the sigil's terms. A plugin refuses with
// a status and a JSON protocol.Refusal; one that sends no why is read by its
// status, and one whose body is not a refusal is the plugin failing.
func refusedBy(resp *protocol.HTTPResponse) (*protocol.Refusal, error) {
	var said struct {
		Why   string `json:"why"`
		Param string `json:"param"`
		Says  string `json:"says"`
	}
	if err := json.Unmarshal(resp.GetBody(), &said); err != nil {
		return nil, errors.Wrapf(err, "the answer is not a refusal: %s", resp.GetBody())
	}
	if said.Says == "" {
		return nil, errors.Newf("the refusal says nothing: %s", resp.GetBody())
	}
	why := said.Why
	switch why {
	case "", sigil.Missing, sigil.NotOneOf, sigil.Invalid, sigil.NotFound, sigil.NotAllowed, sigil.Failed:
	default:
		return nil, errors.Newf("%q is not a kind of no", why)
	}
	if why == "" {
		switch resp.GetStatusCode() {
		case http.StatusBadRequest:
			why = sigil.Invalid
		case http.StatusNotFound:
			why = sigil.NotFound
		case http.StatusForbidden:
			why = sigil.NotAllowed
		default:
			why = sigil.Failed
		}
	}
	return &protocol.Refusal{Why: why, Param: said.Param, Says: said.Says}, nil
}

// ServePluginSigils serves again with the signa every ready plugin holds now.
// A plugin's sigils arrive with its Initialize, after the node has opened, and
// a restart may hand different ones.
func (s *QNTXServer) ServePluginSigils() {
	s.opening.Lock()
	defer s.opening.Unlock()
	s.answerSigils()
	if s.served == nil {
		return
	}
	if _, err := s.reopenHeld(); err != nil && s.logger != nil {
		s.logger.Errorw("Plugin sigils are not served; what the node serves is unchanged", "error", err)
	}
}
