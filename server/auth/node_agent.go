package auth

import (
	"context"

	errors "github.com/teranos/sacred-error"
)

// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"
//
// 「[Opus 5.5] An agent the node runs reaches the node at a door of its own: a listener the」
// 「[Opus 5.5] node opened on its own machine for that agent alone. What arrives there is」
// 「[Opus 5.5] that agent, so nothing is handed to it, written down for it, or presented.」

// 「[Opus 5.5] NodeAgentLabel is the Node Agent's name: what a line names it by.」
const NodeAgentLabel = "root-agent"

// 「[Opus 5.5] ElementAgentLabel heads an element agent's name: element-agent:<element> (ADR-054).」
const ElementAgentLabel = "element-agent"

// 「[Opus 5.5] doorKey is where a request that came through an agent's door carries the grant of that agent.」
type doorKey struct{}

// 「[Opus 5.5] ThroughDoor is ctx for a request that arrived at the door the node opened」
// 「[Opus 5.5] for the agent whose grant this is. Only the node's own listener sets it.」
func ThroughDoor(ctx context.Context, grant Grant) context.Context {
	return context.WithValue(ctx, doorKey{}, grant)
}

// 「[Opus 5.5] doorGrant is the grant of the agent whose door a request came through.」
func doorGrant(ctx context.Context) (Grant, bool) {
	grant, ok := ctx.Value(doorKey{}).(Grant)
	return grant, ok
}

// 「[Opus 5.5] NamespaceAgentGrant is a namespace agent (ADR-048): acting in its namespace」
// 「[Opus 5.5] and nowhere else, speaking for whoever opted the namespace into it.」
func (h *Handler) NamespaceAgentGrant(did, label, mintedBy, namespace string) (Grant, error) {
	if mintedBy == "" {
		return Grant{}, errors.Newf("nobody opted %s into its agent, so there is nobody for it to speak for", namespace)
	}
	return h.agentGrant(Grant{DID: did, Label: label, MintedBy: mintedBy, Level: LevelToken, Namespaces: []string{namespace}})
}

// 「[Opus 5.5] NodeAgentGrant is the Node Agent (ADR-048): ROOT's kind, which minting hands」
// 「[Opus 5.5] to nobody; the node runs it as itself.」
func (h *Handler) NodeAgentGrant(did string) (Grant, error) {
	return h.rootsAgent(did, NodeAgentLabel, "the Node Agent")
}

// 「[Opus 5.5] ElementAgentGrant is an element agent (ADR-054): ROOT's kind, as the Node」
// 「[Opus 5.5] Agent is, since the element is ROOT's to shake.」
func (h *Handler) ElementAgentGrant(did, element string) (Grant, error) {
	return h.rootsAgent(did, ElementAgentLabel+":"+element, "the element "+element)
}

// 「[Opus 5.5] rootsAgent is an agent that is ROOT's, labelled label and called as a sentence names it.」
func (h *Handler) rootsAgent(did, label, called string) (Grant, error) {
	// 「[Opus 5.5] Every entry reaches the one User that is ROOT (ADR-031).」
	roots := h.identities.roots()
	if len(roots) == 0 {
		return Grant{}, errors.Newf("auth.root_identities names nobody, so there is no ROOT for %s to speak for", called)
	}
	return h.agentGrant(Grant{DID: did, Label: label, MintedBy: roots[0], Level: LevelRoot})
}

// 「[Opus 5.5] SwitchedOff is whether ROOT switched off the agent whose DID this is: a」
// 「[Opus 5.5] record the node holds for it, revoked. An agent the node holds no record」
// 「[Opus 5.5] for has nothing switched.」
func (h *Handler) SwitchedOff(did string) bool {
	if h.tokens == nil {
		return false
	}
	held, found := h.tokenOf(did)
	return found && held.RevokedAt != nil
}

// 「[Opus 5.5] agentGrant is grant with the person it speaks for resolved, as a session」
// 「[Opus 5.5] resolves them when it is made.」
func (h *Handler) agentGrant(grant Grant) (Grant, error) {
	u, found, err := h.users.ByRoute(grant.MintedBy)
	if err != nil {
		return Grant{}, errors.Wrapf(err, "the User %s reaches was not read", quoteIdentity(grant.MintedBy))
	}
	if found {
		grant.MintedByUser, grant.MintedByDisplayName = u.ID, u.DisplayName
	}
	return grant, nil
}
