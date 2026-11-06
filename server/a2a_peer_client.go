package server

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// peerClient is a2a-go's client of the agent peer serves at tenant, its own
// Node Agent at none, carrying the person asking here to the peer: their
// token rides each call as a2a-go's client sends it.
func (s *QNTXServer) peerClient(ctx context.Context, peer auth.Client, tenant string, heard *cardHeard) (*a2aclient.Client, error) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, a2aRefusal(a2asdk.ErrUnauthenticated, "a peer is spoken to as the person asking here, and this came through no gate")
	}
	bearer, err := s.authHandler.BearerFor(admitted, peer)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, err.Error())
	}
	at := strings.TrimSuffix(peer.Origin.JoinPath(a2aPrefix).String(), "/")
	var carried http.RoundTripper = otelhttp.NewTransport(http.DefaultTransport)
	if heard != nil {
		heard.RoundTripper = carried
		carried = heard
	}
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{{URL: at, ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version, Tenant: tenant}},
		// The person's token goes to the peer and nowhere else: a redirect is
		// the peer's answer, not a place to follow it to.
		a2aclient.WithRESTTransport(&http.Client{Transport: carried,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}),
		a2aclient.WithCallInterceptors(bearerOf(bearer)))
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "no A2A client speaks to "+peer.Label+": "+err.Error())
	}
	return client, nil
}

// peerCardTimeout is how long a peer is given to hand over a card. A message
// waits on the peer's turn, which is the caller's to bound, not this.
var peerCardTimeout = 15 * time.Second

// cardHeard keeps the body of the extended card a peer answers, the bytes it
// signed, which a card a2a-go decoded and wrote out again is not.
type cardHeard struct {
	http.RoundTripper
	body []byte
}

// cardHeardMost is the most of a card's body kept to verify it.
const cardHeardMost = 1 << 20

func (c *cardHeard) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.RoundTripper.RoundTrip(r)
	if err != nil || !strings.HasSuffix(r.URL.Path, "/extendedAgentCard") {
		return resp, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cardHeardMost))
	_ = resp.Body.Close()
	if err != nil {
		return nil, errors.Wrap(err, "the extended card's body did not read")
	}
	c.body = body
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// bearerOf is a2a-go's call interceptor that carries token as the bearer.
type bearerOf string

func (b bearerOf) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	req.ServiceParams["Authorization"] = []string{"Bearer " + string(b)}
	return ctx, nil, nil
}

func (bearerOf) After(context.Context, *a2aclient.Response) error { return nil }

// peersAgentCard is the card of the agent a peer serves at namespace, asked of
// the peer with a2a-go's client as the person asking here, and verified by
// a2a-go's verifier against the did:key nodes.peers pins (§8.4.3).
func (s *QNTXServer) peersAgentCard(r *http.Request, peer auth.Client, namespace string) (*a2asdk.AgentCard, error) {
	ctx, cancel := context.WithTimeout(r.Context(), peerCardTimeout)
	defer cancel()
	heard := &cardHeard{}
	client, err := s.peerClient(ctx, peer, namespace, heard)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Destroy() }()
	card, err := client.GetExtendedAgentCard(ctx, &a2asdk.GetExtendedAgentCardRequest{Tenant: namespace})
	if errors.Is(err, a2asdk.ErrTaskNotFound) {
		return nil, a2aRefusal(a2asdk.ErrTaskNotFound, peer.Label+" serves no agent at tenant "+namespace)
	}
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, peer.Label+" did not hand over the card of the agent of "+namespace+": "+err.Error())
	}
	if err := verifiedByPeer(r.Context(), heard.body, peer); err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the card "+peer.Label+" handed over for the agent of "+namespace+" is not one it signed: "+err.Error())
	}
	return card, nil
}

// verifiedByPeer is whether one of card's signatures is the peer's, by the key
// its did:key names.
func verifiedByPeer(ctx context.Context, card []byte, peer auth.Client) error {
	key, err := auth.DecodeUserDID(peer.DID)
	if err != nil {
		return errors.Wrapf(err, "nodes.peers names %s by %s, which is no key to verify its card with", peer.Label, peer.DID)
	}
	return verifiedBy(ctx, card, key, nodedid.KeyIDOf(peer.DID))
}

// verifiedBy is whether one of the signatures of card, as its bytes came, is
// key's, under kid, as a2a-go's verifier reads it (§8.4.3).
func verifiedBy(ctx context.Context, card []byte, key crypto.PublicKey, kid string) error {
	verifier := a2acrypto.NewVerifier(a2acrypto.VerifierConfig{KeyResolver: a2acrypto.KeyResolverFunc(func(_ context.Context, asked string) (crypto.PublicKey, error) {
		if asked != kid {
			return nil, errors.Newf("%s is no key this card is verified by, which is %s", asked, kid)
		}
		return key, nil
	})})
	var signed struct {
		Signatures []a2asdk.AgentCardSignature `json:"signatures"`
	}
	if err := json.Unmarshal(card, &signed); err != nil {
		return errors.Wrap(err, "the card is no JSON its signatures are read from")
	}
	var last error = errors.New("the card carries no signature")
	for i := range signed.Signatures {
		if last = verifier.Verify(ctx, card, &signed.Signatures[i]); last == nil {
			return nil
		}
	}
	return last
}
