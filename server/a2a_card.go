package server

import (
	"net/http"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
)

// a2aCard is the card the node can give the caller of r: what am node says of
// it, and a skill per signum the caller reaches over A2A, "a caller is shown
// only what they reach" (ADR-039). GetExtendedAgentCard does not serve it
// until a public card declares the capability (§3.3.4).
func (s *QNTXServer) a2aCard(r *http.Request) a2a.Card {
	card := a2a.Card{
		Name:        appcfg.GetString("node.name"),
		Description: appcfg.GetString("node.description"),
		Version:     version.VersionTag,
		URL:         a2aURL(r),
		MCP:         a2a.Interface{URL: nodeURL(r) + "/mcp", Version: mcpProtocolVersion},
		Skills:      []a2a.Skill{},
	}
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
		// id is required and nothing the node has follows it yet.
		card.Skills = append(card.Skills, a2a.Skill{
			Name: signum.GetName(), Description: signum.GetDescription(), Tags: signum.GetTags(),
		})
	}
	return card
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
