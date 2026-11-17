package auth

import (
	"crypto/ed25519"
	"slices"
	"strings"

	"github.com/teranos/QNTX/internal/named"
	errors "github.com/teranos/sacred-error"
)

// "We need this:"
// "a QNTX change that declares grants in am.toml"
// "The node would hold these as live grants while they're listed."
// "Their rows land in fleetingmem, signed by their own key."

// declaredID is the id a grant am.toml lists goes by: no record holds it, so
// nothing can revoke it but striking its line.
func declaredID(label string) string {
	return "am.toml:" + label
}

// DeclaredGrant is one grant auth.grants lists: a key, what it is called
// here, and the namespaces it acts in.
type DeclaredGrant struct {
	Label      string
	DID        string
	Namespaces []string
}

// SetGrants holds the grants auth.grants lists in place of those it listed
// before: each a live grant for its key while its line stands. A list that
// does not hold leaves the grants held before as they were.
//
// A grant is ATTESTOR in its namespaces, as a key that signs for its person
// is. What it writes is signed by the key itself, and what it may write is
// what the lines of roles its label holds say. It speaks for whoever am.toml
// is ROOT's, as the Node Agent does: a token speaks for whoever minted it,
// and striking them out of am.toml reaches it too.
func (h *Handler) SetGrants(listed []DeclaredGrant) error {
	minted, err := h.mintedLabelsAndDIDs()
	if err != nil {
		return err
	}
	roots := h.identities.roots()
	if len(listed) > 0 && len(roots) == 0 {
		return errors.New("auth.root_identities names nobody, so there is nobody for auth.grants to speak for")
	}
	held := make(map[string]TokenInfo, len(listed))
	labels := map[string]bool{}
	for _, grant := range listed {
		label, did := strings.TrimSpace(grant.Label), strings.TrimSpace(grant.DID)
		if !names(label) {
			return errors.Newf("auth.grants names %s as %q, which is no label", did, grant.Label)
		}
		if labels[label] {
			return errors.Newf("auth.grants names the label %s twice", label)
		}
		if minted[label] {
			return errors.Newf("auth.grants names %s, and a token minted here is already named %s", label, label)
		}
		key, err := DecodeUserDID(did)
		if err != nil {
			return errors.Wrapf(err, "auth.grants names %s by %q, which is not an ed25519 did:key", label, did)
		}
		if len(key) != ed25519.PublicKeySize {
			return errors.Newf("auth.grants names %s by %q, whose key is not an ed25519 public key", label, did)
		}
		if _, twice := held[did]; twice {
			return errors.Newf("auth.grants names %s twice", did)
		}
		if minted[did] {
			return errors.Newf("auth.grants names %s for %s, and a token minted here already holds that key", did, label)
		}
		// A grant acting in no namespace would admit its key to nothing.
		declared, err := named.ItemsOf("auth.grants, for the namespaces "+label+" acts in,", grant.Namespaces)
		if err != nil {
			return err
		}
		acts := make([]string, 0, len(declared.All()))
		for _, written := range declared.All() {
			namespace := strings.TrimSpace(written)
			if !names(namespace) {
				return errors.Newf("auth.grants names %s acting in %q, which names no namespace", label, written)
			}
			if slices.ContainsFunc(acts, sameNamespace(namespace)) {
				return errors.Newf("auth.grants names %s acting in %s twice", label, namespace)
			}
			acts = append(acts, namespace)
		}
		labels[label] = true
		grant, err := h.agentGrant(Grant{DID: did, Label: label, MintedBy: roots[0], Level: LevelAttestor, Namespaces: acts})
		if err != nil {
			return err
		}
		held[did] = TokenInfo{
			ID:                  declaredID(label),
			Label:               grant.Label,
			DID:                 grant.DID,
			MintedBy:            grant.MintedBy,
			MintedByUser:        grant.MintedByUser,
			MintedByDisplayName: grant.MintedByDisplayName,
			Level:               grant.Level,
			Namespaces:          grant.Namespaces,
		}
	}
	h.declared.Store(&held)
	return nil
}

// declaredGrant is the grant auth.grants lists for did, and whether it lists one.
func (h *Handler) declaredGrant(did string) (TokenInfo, bool) {
	held := h.declared.Load()
	if held == nil {
		return TokenInfo{}, false
	}
	grant, listed := (*held)[did]
	return grant, listed
}

// declaresLabel is whether auth.grants lists a grant called label.
func (h *Handler) declaresLabel(label string) bool {
	held := h.declared.Load()
	if held == nil {
		return false
	}
	for _, grant := range *held {
		if grant.Label == label {
			return true
		}
	}
	return false
}

// mintedLabelsAndDIDs is every label and key a token minted here holds,
// revoked ones among them: revocation is a switch, and a switched-off token
// comes back.
func (h *Handler) mintedLabelsAndDIDs() (map[string]bool, error) {
	minted := map[string]bool{}
	if h.tokens == nil {
		return minted, nil
	}
	listed, err := h.tokens.List()
	if err != nil {
		return nil, errors.Wrap(err, "the token store did not answer, so auth.grants is not held against it")
	}
	for _, t := range listed {
		minted[t.Label] = true
		if t.DID != "" {
			minted[t.DID] = true
		}
	}
	return minted, nil
}
