package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Signing an agent in to Claude Code is Claude Code's own flow, carried by the
// node (ADR-048): the URL it prints is given to whoever signs in, and the code
// they are shown is handed back to it. The node holds no credential; what the
// sign-in leaves is in the agent's own config dir.
//
// "The one who set's it up does, they provide their own Subscription or API key"

// signInWindow is how long a sign-in begun waits for its code.
const signInWindow = 10 * time.Minute

// statusWindow is how long asking Claude Code whether it is signed in may take.
const statusWindow = 15 * time.Second

// signingIn is an agent's sign-in begun and not finished.
type signingIn struct {
	mu      sync.Mutex
	pending *claudecode.SigningIn
	cancel  context.CancelFunc
}

// take hands over the pending sign-in, leaving none.
func (s *signingIn) take() (*claudecode.SigningIn, context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, cancel := s.pending, s.cancel
	s.pending, s.cancel = nil, nil
	return pending, cancel
}

// loginSigil is the Claude harness's own sigil: sign the agent in.
func (s *QNTXServer) loginSigil(h *harness) harnessSigil {
	return harnessSigil{
		sigil: &protocol.Sigil{
			Name: "login",
			Does: "Signs the agent in to Claude Code, by Claude Code's own flow. Called without a code it begins a sign-in and gives the URL to sign in at; " +
				"called with the code shown there it finishes the sign-in. The node holds no credential: what the sign-in leaves is in the agent's own config dir.",
			Takes: []*protocol.Param{
				{Name: "code", Says: "The code shown after signing in at the URL. Not sent, a sign-in is begun and the URL given."},
				{Name: "billing", OneOf: claudecode.Billings, Says: "Whose account pays: the Claude subscription, or the Console. Not sent, the subscription."},
			},
			Gives: []*protocol.Field{
				{Name: "url", Says: "Where to sign in, while a sign-in is begun and not finished. Empty once finished."},
				{Name: "signed_in", Says: "Whether Claude Code says the agent is signed in."},
				{Name: "auth_method", Says: "How it is signed in, as Claude Code names it."},
				{Name: "api_provider", Says: "Whose API it is signed in to, as Claude Code names it."},
			},
			Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/" + h.name + "/login"},
		},
		answer: func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
			return s.claudeLogin(ctx, h, sent)
		},
	}
}

func (s *QNTXServer) claudeLogin(ctx context.Context, h *harness, sent sigil.Sent) (any, *protocol.Refusal) {
	agent, refused := s.harnessAgent(h)
	if refused != nil {
		return nil, refused
	}
	return s.loginAgent(ctx, h, agent, sent)
}

// loginAgent signs one agent in to Claude Code, or finishes the sign-in begun.
func (s *QNTXServer) loginAgent(ctx context.Context, h *harness, agent *rootAgent, sent sigil.Sent) (any, *protocol.Refusal) {
	binary, err := s.harnessHeldBy(h.name).Path(ctx)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no Claude Code to sign in to: " + err.Error()}
	}

	if code := sent["code"]; code != "" {
		pending, cancel := agent.signingIn.take()
		if pending == nil {
			return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "code",
				Says: "no sign-in is begun to finish: call login without a code, sign in at the URL it gives, and hand the code back here"}
		}
		err := pending.Finish(code)
		cancel()
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Param: "code", Says: err.Error()}
		}
		return s.signedIn(ctx, binary, agent.home, "")
	}

	// A sign-in begun over one not finished ends the first: one URL is live.
	if pending, cancel := agent.signingIn.take(); pending != nil {
		if err := pending.Abandon(); err != nil {
			s.logger.Warnw("the sign-in begun before was not ended cleanly", "agent", agent.did, "error", err)
		}
		cancel()
	}
	// Under the node's own context, not the caller's: the caller leaves, and
	// the process waits for the code until the window ends.
	window, cancel := context.WithTimeout(s.ctx, signInWindow)
	in, err := claudecode.SignIn{Binary: binary, Home: agent.home, Billing: sent["billing"]}.Begin(window)
	if err != nil {
		cancel()
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the sign-in did not begin: " + err.Error()}
	}
	agent.signingIn.mu.Lock()
	agent.signingIn.pending, agent.signingIn.cancel = in, cancel
	agent.signingIn.mu.Unlock()
	// A window that closes with no code leaves nothing pending.
	go func() {
		<-window.Done()
		agent.signingIn.mu.Lock()
		defer agent.signingIn.mu.Unlock()
		if agent.signingIn.pending == in {
			if err := in.Abandon(); err != nil {
				s.logger.Warnw("a sign-in nobody finished was not ended cleanly", "agent", agent.did, "error", err)
			}
			agent.signingIn.pending, agent.signingIn.cancel = nil, nil
		}
	}()
	return s.signedIn(ctx, binary, agent.home, in.URL)
}

// signedIn is login's answer: the URL live, if any, and what Claude Code says.
func (s *QNTXServer) signedIn(ctx context.Context, binary, home, url string) (any, *protocol.Refusal) {
	status, err := s.claudeStatus(ctx, binary, home)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "Claude Code did not say whether the agent is signed in: " + err.Error()}
	}
	return map[string]any{"url": url, "signed_in": status.SignedIn, "auth_method": status.AuthMethod, "api_provider": status.APIProvider}, nil
}

// claudeStatus asks Claude Code whether the agent under home is signed in.
func (s *QNTXServer) claudeStatus(ctx context.Context, binary, home string) (claudecode.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, statusWindow)
	defer cancel()
	return claudecode.StatusOf(ctx, binary, home)
}
