package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "i want the ChatGPT message to be collected right? as in, not sent immediately?"
// "laye owning collections, with the island in laye."
//
// What a person's connector says to a session is the person's word, and it is
// collected by laye: it stands with what else waits on them, and goes when
// they release it.

// connectorSaidPath is where laye takes what a connector said (laye-front CONNECTOR_SAID).
const connectorSaidPath = "/laye/collected/said"

// handedWithin is how long laye is given to collect it.
var handedWithin = 10 * time.Second

// aConnectorSpeaks is whether r is a connector's: a token issued through a
// client under another DID than the client's own, as ChatGPT's and claude.ai's
// are. A person's own session and an agent's own key are not.
func aConnectorSpeaks(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	admitted, gated := auth.AdmissionFrom(r.Context())
	// A connector is admitted as the person who said yes, with no Grant, its
	// own token's DID beside its client's (auth admittedAsConnector,
	// admittedAsThePerson).
	if !gated || admitted.TokenDID == "" || admitted.ClientDID == "" {
		return "", false
	}
	return admitted.TokenDID, admitted.ClientDID != admitted.TokenDID
}

// layeCollected is laye's answer: the Task of what it collected.
type layeCollected struct {
	Task struct {
		ID     string `json:"id"`
		Status struct {
			State   string `json:"state"`
			Message struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"message"`
		} `json:"status"`
	} `json:"task"`
}

// layeOrigin is the [laye] origin am.toml names, without a trailing slash, or
// empty when it names none.
func (s *QNTXServer) layeOrigin() string {
	if s.deps == nil || s.deps.cfg == nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(s.deps.cfg.Laye.Origin), "/")
}

// layeReachable is nil for an origin a node-signed request may go to: over
// HTTPS, or plain HTTP to this machine alone (CodeRabbit on #1822: what a
// person said and the node's signature never cross a network in the clear).
func layeReachable(origin string) error {
	at, err := url.Parse(origin)
	if err != nil {
		return errors.Wrapf(err, "the [laye] origin %q does not read", origin)
	}
	if at.Scheme == "https" {
		return nil
	}
	if host := at.Hostname(); at.Scheme == "http" && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return nil
	}
	return errors.Newf("the [laye] origin %q is not HTTPS: the node signs what it hands laye, and sends it over HTTPS or to this machine alone", origin)
}

// toLaye follows no redirect: a node-signed request goes to the origin
// am.toml names and nowhere it is sent on to.
var toLaye = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// handToLaye hands what a connector said to the agent called to laye, which
// collects it. It answers what laye said of it.
func (s *QNTXServer) handToLaye(ctx context.Context, to, fromDID, from, says, contextID string) (string, error) {
	return s.handToLayeAs(ctx, layeSaid{To: to, FromDID: fromDID, From: from, Says: says, Context: contextID})
}

// layeSaid is what laye is handed of one thing said to an agent (laye-front
// ConnectorSaid): who to, by name and, for this node's own agent, by key;
// whose it is; and whether it wakes the agent.
type layeSaid struct {
	To      string `json:"to"`
	ToDID   string `json:"to_did,omitempty"`
	FromDID string `json:"from_did"`
	From    string `json:"from"`
	Says    string `json:"says"`
	Context string `json:"context"`
	// The person who sees what waits for this node's own agent, which laye
	// hands back to this node when it is released (laye_delivered.go).
	Person string `json:"person,omitempty"`
	Node   bool   `json:"node,omitempty"`
	Quiet  bool   `json:"quiet,omitempty"`
}

// handToLayeAs hands one thing said to laye, which collects it, and answers
// what laye said of it.
func (s *QNTXServer) handToLayeAs(ctx context.Context, said layeSaid) (string, error) {
	to, fromDID := said.To, said.FromDID
	origin := s.layeOrigin()
	if origin == "" {
		return "", errors.New("am.toml names no [laye] origin to hand it to")
	}
	if refused := layeReachable(origin); refused != nil {
		return "", refused
	}
	if s.authHandler == nil {
		return "", errors.New("this node has no key to sign as")
	}
	signed, err := s.authHandler.SignedAsNode(origin)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(said)
	if err != nil {
		return "", errors.Wrap(err, "what was said did not serialize")
	}
	within, done := context.WithTimeout(ctx, handedWithin)
	defer done()
	req, err := http.NewRequestWithContext(within, http.MethodPost, origin+connectorSaidPath, bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "the request to laye was not made")
	}
	req.Header.Set("Authorization", signed)
	req.Header.Set("Content-Type", "application/json")
	resp, err := toLaye.Do(req)
	if err != nil {
		return "", errors.Wrapf(err, "laye at %s did not answer", origin)
	}
	defer resp.Body.Close()
	answered, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", errors.Wrap(err, "laye's answer did not arrive")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("laye answered %d: %s", resp.StatusCode, strings.TrimSpace(string(answered)))
	}
	var got layeCollected
	if err := json.Unmarshal(answered, &got); err != nil {
		return "", errors.Wrapf(err, "laye's answer is not a Task: %s", answered)
	}
	answered = nil
	text := ""
	for _, p := range got.Task.Status.Message.Parts {
		text += p.Text
	}
	s.logger.Infow("what was said to an agent is collected by laye", "to", to, "from", fromDID, "node", said.Node, "laye_task", got.Task.ID)
	return text + " (laye task " + got.Task.ID + ")", nil
}
