package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	if !gated || admitted.Grant == nil || admitted.Grant.DID == "" || admitted.ClientDID == "" {
		return "", false
	}
	return admitted.Grant.DID, admitted.ClientDID != admitted.Grant.DID
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

// handToLaye hands what a connector said to the agent called to laye, which
// collects it. It answers what laye said of it.
func (s *QNTXServer) handToLaye(ctx context.Context, to, fromDID, from, says, contextID string) (string, error) {
	if s.deps == nil || s.deps.cfg == nil || strings.TrimSpace(s.deps.cfg.Laye.Origin) == "" {
		return "", errors.New("am.toml names no [laye] origin to hand it to")
	}
	if s.authHandler == nil {
		return "", errors.New("this node has no key to sign as")
	}
	origin := strings.TrimSuffix(strings.TrimSpace(s.deps.cfg.Laye.Origin), "/")
	signed, err := s.authHandler.SignedAsNode(origin)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]string{"to": to, "from_did": fromDID, "from": from, "says": says, "context": contextID})
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
	resp, err := http.DefaultClient.Do(req)
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
	said := ""
	for _, p := range got.Task.Status.Message.Parts {
		said += p.Text
	}
	s.logger.Infow("what a connector said to a named agent is collected by laye", "to", to, "from", fromDID, "laye_task", got.Task.ID)
	return said + " (laye task " + got.Task.ID + ")", nil
}
