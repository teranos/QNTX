package server

import (
	"context"
	"crypto"
	"encoding/json"
	"net/http"
	"strings"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// peerClient is a2a-go's client of the agent peer serves at tenant, its own
// Node Agent at none, carrying the person asking here to the peer: their
// token rides each call as a2a-go's client sends it.
func (s *QNTXServer) peerClient(ctx context.Context, peer auth.Client, tenant string) (*a2aclient.Client, error) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, a2aRefusal(a2asdk.ErrUnauthenticated, "a peer is spoken to as the person asking here, and this came through no gate")
	}
	bearer, err := s.authHandler.BearerFor(admitted, peer)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, err.Error())
	}
	at := strings.TrimSuffix(peer.Origin.JoinPath(a2aPrefix).String(), "/")
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{{URL: at, ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version, Tenant: tenant}},
		a2aclient.WithCallInterceptors(bearerOf(bearer)))
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "no A2A client speaks to "+peer.Label+": "+err.Error())
	}
	return client, nil
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
	client, err := s.peerClient(r.Context(), peer, namespace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Destroy() }()
	card, err := client.GetExtendedAgentCard(r.Context(), &a2asdk.GetExtendedAgentCardRequest{Tenant: namespace})
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not hand over the card of the agent of %s", peer.Label, namespace)
	}
	if err := verifiedByPeer(r.Context(), card, peer); err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the card "+peer.Label+" handed over for the agent of "+namespace+" is not one it signed: "+err.Error())
	}
	return card, nil
}

// verifiedByPeer is whether one of card's signatures is the peer's, by the key
// its did:key names.
func verifiedByPeer(ctx context.Context, card *a2asdk.AgentCard, peer auth.Client) error {
	key, err := auth.DecodeUserDID(peer.DID)
	if err != nil {
		return errors.Wrapf(err, "nodes.peers names %s by %s, which is no key to verify its card with", peer.Label, peer.DID)
	}
	return verifiedBy(ctx, card, key, nodedid.KeyIDOf(peer.DID))
}

// verifiedBy is whether one of card's signatures is key's, under kid, as
// a2a-go's verifier reads it (§8.4.3).
func verifiedBy(ctx context.Context, card *a2asdk.AgentCard, key crypto.PublicKey, kid string) error {
	verifier := a2acrypto.NewVerifier(a2acrypto.VerifierConfig{KeyResolver: a2acrypto.KeyResolverFunc(func(_ context.Context, asked string) (crypto.PublicKey, error) {
		if asked != kid {
			return nil, errors.Newf("%s is no key this card is verified by, which is %s", asked, kid)
		}
		return key, nil
	})})
	raw, err := json.Marshal(card)
	if err != nil {
		return errors.Wrap(err, "the card did not marshal")
	}
	var last error = errors.New("the card carries no signature")
	for i := range card.Signatures {
		if last = verifier.Verify(ctx, raw, &card.Signatures[i]); last == nil {
			return nil
		}
	}
	return last
}
