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
	handed, err := client.GetExtendedAgentCard(r.Context(), &a2asdk.GetExtendedAgentCardRequest{Tenant: namespace})
	if carried.status == http.StatusNotFound {
		return nil, a2aRefusal(a2asdk.ErrTaskNotFound, peer.Label+" serves no agent at tenant "+namespace)
	}
	if err != nil && carried.status == 0 {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, peer.Label+" did not answer for the card of the agent of "+namespace+": "+err.Error())
	}
	if err != nil {
		said := bounded(string(carried.body), saidDefectMost)
		if carried.unread != nil {
			said += " (the rest did not read: " + carried.unread.Error() + ")"
		}
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, peer.Label+" answered "+strconv.Itoa(carried.status)+" for the card of the agent of "+namespace+": "+said)
	}
	raw, err := json.Marshal(handed)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the card "+peer.Label+" handed over did not marshal: "+err.Error())
	}
	card, err := a2a.ReadCard(raw)
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
// keeps how the peer answered.
type peerCarrier struct {
	bearer string
	origin *url.URL
	status int
	body   []byte
	// unread is why the answer's body did not read to its end.
	unread error
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, nodeBodyMost))
	_ = resp.Body.Close()
	c.body = body
	if err != nil {
		c.unread = err
		return nil, err
	}
	resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}
