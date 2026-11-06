package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/syscap"
	errors "github.com/teranos/sacred-error"
)

// agentCardPath is where an agent that has never seen the node finds it
// (§8.2, §14.3).
const agentCardPath = "/.well-known/agent-card.json"

// agentCardSigna is the signa the public card shows, in this order, the ones
// of these that are signa:
// "Namespaces, MCP, Plugins, Attestations, Staand, i, mail, access tokens"
var agentCardSigna = []string{"namespaces", "plugins", "staands", "i", "mail"}

// nodeExtensionURI names the extension that carries what the AgentCard has no
// field for: the node's DID, its health and what this build can do.
const nodeExtensionURI = "https://github.com/teranos/QNTX/blob/main/docs/adr/ADR-039-sigils.md#the-node-extension-v1"

// cardBase is the card without its skills: what am.toml says of the node, the
// ways it is reached, and the node extension.
func (s *QNTXServer) cardBase(r *http.Request) agentCard {
	return agentCard{
		Name:        appcfg.GetString("node.name"),
		Description: appcfg.GetString("node.description"),
		Version:     version.VersionTag,
		URL:         a2aURL(r),
		// "Implement default output types": what the node gives is JSON,
		// everywhere, and the Node Agent's text is its skill's own.
		OutputModes: cardModes,
		Skills:      []cardSkill{},
		Extensions:  []a2asdk.AgentExtension{s.nodeExtension(r)},
		Session:     auth.SessionCookie,
	}
}

// nodeExtension is the node's DID, its health as /health says it, its
// system capabilities as am syscap says them, and where its MCP answers.
func (s *QNTXServer) nodeExtension(r *http.Request) a2asdk.AgentExtension {
	params := map[string]any{}
	if node, held := s.nodeKey(); held {
		params["did"] = node.DID
	}
	_, params["health"] = s.health(r.Context())
	// "it lets agents figure out MCP surface amongst other things"
	params["mcp"] = map[string]any{"url": nodeURL(r) + "/mcp", "protocolVersion": mcpProtocolVersion}
	// A Struct takes JSON's values, so the capabilities go through JSON.
	if raw, err := json.Marshal(syscap.Get(s.store)); err == nil {
		var capabilities map[string]any
		if json.Unmarshal(raw, &capabilities) == nil {
			params["syscap"] = capabilities
		}
	}
	return a2asdk.AgentExtension{
		URI:         nodeExtensionURI,
		Description: "The node's DID, its health, what this build can do, and where its MCP answers.",
		Params:      params,
	}
}

// nodeKey is the node's own identity, when it holds one: its DID, and the key
// that signs as it.
func (s *QNTXServer) nodeKey() (*nodedid.Handler, bool) {
	if s.nodeDID != nil && s.nodeDID.DID != "" {
		return s.nodeDID, true
	}
	return nil, false
}

// skillOf is a signum as a skill. Its id is the signum. The Node Agent is what
// a message sent to the node reaches, and it takes text and gives text.
func skillOf(signum *protocol.Signum) cardSkill {
	skill := cardSkill{ID: signum.GetName(), Name: signum.GetName(), Description: signum.GetDescription(), Tags: signum.GetTags()}
	if signum.GetName() == nodeAgentSignumName {
		skill.Modes = []string{textPlain}
	}
	return skill
}

// a2aCard is the card the node can give the caller of r, with a skill per
// signum the caller reaches over A2A (ADR-039). GetExtendedAgentCard answers
// it (§3.1.11), and am node shows it.
func (s *QNTXServer) a2aCard(r *http.Request) agentCard {
	admitted, known := auth.AdmissionFrom(r.Context())
	return s.a2aCardOf(r, admitted, known)
}

// a2aCardOf is a2aCard for a caller already read off r.
func (s *QNTXServer) a2aCardOf(r *http.Request, admitted auth.Admission, known bool) agentCard {
	card := s.cardBase(r)
	for _, signum := range s.checkedSigna() {
		// "make am the card, not a skill on it": the Agent Card describes the
		// node, and a skill is a capability the node can perform.
		if signum.GetName() == amSignumName {
			continue
		}
		reached := false
		for _, sigil := range signum.GetSigils() {
			held := heldBy{signum: signum.GetName(), sigil: sigil}
			if reaching, anyone := s.reachingOver(reach.OverA2A, held); offeredTo(admitted, known, reaching, anyone) {
				reached = true
				break
			}
		}
		if !reached {
			continue
		}
		card.Skills = append(card.Skills, skillOf(signum.Signum))
	}
	return card
}

// publicCard is the card at agentCardPath, read by somebody the node does not
// know: the signa agentCardSigna names, each saying it takes the bearer token.
func (s *QNTXServer) publicCard(r *http.Request) agentCard {
	card := s.cardBase(r)
	held := s.checkedSigna()
	for _, name := range agentCardSigna {
		at := slices.IndexFunc(held, func(signum sigil.Signum) bool { return signum.GetName() == name })
		if at < 0 {
			continue
		}
		skill := skillOf(held[at].Signum)
		skill.Bearer = true
		card.Skills = append(card.Skills, skill)
	}
	return card
}

// HandleAgentCard serves the public card: a2a-go's card handler, the card
// signed with the node's own key (§8.4) by a2a-go's signed card producer.
func (s *QNTXServer) HandleAgentCard(w http.ResponseWriter, r *http.Request) {
	producer := a2asrv.AgentCardProducerFn(func(context.Context) (*a2asdk.AgentCard, error) {
		return s.publicCard(r).sdk(), nil
	})
	a2asrv.NewAgentCardHandler(a2asrv.NewSignedCardProducer(producer, s.cardSigners)).ServeHTTP(w, r)
}

// cardSigners is the node's own key, under the key id its DID document names
// it by, so whoever holds the node's did:key holds what verifies its cards.
func (s *QNTXServer) cardSigners(context.Context) ([]*a2acrypto.Signer, error) {
	node, held := s.nodeKey()
	if !held {
		return nil, errors.New("a card is signed with the node's key, and this node holds none")
	}
	signer, err := a2acrypto.NewSigner(a2acrypto.SignerConfig{PrivateKey: node.PrivateKey, KeyID: node.KeyID()})
	if err != nil {
		return nil, errors.Wrap(err, "the node's key signs no card")
	}
	return []*a2acrypto.Signer{signer}, nil
}

// signedCard is card signed with the node's key, as a2a-go's signed card
// producer signs the public one.
func (s *QNTXServer) signedCard(ctx context.Context, card *a2asdk.AgentCard) (*a2asdk.AgentCard, error) {
	return a2asrv.NewSignedCardProducer(a2asrv.AgentCardProducerFn(func(context.Context) (*a2asdk.AgentCard, error) {
		return card, nil
	}), s.cardSigners).Card(ctx)
}

// mcpProtocolVersion is the newest MCP the go-sdk this node is built with
// speaks, which a client asking for the newest is answered in. The SDK keeps it
// unexported, so TestTheCardNamesTheMCPTheNodeSpeaks holds it to a handshake.
const mcpProtocolVersion = "2026-07-28"

// nodeURL is the node as the caller of r reached it.
func nodeURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// a2aURL is where the caller of r reached the binding.
func a2aURL(r *http.Request) string {
	return nodeURL(r) + a2aPrefix[:len(a2aPrefix)-1]
}

// cardSkill is one signum as A2A sees it: "On the Agent Card a signum is
// described as an AgentSkill" (ADR-039).
type cardSkill struct {
	ID, Name, Description string
	Tags                  []string
	// Bearer says calling the skill takes the node's bearer token, for a card
	// that shows a skill to whoever reads it.
	Bearer bool
	// Modes is the media types the skill takes and gives, in place of the
	// card's; nil holds the card's.
	Modes []string
}

// cardModes is the media types the node takes and gives: JSON, everywhere.
var cardModes = []string{"application/json"}

// agentCard is what the node can say of itself to one caller.
type agentCard struct {
	Name, Description, Version string
	// URL is where the binding answers, and Tenant the agent behind it.
	URL, Tenant string
	// OutputModes is the card's default output modes, which a skill's Modes
	// override.
	OutputModes []string
	Skills      []cardSkill
	Extensions  []a2asdk.AgentExtension
	// Session is the cookie a person's passkey session is carried in, which
	// the node admits as it admits the bearer token.
	Session string
}

// sdk is the card as a2a-go's AgentCard. It is reached over HTTP+JSON and
// JSON-RPC, both at its URL.
func (c agentCard) sdk() *a2asdk.AgentCard {
	bearer := a2asdk.SecurityRequirements{"bearer": {}}
	card := &a2asdk.AgentCard{
		Name: c.Name, Description: c.Description, Version: c.Version,
		SupportedInterfaces: []*a2asdk.AgentInterface{
			{URL: c.URL, ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version, Tenant: c.Tenant},
			{URL: c.URL, ProtocolBinding: a2asdk.TransportProtocolJSONRPC, ProtocolVersion: a2asdk.Version, Tenant: c.Tenant},
		},
		Capabilities:         a2asdk.AgentCapabilities{PushNotifications: true, ExtendedAgentCard: true, Extensions: c.Extensions},
		SecuritySchemes:      a2asdk.NamedSecuritySchemes{"bearer": a2asdk.HTTPAuthSecurityScheme{Scheme: "Bearer"}},
		SecurityRequirements: a2asdk.SecurityRequirementsOptions{bearer},
		DefaultInputModes:    cardModes,
		DefaultOutputModes:   c.OutputModes,
		Skills:               []a2asdk.AgentSkill{},
	}
	if c.Session != "" {
		card.SecuritySchemes["session"] = a2asdk.APIKeySecurityScheme{Description: "A person's passkey session", Location: a2asdk.APIKeySecuritySchemeLocationCookie, Name: c.Session}
		card.SecurityRequirements = append(card.SecurityRequirements, a2asdk.SecurityRequirements{"session": {}})
	}
	for _, skill := range c.Skills {
		tags := skill.Tags
		if tags == nil {
			tags = []string{}
		}
		held := a2asdk.AgentSkill{ID: skill.ID, Name: skill.Name, Description: skill.Description, Tags: tags, InputModes: skill.Modes, OutputModes: skill.Modes}
		if skill.Bearer {
			held.SecurityRequirements = a2asdk.SecurityRequirementsOptions{bearer}
		}
		card.Skills = append(card.Skills, held)
	}
	return card
}

// cardJSON is the card as a2a-go writes it.
func cardJSON(card *a2asdk.AgentCard) (json.RawMessage, error) {
	raw, err := json.Marshal(card)
	return raw, errors.Wrap(err, "the card did not marshal")
}
