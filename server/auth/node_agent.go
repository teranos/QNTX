package auth

import (
	"context"

	errors "github.com/teranos/sacred-error"
)

// "A signature proves the key, and nothing reusable leaves the node."
//
// An agent the node runs reaches the node at a door of its own: a listener the
// node opened on its own machine for that agent alone. What arrives there is
// that agent, so nothing is handed to it, written down for it, or presented.

// NodeAgentLabel is the Node Agent's name: what a line names it by.
const NodeAgentLabel = "root-agent"

// ElementAgentLabel heads an element agent's name: element-agent:<element> (ADR-054).
const ElementAgentLabel = "element-agent"

// doorKey is where a request that came through an agent's door carries the grant of that agent.
type doorKey struct{}

// ThroughDoor is ctx for a request that arrived at the door the node opened
// for the agent whose grant this is. Only the node's own listener sets it.
func ThroughDoor(ctx context.Context, grant Grant) context.Context {
	return context.WithValue(ctx, doorKey{}, grant)
}

// doorGrant is the grant of the agent whose door a request came through.
func doorGrant(ctx context.Context) (Grant, bool) {
	grant, ok := ctx.Value(doorKey{}).(Grant)
	return grant, ok
}

// NamespaceAgentGrant is a namespace agent (ADR-048): acting in its namespace
// and nowhere else, speaking for whoever opted the namespace into it.
func (h *Handler) NamespaceAgentGrant(did, label, mintedBy, namespace string) (Grant, error) {
	if mintedBy == "" {
		return Grant{}, errors.Newf("nobody opted %s into its agent, so there is nobody for it to speak for", namespace)
	}
	return h.agentGrant(Grant{DID: did, Label: label, MintedBy: mintedBy, Level: LevelToken, Namespaces: []string{namespace}})
}

// NodeAgentGrant is the Node Agent (ADR-048): ROOT's kind, which minting hands
// to nobody; the node runs it as itself.
func (h *Handler) NodeAgentGrant(did string) (Grant, error) {
	return h.rootsAgent(did, NodeAgentLabel, "the Node Agent")
}

// ElementAgentGrant is an element agent (ADR-054): ROOT's kind, as the Node
// Agent is, since the element is ROOT's to shake.
func (h *Handler) ElementAgentGrant(did, element string) (Grant, error) {
	return h.rootsAgent(did, ElementAgentLabel+":"+element, "the element "+element)
}

// rootsAgent is an agent that is ROOT's, labelled label and called as a sentence names it.
func (h *Handler) rootsAgent(did, label, called string) (Grant, error) {
	// Every entry reaches the one User that is ROOT (ADR-031).
	roots := h.identities.roots()
	if len(roots) == 0 {
		return Grant{}, errors.Newf("auth.root_identities names nobody, so there is no ROOT for %s to speak for", called)
	}
	return h.agentGrant(Grant{DID: did, Label: label, MintedBy: roots[0], Level: LevelRoot})
}

// agentGrant is grant with the person it speaks for resolved, as a session
// resolves them when it is made.
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
