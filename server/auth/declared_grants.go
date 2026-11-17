package auth

import (
	"crypto/ed25519"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/named"
	errors "github.com/teranos/sacred-error"
)

// "We need this:"
// "a QNTX change that declares grants in am.toml"
// "「[Opus 5.5] The node would hold these as live grants while they're listed.」"
// "Their rows land in fleetingmem, signed by their own key."

// 「[Opus 5.5] declaredID is the id a grant am.toml lists goes by: no record holds it, so」
// 「[Opus 5.5] nothing can revoke it but striking its line.」
func declaredID(label string) string {
	return "am.toml:" + label
}

// 「[Opus 5.5] DeclaredGrant is one grant auth.grants lists: a key, what it is called」
// 「[Opus 5.5] here, and the namespaces it acts in.」
type DeclaredGrant struct {
	Label      string
	DID        string
	Namespaces []string
	// Roles its label holds in each namespace it acts in, as ROOT's grant line.
	Roles []string
}

// DeclaredWords is one WRITE or READ line auth.words lists: the words the
// roles may write or read, ROOT's as a word line in the store is.
type DeclaredWords struct {
	Write []string
	Read  []string
	Of    []string
	All   bool
}

// declaredLines is what auth.grants and auth.words say about roles, as the
// lines the store would hold had ROOT written them: by namespace, lowercased,
// and the word lines. Read beside the store's, and outranked by nothing but a
// later line of ROOT's.
type declaredLines struct {
	byNamespace map[string][]RoleLine
	words       []WordLine
}

// roleOf is a role as a line names it: one upper-case word.
func roleOf(written string) (string, bool) {
	role := strings.ToUpper(strings.TrimSpace(written))
	if role == "" || strings.ContainsAny(role, " \t\n") {
		return "", false
	}
	return role, true
}

// rolesOf is the roles a line names, each once.
func rolesOf(field string, written []string) ([]string, error) {
	roles := make([]string, 0, len(written))
	for _, w := range written {
		role, ok := roleOf(w)
		if !ok {
			return nil, errors.Newf("%s names %q, which is no role", field, w)
		}
		if slices.Contains(roles, role) {
			return nil, errors.Newf("%s names the role %s twice", field, role)
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// wordsOf is the words a line names, each once and each a word.
func wordsOf(field string, written []string) ([]string, error) {
	words := make([]string, 0, len(written))
	for _, w := range written {
		word := strings.TrimSpace(w)
		if word == "" || strings.ContainsAny(word, " \t\n") {
			return nil, errors.Newf("%s names %q, which is no word", field, w)
		}
		if word == PredicateWordsRevoked {
			return nil, errors.Newf("%s names %s, which takes words away, and am.toml takes away by striking the line", field, word)
		}
		if slices.Contains(words, word) {
			return nil, errors.Newf("%s names %s twice", field, word)
		}
		words = append(words, word)
	}
	return words, nil
}

// declaredWordLines is auth.words as the word lines ROOT would have written.
func declaredWordLines(listed []DeclaredWords, root string, at time.Time) ([]WordLine, error) {
	var lines []WordLine
	for i, said := range listed {
		field := "auth.words line " + strconv.Itoa(i+1)
		roles, err := rolesOf(field+", for the roles it is of,", said.Of)
		if err != nil {
			return nil, err
		}
		if len(roles) == 0 {
			return nil, errors.Newf("%s is of no role", field)
		}
		write, err := wordsOf(field+", for what it writes,", said.Write)
		if err != nil {
			return nil, err
		}
		read, err := wordsOf(field+", for what it reads,", said.Read)
		if err != nil {
			return nil, err
		}
		if len(write) == 0 && len(read) == 0 {
			return nil, errors.Newf("%s names no word to write or read", field)
		}
		if said.All && len(read) == 0 {
			return nil, errors.Newf("%s says all, which widens a READ line, and reads nothing", field)
		}
		if len(write) > 0 {
			lines = append(lines, WordLine{Write: true, Words: write, Roles: roles, Actor: root, At: at})
		}
		if len(read) > 0 {
			lines = append(lines, WordLine{Words: read, Roles: roles, All: said.All, Actor: root, At: at})
		}
	}
	return lines, nil
}

// 「[Opus 5.5] SetGrants holds the grants auth.grants lists in place of those it listed」
// 「[Opus 5.5] before: each a live grant for its key while its line stands. A list that」
// 「[Opus 5.5] does not hold leaves the grants held before as they were.」
//
// 「[Opus 5.5] A grant is ATTESTOR in its namespaces, as a key that signs for its person」
// 「[Opus 5.5] is. What it writes is signed by the key itself, and what it may write is」
// 「[Opus 5.5] what the lines of roles its label holds say. It speaks for whoever am.toml」
// 「[Opus 5.5] is ROOT's, as the Node Agent does: a token speaks for whoever minted it,」
// 「[Opus 5.5] and striking them out of am.toml reaches it too.」
//
// The roles a grant names and the lines auth.words lists are held beside them,
// as lines by that same ROOT, said when am.toml was read: the store's lines are
// read with them, and a line ROOT writes in the store later outranks them as
// any later line of ROOT's does (ADR-034).
func (h *Handler) SetGrants(listed []DeclaredGrant, words []DeclaredWords) error {
	minted, err := h.mintedLabelsAndDIDs()
	if err != nil {
		return err
	}
	roots := h.identities.roots()
	if len(listed) > 0 && len(roots) == 0 {
		return errors.New("auth.root_identities names nobody, so there is nobody for auth.grants to speak for")
	}
	if len(words) > 0 && len(roots) == 0 {
		return errors.New("auth.root_identities names nobody, so there is nobody for auth.words to be said by")
	}
	saidAt := time.Now()
	var root string
	if len(roots) > 0 {
		root = roots[0]
	}
	lines := declaredLines{byNamespace: map[string][]RoleLine{}}
	if lines.words, err = declaredWordLines(words, root, saidAt); err != nil {
		return err
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
		// 「[Opus 5.5] A grant acting in no namespace would admit its key to nothing.」
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
		roles, err := rolesOf("auth.grants, for the roles "+label+" holds,", grant.Roles)
		if err != nil {
			return err
		}
		if len(roles) > 0 {
			for _, namespace := range acts {
				ns := strings.ToLower(namespace)
				lines.byNamespace[ns] = append(lines.byNamespace[ns],
					RoleLine{Routes: []string{label}, Roles: roles, Granted: true, Actor: root, At: saidAt})
			}
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
	h.declaredRoles.Store(&lines)
	return nil
}

// declaredRoleLines is the grant lines am.toml says in namespace.
func (h *Handler) declaredRoleLines(namespace string) []RoleLine {
	lines := h.declaredRoles.Load()
	if lines == nil {
		return nil
	}
	return lines.byNamespace[strings.ToLower(namespace)]
}

// declaredWordsHeld is the word lines am.toml says.
func (h *Handler) declaredWordsHeld() []WordLine {
	lines := h.declaredRoles.Load()
	if lines == nil {
		return nil
	}
	return lines.words
}

// 「[Opus 5.5] declaredGrant is the grant auth.grants lists for did, and whether it lists one.」
func (h *Handler) declaredGrant(did string) (TokenInfo, bool) {
	held := h.declared.Load()
	if held == nil {
		return TokenInfo{}, false
	}
	grant, listed := (*held)[did]
	return grant, listed
}

// 「[Opus 5.5] declaresLabel is whether auth.grants lists a grant called label.」
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

// 「[Opus 5.5] mintedLabelsAndDIDs is every label and key a token minted here holds,」
// 「[Opus 5.5] revoked ones among them: revocation is a switch, and a switched-off token」
// 「[Fable 5.1] comes back.」
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
