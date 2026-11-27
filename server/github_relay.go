package server

// "why cant we make q relay it"

// 「[Opus 5.5] A relay is ROOT's line that this node's subsystems ask GitHub through another」
// 「[Opus 5.5] node, the one holding the App, by that node's own github ask (ADR-056).」

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/subsystem"
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
	origin, err := relayOrigin(relay.URL)
	if err != nil {
		return nil, errors.Wrapf(err, "%s was not asked through the relay", operation)
	}
	signed, err := signAs(origin)
	if err != nil {
		return nil, errors.Wrapf(err, "the ask of %s to %s was not signed", operation, relay.URL)
	}
	asking := map[string]string{"operation": operation, "request": string(request)}
	// 「[Opus 5.5] An ask made as one installation is that installation at the far node too.」
	if installation, of := services.InstallationOf(ctx); of {
		asking["installation"] = strconv.FormatInt(installation, 10)
	}
	body, err := json.Marshal(asking)
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
	answer, err := toRelay.Do(r)
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
		Answer map[string]json.RawMessage `json:"answer"`
	}
	if err := json.Unmarshal(raw, &given); err != nil {
		return nil, errors.Wrapf(err, "what %s answered %s did not read", relay.URL, operation)
	}
	// 「[Opus 5.5] The far node answered GitHub's answer alone, success said by answering at」
	// 「[Opus 5.5] all; here it is said again, as this node's own GitHubService answers it.」
	if given.Answer == nil {
		given.Answer = map[string]json.RawMessage{}
	}
	given.Answer["success"] = json.RawMessage("true")
	answered, err := json.Marshal(given.Answer)
	if err != nil {
		return nil, errors.Wrapf(err, "what %s answered %s did not encode", relay.URL, operation)
	}
	return answered, nil
}

// 「[Opus 5.5] relayCredential asks the relay for a git credential for one repository, as」
// 「[Opus 5.5] this node, held to what relayAsk is held to.」
func relayCredential(ctx context.Context, signAs func(origin string) (string, error), relay GitHubRelay, owner, repo string) (subsystem.GitCredential, error) {
	origin, err := relayOrigin(relay.URL)
	if err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "no credential for %s/%s was asked of the relay", owner, repo)
	}
	signed, err := signAs(origin)
	if err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "the ask for a credential for %s/%s to %s was not signed", owner, repo, relay.URL)
	}
	body, err := json.Marshal(map[string]string{"owner": owner, "repo": repo})
	if err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "the repository %s/%s did not encode", owner, repo)
	}
	ctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+githubCredentialPath, bytes.NewReader(body))
	if err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "the ask for a credential to %s is not a request", relay.URL)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", signed)
	answer, err := toRelay.Do(r)
	if err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "%s was not reached for a credential for %s/%s", relay.URL, owner, repo)
	}
	defer answer.Body.Close()
	raw, err := io.ReadAll(answer.Body)
	if err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "what %s answered for %s/%s did not read", relay.URL, owner, repo)
	}
	if answer.StatusCode != http.StatusOK {
		return subsystem.GitCredential{}, errors.Newf("%s answered the credential for %s/%s with %s: %s", relay.URL, owner, repo, strconv.Itoa(answer.StatusCode), raw)
	}
	var given struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &given); err != nil {
		return subsystem.GitCredential{}, errors.Wrapf(err, "what %s answered for %s/%s did not read", relay.URL, owner, repo)
	}
	if given.Username == "" || given.Password == "" {
		return subsystem.GitCredential{}, errors.Newf("%s answered the credential for %s/%s with no username or password", relay.URL, owner, repo)
	}
	return subsystem.GitCredential{Username: given.Username, Password: given.Password}, nil
}

// 「[Opus 5.5] toRelay follows no redirect: the node's signature goes to the relay it」
// 「[Opus 5.5] names and nowhere it is sent on to, as toLaye's does.」
var toRelay = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// 「[Opus 5.5] relayOrigin is the origin raw names, which the node signs for: over HTTPS, or」
// 「[Opus 5.5] over HTTP to this machine alone, with no path to it.」
func relayOrigin(raw string) (string, error) {
	at, err := url.Parse(raw)
	if err != nil {
		return "", errors.Wrapf(err, "the relay %q does not read", raw)
	}
	if at.Host == "" || at.User != nil || strings.TrimRight(at.Path, "/") != "" || at.RawQuery != "" || at.Fragment != "" {
		return "", errors.Newf("the relay %q is not an origin: the node signs for scheme and host alone", raw)
	}
	host := at.Hostname()
	local := host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if at.Scheme != "https" && !(at.Scheme == "http" && local) {
		return "", errors.Newf("the relay %q is not HTTPS: the node signs what it hands over, and sends it over HTTPS or to this machine alone", raw)
	}
	return at.Scheme + "://" + at.Host, nil
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
		origin, err := relayOrigin(relay.URL)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "url", Says: err.Error()}
		}
		relay.URL = origin
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
