package auth

import (
	"slices"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// RoleLine is one thing said about a role, read back out of the store.

// It is an attestation and nothing else: the subjects are the routes it is
// about, the role is a predicate beside the one that says granted or revoked,
// and the actor is who said it. The namespace is the context, and is not on
// the line because a reader asks for one namespace at a time.
type RoleLine struct {
	// Routes are the line's subjects — accounts and did:keys, the way
	// auth.root_identities names people. A User reaches any number of them.
	Routes []string
	// Roles are the predicates that are not the granted/revoked one. A line
	// naming two roles says the same thing about both.
	Roles []string
	// Granted is which of the two predicates this line is. False is a revoke.
	Granted bool
	// Actor is the granter: the actor the node put on the line, which is
	// ROOT's own identity or the DID of a token ROOT minted.
	Actor string
	At    time.Time
}

// RoleReader is the read half the Attestor is not.

// Attestor records and never reads back, on purpose. This is the other half,
// deliberately a second interface: it is implemented against the system store
// outside this package, so nothing in here can hold a second answer about who
// may do what.
type RoleReader interface {
	// RoleLines is every granted and revoked line written in one namespace.
	RoleLines(namespace string) ([]RoleLine, error)
}

// SetRoleReader hands the handler somewhere to read grants from, the way
// SetAttestor hands it somewhere to write. Nil reads nothing, which is a node
// where nobody holds a role rather than a node that fails.
func (h *Handler) SetRoleReader(r RoleReader) {
	h.roles = r
}

// heldRoles is what has been read, per namespace.

// The node is the only writer of these lines, so what is here cannot go stale
// behind its back: a write drops the whole of it. No ticker, nothing expires.
type heldRoles struct {
	mu          sync.Mutex
	byNamespace map[string][]RoleLine
}

// ForgetRoles drops every namespace's lines. Called where a grant is written
// and nowhere else.
func (h *Handler) ForgetRoles() {
	h.held.mu.Lock()
	defer h.held.mu.Unlock()
	h.held.byNamespace = nil
}

// roleLines is the namespace's lines, read the first time they are asked for.
func (h *Handler) roleLines(namespace string) []RoleLine {
	if h.roles == nil {
		return nil
	}
	h.held.mu.Lock()
	defer h.held.mu.Unlock()
	if lines, ok := h.held.byNamespace[namespace]; ok {
		return lines
	}
	lines, err := h.roles.RoleLines(namespace)
	if err != nil {
		// Not cached: a read that failed is not an answer, and caching it
		// would make one store hiccup mean nobody holds anything until a write.
		h.logger.Errorw("could not read the roles held in a namespace",
			"namespace", namespace, "error", err)
		return nil
	}
	if h.held.byNamespace == nil {
		h.held.byNamespace = map[string][]RoleLine{}
	}
	h.held.byNamespace[namespace] = lines
	return lines
}

// RolesOf is every role a User holds in one namespace.

// A person is reached by any number of routes (ADR-030) and a grant names one
// of them, so the User is what a line is matched against rather than whichever
// route they happened to log in by.
func (h *Handler) RolesOf(u User, namespace string) []string {
	return h.rolesHeld(u.Reaches, namespace)
}

// RolesOfDID is every role one did:key holds in a namespace — a token's own
// DID, so a grant is one kind of line whether it names a person or a program.
func (h *Handler) RolesOfDID(did, namespace string) []string {
	if did == "" {
		return nil
	}
	return h.rolesHeld(func(route string) bool { return route == did }, namespace)
}

// roleClaim is one line's say about one role, reduced to what settles it.
type roleClaim struct {
	granted bool
	byRoot  bool
	at      time.Time
}

// outranks is the whole of how two claims about one role are settled.

// A ROOT claim beats every other actor whatever the clock says. Among equals
// the last claim in time wins. Two claims at the same instant settle as
// revoked: losing a revocation is worse than losing a grant.
//
// A token ROOT minted writes under its own DID (TOKATTEST), so its lines are
// among the other actors here and settle by time.
func (c roleClaim) outranks(held roleClaim) bool {
	if c.byRoot != held.byRoot {
		return c.byRoot
	}
	if c.at.Equal(held.at) {
		return !c.granted
	}
	return c.at.After(held.at)
}

// rolesHeld resolves every line a reacher matches into the roles that hold.
func (h *Handler) rolesHeld(reaches func(route string) bool, namespace string) []string {
	settled := map[string]roleClaim{}
	for _, line := range h.roleLines(namespace) {
		if !slices.ContainsFunc(line.Routes, reaches) {
			continue
		}
		// Whether the granter is ROOT is asked of am.toml now rather than
		// recorded then, so striking an account out takes its grants with it,
		// the way it takes its passkeys (binding.go).
		claim := roleClaim{granted: line.Granted, byRoot: h.levelOf(line.Actor) == LevelRoot, at: line.At}
		for _, role := range line.Roles {
			if standing, seen := settled[role]; seen && !claim.outranks(standing) {
				continue
			}
			settled[role] = claim
		}
	}

	held := make([]string, 0, len(settled))
	for role, claim := range settled {
		if claim.granted {
			held = append(held, role)
		}
	}
	// A map is not an order, and a caller reading two different orders for one
	// answer would think something changed.
	slices.Sort(held)
	return held
}

// MayGrantRoles reports whether an admission may write one of the two
// predicates.

// ROOT, and a token ROOT minted — which is how a line in a pbt becomes a
// grant (ADR-025). Everyone else is refused, including everyone who reaches
// /api/attestations for every other predicate.
func (h *Handler) MayGrantRoles(a Admission) bool {
	if a.Grant != nil {
		return h.levelOf(a.Grant.MintedBy) == LevelRoot
	}
	return a.level == LevelRoot
}

// RoleWritten names which of the two predicates a write is, and whether it is
// one at all. The handler asks before it decides anything else about the write.
func RoleWritten(predicates []string) (string, bool) {
	for _, predicate := range predicates {
		if predicate == PredicateRoleGranted || predicate == PredicateRoleRevoked {
			return predicate, true
		}
	}
	return "", false
}

// AsRoleLine reads a stored attestation as what it says about a role. False is
// an attestation that says nothing about one.

// The grammar is here rather than where the store is read, so there is one
// place that knows a role is the predicate beside the granted/revoked one.
func AsRoleLine(as *types.As) (RoleLine, bool) {
	granted := slices.Contains(as.Predicates, PredicateRoleGranted)
	revoked := slices.Contains(as.Predicates, PredicateRoleRevoked)
	// Neither is not a role line. Both is a line that grants and revokes the
	// same role at the same instant, which says nothing and is read as nothing.
	if granted == revoked {
		return RoleLine{}, false
	}

	line := RoleLine{Routes: as.Subjects, Granted: granted, At: as.Timestamp}
	for _, predicate := range as.Predicates {
		if predicate == PredicateRoleGranted || predicate == PredicateRoleRevoked {
			continue
		}
		line.Roles = append(line.Roles, predicate)
	}
	if len(line.Routes) == 0 || len(line.Roles) == 0 {
		return RoleLine{}, false
	}
	// The granter is the actor the node put on the line and nothing after it:
	// what a caller named beside it is theirs, and is not who granted this.
	if len(as.Actors) > 0 {
		line.Actor = as.Actors[0]
	}
	return line, true
}
