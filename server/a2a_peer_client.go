package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// peersAgentCard is the card of the agent a peer serves at namespace, asked of
// the peer over A2A with a2a-go's client as the person asking here, and handed
// over as the peer signed it. The peer is known by the did:key nodes.peers
// pins, which is the key store its signature is verified against (§8.4.3).
// The card's interface is the peer's own, so whoever holds it talks to the
// peer.
func (s *QNTXServer) peersAgentCard(r *http.Request, peer auth.Client, namespace string) (protoreflect.Message, error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated {
		return nil, a2aRefusal(a2asdk.ErrUnauthenticated, "the card of an agent a peer serves is asked of it as the person asking here, and this one came through no gate")
	}
	bearer, err := s.authHandler.BearerFor(admitted, peer)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, peer.Label+" did not hand over the card of the agent of "+namespace+": "+err.Error())
	}
	carried := &peerCarrier{bearer: bearer, origin: peer.Origin}
	at := strings.TrimSuffix(peer.Origin.JoinPath(a2aPrefix).String(), "/")
	client, err := a2aclient.NewFromEndpoints(r.Context(),
		[]*a2asdk.AgentInterface{{URL: at, ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version, Tenant: namespace}},
		// The person's token goes to the peer and nowhere else: a redirect is
		// the peer's answer, not a place to follow it to.
		a2aclient.WithRESTTransport(&http.Client{Timeout: peerCardTimeout, Transport: carried,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "no A2A client speaks to "+peer.Label+": "+err.Error())
	}
	defer func() { _ = client.Destroy() }()
	_, err = client.GetExtendedAgentCard(r.Context(), &a2asdk.GetExtendedAgentCardRequest{Tenant: namespace})
	if carried.status == http.StatusNotFound {
		return nil, a2aRefusal(a2asdk.ErrTaskNotFound, peer.Label+" serves no agent at tenant "+namespace)
	}
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, peer.Label+" answered "+strconv.Itoa(carried.status)+" for the card of the agent of "+namespace+": "+err.Error())
	}
	// The card is verified as the peer sent it: what it signed is ProtoJSON,
	// and a2a-go v2.5's AgentCard does not keep all of it ("streaming": false
	// among it).
	card, err := a2a.ReadCard(carried.body)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, peer.Label+" handed over no card for the agent of "+namespace+": "+err.Error())
	}
	key, err := auth.DecodeUserDID(peer.DID)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "nodes.peers names "+peer.Label+" by "+peer.DID+", which is no key to verify its card with: "+err.Error())
	}
	if err := a2a.Verify(card, key, nodedid.KeyIDOf(peer.DID)); err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the card "+peer.Label+" handed over for the agent of "+namespace+" is not one it signed: "+err.Error())
	}
	return given(card, "the card "+peer.Label+" handed over for the agent of "+namespace)
}

// peerCardTimeout is how long a peer is given to hand over a card.
const peerCardTimeout = 15 * time.Second

// peerCarrier carries the person to a peer, their token on each request, and
// keeps how the peer answered, the card as it sent it. a2a-go v2.5 reads a
// card's security scopes as a list where a2a.proto declares a StringList
// (a2a-go#437, fixed in v2.7): the card is handed to it in the shape it reads.
type peerCarrier struct {
	bearer string
	origin *url.URL
	status int
	body   []byte
}

func (c *peerCarrier) RoundTrip(r *http.Request) (*http.Response, error) {
	carried := r.Clone(r.Context())
	if carried.URL.Scheme == c.origin.Scheme && carried.URL.Host == c.origin.Host {
		carried.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := http.DefaultTransport.RoundTrip(carried)
	if err != nil {
		return nil, err
	}
	c.status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, nodeBodyMost))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	c.body = body
	shaped := scopesShaped(body)
	resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(shaped)), int64(len(shaped))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// scopesShaped is a card's JSON with each security requirement's scopes as
// a2a-go v2.5 reads them, a list, where a2a.proto declares a StringList.
// Anything that is no card is left as it is.
func scopesShaped(raw []byte) []byte {
	var card map[string]any
	if json.Unmarshal(raw, &card) != nil {
		return raw
	}
	shape := func(held any) {
		requirements, _ := held.([]any)
		for _, requirement := range requirements {
			declared, _ := requirement.(map[string]any)
			schemes, _ := declared["schemes"].(map[string]any)
			for name, scopes := range schemes {
				// Scopes already a list, or null, are left as they are.
				declared, isStringList := scopes.(map[string]any)
				if !isStringList {
					continue
				}
				listed, _ := declared["list"].([]any)
				if listed == nil {
					listed = []any{}
				}
				schemes[name] = listed
			}
		}
	}
	shape(card["securityRequirements"])
	skills, _ := card["skills"].([]any)
	for _, skill := range skills {
		if held, isSkill := skill.(map[string]any); isSkill {
			shape(held["securityRequirements"])
		}
	}
	shaped, err := json.Marshal(card)
	if err != nil {
		return raw
	}
	return shaped
}
