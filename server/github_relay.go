package server

// "why cant we make q relay it"

// 「[Opus 5.5] A relay is ROOT's line that this node's subsystems ask GitHub through another」
// 「[Opus 5.5] node, the one holding the App, by that node's own github ask (ADR-056).」

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

const (
	githubRelayLine = "relay"
	// 「[Opus 5.5] relayWait is how long the far node has to answer one operation.」
	relayWait = 2 * time.Minute
)

// "I WANT TO KILL THIS IDEA, I DONT WANT TO MAINTAIN TOKENS, AGENNTS KEEP REACHING FOR IT AND CREATING MORE TOKENS, THIS ISNT SECURE"

// 「[Opus 5.5] GitHubRelay is the node this one asks GitHub through. It asks as itself,」
// 「[Opus 5.5] signed by its own key (auth.SignedAsNode), so it holds nothing to ask with.」
type GitHubRelay struct {
	URL string `json:"url"`
}

// 「[Opus 5.5] Relay is the node's relay, and whether it has one. An empty URL is one turned off.」
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
	return relay, relay.URL != "", nil
}

// 「[Opus 5.5] SetRelay writes the relay whole.」
func (r NodeRecords) SetRelay(actor string, relay GitHubRelay) error {
	return r.nodeRecord(actor, githubSubject, githubRelayLine, "_", map[string]any{"url": relay.URL})
}

// 「[Opus 5.5] relayAsk asks the far node one operation, as this node, and hands back」
// 「[Opus 5.5] GitHub's answer. signAs is this node's signature for an origin.」
func relayAsk(ctx context.Context, signAs func(origin string) (string, error), relay GitHubRelay, operation string, request []byte) ([]byte, error) {
	origin := strings.TrimRight(relay.URL, "/")
	signed, err := signAs(origin)
	if err != nil {
		return nil, errors.Wrapf(err, "the ask of %s to %s was not signed", operation, relay.URL)
	}
	body, err := json.Marshal(map[string]string{"operation": operation, "request": string(request)})
	if err != nil {
		return nil, errors.Wrapf(err, "what %s takes did not encode", operation)
	}
	ctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+githubPath+"/ask", bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrapf(err, "the relay to %s is not a request", relay.URL)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", signed)
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
			{Name: "url", Says: "The origin of the node holding the App, which this node asks as itself. Empty turns the relay off."},
		},
		Gives: []*protocol.Field{{Name: "relay", Says: "The relay as it now stands."}},
		Http:  &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/relay"},
	}
}

func (g *gitHub) githubRelay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	relay := GitHubRelay{URL: sent["url"]}
	if relay.URL != "" {
		if err := forwardURL(relay.URL); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "url", Says: err.Error()}
		}
	}
	if err := g.host.nodeRecords().SetRelay(actorOf(ctx), relay); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]GitHubRelay{"relay": relay}, nil
}

// 「[Opus 5.5] signedAsNode is this node's signature for origin, made with its own key,」
// 「[Opus 5.5] which every node holds whether or not it runs login.」
func (s *QNTXServer) signedAsNode(origin string) (string, error) {
	if s.nodeDID == nil {
		return "", errors.New("this node holds no key of its own to sign with")
	}
	return auth.SignAs(s.nodeDID.PrivateKey, origin)
}
