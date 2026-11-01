package server

// "why cant we make q relay it"

// A relay is ROOT's line that this node's subsystems ask GitHub through another
// node, the one holding the App, by that node's own github ask (ADR-056).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

const (
	githubRelayLine = "relay"
	// relayWait is how long the far node has to answer one operation.
	relayWait = 2 * time.Minute
)

// GitHubRelay is the node this one asks GitHub through, and where the token it
// asks with is kept.
type GitHubRelay struct {
	URL      string `json:"url"`
	TokenRef string `json:"token_ref"`
}

// Relay is the node's relay, and whether it has one. An empty URL is one turned off.
func (r NodeRecords) Relay() (GitHubRelay, bool, error) {
	newest, err := r.newest(githubSubject)
	if err != nil {
		return GitHubRelay{}, false, err
	}
	as, ok := newest[githubRelayLine]
	if !ok {
		return GitHubRelay{}, false, nil
	}
	relay := GitHubRelay{}
	relay.URL, _ = as.Attributes["url"].(string)
	relay.TokenRef, _ = as.Attributes["token_ref"].(string)
	return relay, relay.URL != "", nil
}

// SetRelay writes the relay whole.
func (r NodeRecords) SetRelay(actor string, relay GitHubRelay) error {
	return r.nodeRecord(actor, githubSubject, githubRelayLine, "_", map[string]any{"url": relay.URL, "token_ref": relay.TokenRef})
}

// relayTokenRef is whether ref says where a token is, rather than being one.
func relayTokenRef(ref string) error {
	if !secretref.IsReference(ref) {
		return errors.New("token_ref says where the token is kept, as env:NAME or ssm:///path, and never is the token")
	}
	return nil
}

// relayAsk asks the far node one operation and hands back GitHub's answer.
func relayAsk(ctx context.Context, relay GitHubRelay, operation string, request []byte) ([]byte, error) {
	token, err := secretref.Resolve(ctx, relay.TokenRef)
	if err != nil {
		return nil, errors.Wrapf(err, "the token for the relay to %s was not read", relay.URL)
	}
	body, err := json.Marshal(map[string]string{"operation": operation, "request": string(request)})
	if err != nil {
		return nil, errors.Wrapf(err, "what %s takes did not encode", operation)
	}
	ctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(relay.URL, "/")+githubPath+"/ask", bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrapf(err, "the relay to %s is not a request", relay.URL)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	answer, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not reach %s", operation, relay.URL)
	}
	defer answer.Body.Close()
	raw, err := io.ReadAll(answer.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "what %s answered %s did not read", relay.URL, operation)
	}
	if answer.StatusCode != http.StatusOK {
		return nil, errors.Newf("%s answered %s with %s: %s", relay.URL, operation, strconv.Itoa(answer.StatusCode), raw)
	}
	var given struct {
		Answer json.RawMessage `json:"answer"`
	}
	if err := json.Unmarshal(raw, &given); err != nil {
		return nil, errors.Wrapf(err, "what %s answered %s did not read", relay.URL, operation)
	}
	return given.Answer, nil
}

func githubRelaySigil() *protocol.Sigil {
	return &protocol.Sigil{
		Name: "relay",
		Does: "Set the node this node's subsystems ask GitHub through, for a node that does not hold the App. An empty url turns it off.",
		Takes: []*protocol.Param{
			{Name: "url", Says: "The origin of the node holding the App. Empty turns the relay off."},
			{Name: "token_ref", Says: "Where a token ROOT on that node minted is kept: env:NAME or ssm:///path."},
		},
		Gives: []*protocol.Field{{Name: "relay", Says: "The relay as it now stands."}},
		Http:  &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/relay"},
	}
}

func (g *gitHub) githubRelay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	relay := GitHubRelay{URL: sent["url"], TokenRef: sent["token_ref"]}
	if relay.URL != "" {
		if err := forwardURL(relay.URL); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "url", Says: err.Error()}
		}
		if err := relayTokenRef(relay.TokenRef); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "token_ref", Says: err.Error()}
		}
	}
	if err := g.host.nodeRecords().SetRelay(actorOf(ctx), relay); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]GitHubRelay{"relay": relay}, nil
}
