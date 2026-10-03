package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/syscap"
	"google.golang.org/protobuf/encoding/protojson"
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

// cardBase is the card without its skills: what am node says of the node, the
// ways it is reached, and the node extension.
func (s *QNTXServer) cardBase(r *http.Request) a2a.Card {
	return a2a.Card{
		Name:        appcfg.GetString("node.name"),
		Description: appcfg.GetString("node.description"),
		Version:     version.VersionTag,
		URL:         a2aURL(r),
		Skills:      []a2a.Skill{},
		Extensions:  []a2a.Extension{s.nodeExtension(r)},
	}
}

// nodeExtension is the node's DID, its health as /health says it, its
// system capabilities as am syscap says them, and where its MCP answers.
func (s *QNTXServer) nodeExtension(r *http.Request) a2a.Extension {
	params := map[string]any{}
	if s.nodeDID != nil && s.nodeDID.DID != "" {
		params["did"] = s.nodeDID.DID
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
	return a2a.Extension{
		URI:         nodeExtensionURI,
		Description: "The node's DID, its health, what this build can do, and where its MCP answers.",
		Params:      params,
	}
}

// skillOf is a signum as a skill. Its id is the signum.
func skillOf(signum *protocol.Signum) a2a.Skill {
	return a2a.Skill{ID: signum.GetName(), Name: signum.GetName(), Description: signum.GetDescription(), Tags: signum.GetTags()}
}

// a2aCard is the card the node can give the caller of r, with a skill per
// signum the caller reaches over A2A (ADR-039). GetExtendedAgentCard does not
// serve it until a public card declares the capability (§3.3.4).
func (s *QNTXServer) a2aCard(r *http.Request) a2a.Card {
	card := s.cardBase(r)
	admitted, known := auth.AdmissionFrom(r.Context())
	for _, signum := range s.checkedSigna() {
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
func (s *QNTXServer) publicCard(r *http.Request) a2a.Card {
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

// HandleAgentCard serves the public card. A card lacking a field the spec
// requires is no AgentCard (§5.7), and the well-known URI answers an AgentCard
// (§14.3), so a card that lacks one is refused, naming what it lacks.
func (s *QNTXServer) HandleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, r.Method+" is not how the agent card is read")
		return
	}
	card, err := s.publicCard(r).Message()
	if err != nil {
		s.logger.Errorw("The agent card was not built", "error", err)
		writeError(w, http.StatusInternalServerError, "the agent card was not built: "+err.Error())
		return
	}
	if missing := a2a.Missing(card); len(missing) > 0 {
		s.logger.Errorw("The agent card lacks what the spec requires, so it is not served", "missing", missing)
		writeError(w, http.StatusInternalServerError, "the agent card lacks what the spec requires: "+strings.Join(missing, ", "))
		return
	}
	marshalled, err := protojson.Marshal(card.Interface())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the agent card did not marshal: "+err.Error())
		return
	}
	// protojson varies its whitespace on purpose; compacted, the ETag is the
	// card's and not the encoder's.
	var body bytes.Buffer
	if err := json.Compact(&body, marshalled); err != nil {
		writeError(w, http.StatusInternalServerError, "the agent card did not compact: "+err.Error())
		return
	}
	sum := sha256.Sum256(body.Bytes())
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	// §8.6.1. Short, because health is on the card.
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("ETag", etag)
	if matches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body.Bytes()); err != nil {
		s.logger.Warnw("The agent card was not delivered", "peer", r.RemoteAddr, "error", err)
	}
}

// matches is If-None-Match against one strong ETag (RFC 9110 §13.1.2): a list
// of tags, a weak one compared by its opaque part, or *.
func matches(header, etag string) bool {
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
		if tag == "*" || tag == etag {
			return true
		}
	}
	return false
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
