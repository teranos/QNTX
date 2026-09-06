package auth

import "context"

// The two namespaces a deployment always has (ADR-026). Every other namespace
// is created by SUPER, and neither of these can be deleted.
const (
	NamespaceSystem  = "system"
	NamespaceDefault = "default"
)

// Level is how much an admission may do (ADR-027). It says how much, never
// where.
type Level string

// The ladder is scope. ATTESTOR acts inside a namespace, SUPER crosses
// namespaces, ROOT goes beyond QNTX.
const (
	// LevelSuper crosses namespaces and creates or disables them.
	LevelSuper Level = "SUPER"
	// LevelRoot goes beyond QNTX — wanted on dev, not on prod.
	LevelRoot Level = "ROOT"
	// LevelToken is what a bearer token gets. It cannot mint tokens.
	LevelToken Level = "TOKEN"
	// LevelAttestor acts inside a namespace. A User is the human; this is what
	// they may do (ADR-031).
	LevelAttestor Level = "ATTESTOR"
	// LevelPublicRegistration is somebody who walked up to a door and made
	// themselves. Every other User the node holds was put there by somebody.
	// This rung logs in and is attested, and that is the whole of it.
	LevelPublicRegistration Level = "PUBLIC_REGISTRATION"
)

// Admission is what a request was granted at the door. Middleware resolves it
// once and every handler past that point reads it: Namespaces is where,
// Identity and UserID are who, Grant is which predicates.
type Admission struct {
	// level is how much. Unexported: server/reach is the only authority on
	// which levels reach a path, and a package that cannot read this cannot
	// hold a second answer.
	level Level
	// roles is what this admission holds in the namespace it acts in, read
	// from the lines ROOT wrote. Unexported for the same reason as level.
	roles []string
	// seesSystem is whether a role is held in system. A grant may name system
	// as its namespace, and a role held there sees it.
	seesSystem bool
	// words is what the held roles may read and write, from the READ and
	// WRITE lines ROOT wrote for them.
	words Words
	// Namespaces is where this admission may act. A session names the door the
	// person registered at (ADR-032); a token names what its record does. None
	// is every namespace the node serves, which is what a session that came in
	// by no door names.
	Namespaces []string
	// Identity is the auth.root_identities entry that admitted this request —
	// an account URL or a did:key. A token carries the identity that minted it.
	Identity string
	// UserID is who that identity reaches (ADR-031). A person holds several
	// routes and several keys, so the route says which door was used and this
	// says who walked through it.

	// Empty on a deployment that keeps no Users, and on a bearer token, which
	// names the route that minted it and has not been resolved past that.
	UserID string
	// DisplayName is what to call that person. The status line draws it, because
	// a route is a door rather than a name.
	DisplayName string
	// Grant is present only when a token made the request. It names the token's
	// own DID and the predicates it may touch, and nil means unrestricted —
	// which is what a passkey session is.
	Grant *Grant
}

// Admitted builds one. What a request holds is what Middleware resolved for
// it, so building one grants nothing.
func Admitted(level Level, namespaces ...string) Admission {
	return Admission{level: level, Namespaces: namespaces}
}

// Holding is an admission with roles on it, for a test standing in for
// Middleware. Nothing on the request path builds one this way.
func Holding(a Admission, roles ...string) Admission {
	a.roles = roles
	return a
}

// Saying is an admission with words on it, for the same tests.
func Saying(a Admission, words Words) Admission {
	a.words = words
	return a
}

// LevelName is the rung, for a log line or a row somebody reads. Comparing it
// is deciding reach, and reach is server/reach's — this is for writing down.
func (a Admission) LevelName() string {
	return string(a.level)
}

// ReachesAStore reports whether this admission holds a universe at all.
// Somebody who walked up to a door reaches none on their own: the rung buys
// logging in and being attested. A role held where they act is what reaches
// further, and it is a line ROOT wrote.
func (a Admission) ReachesAStore() bool {
	return a.level != LevelPublicRegistration || len(a.roles) > 0
}

// MaySeeSystem reports whether the system namespace is visible to this
// admission: SUPER and above (ADR-027), where Users and tokens live, or a
// role held in system itself.
func (a Admission) MaySeeSystem() bool {
	return a.level == LevelRoot || a.level == LevelSuper || a.seesSystem
}

// Roles is what this admission holds where it acts, for writing down. Reach
// is server/reach's, and the write gate's, and neither reads this.
func (a Admission) Roles() []string {
	return append([]string(nil), a.roles...)
}

// belowTheLadder reports whether this admission is decided by lines: an
// ATTESTOR token, or somebody who walked up to a door. ROOT, SUPER and a
// SUPER token stand above the lines and are narrowed by nothing.
func (a Admission) belowTheLadder() bool {
	if a.Grant != nil {
		return a.Grant.Scoped()
	}
	return a.level == LevelPublicRegistration
}

// MayRead reports whether this admission may read attestations with a
// predicate. "DEFAULT DENY": below the ladder, what the READ lines of the
// held roles name and nothing else. A role with no READ line reads nothing,
// and so does a token whose DID holds no role.
func (a Admission) MayRead(predicate string) bool {
	if a.belowTheLadder() {
		return permits(a.words.Read, predicate)
	}
	return true
}

// MayWrite reports whether this admission may write attestations with a
// predicate. The same shape as MayRead: a role with no WRITE line writes
// nothing.
func (a Admission) MayWrite(predicate string) bool {
	if a.belowTheLadder() {
		return permits(a.words.Write, predicate)
	}
	return true
}

// ReadScope is the predicates a read is narrowed to, and whether it is
// narrowed at all. A query through an admission above the ladder goes out as
// it came in.
func (a Admission) ReadScope() ([]string, bool) {
	if !a.belowTheLadder() {
		return nil, false
	}
	return append([]string(nil), a.words.Read...), true
}

// OwnOnly reports whether a read is narrowed to what this admission's own
// actor wrote. Below the ladder it is, unless a READ line for a held role
// says `all`: reading beyond your own rows is a word written down, not the
// absence of one.
func (a Admission) OwnOnly() bool {
	return a.belowTheLadder() && !a.words.All
}

// ActsAs is the actor the node puts first on everything this admission
// writes: a token's DID, or the route a person came in by. Empty is an
// admission that signs nothing, which is ROOT and everyone above the ladder
// writing without a role.
func (a Admission) ActsAs() string {
	if a.Grant != nil {
		return a.Grant.DID
	}
	if len(a.roles) > 0 {
		return a.Identity
	}
	return ""
}

// Words is what the roles an admission holds may say: the READ and WRITE
// lines of every held role, joined.
type Words struct {
	Read  []string
	Write []string
	// All is whether any READ line for a held role said `all`: reading
	// beyond your own rows. Without it a read below the ladder is own.
	All bool
}

type admissionKey struct{}

type admissionSinkKey struct{}

// WithAdmissionSink puts a slot in the context for what the request turns out
// to be granted. Middleware hands the admission down on a copy of the request,
// so a layer wrapped around it needs somewhere to have the answer written.
func WithAdmissionSink(ctx context.Context) (context.Context, *Admission) {
	sink := &Admission{}
	return context.WithValue(ctx, admissionSinkKey{}, sink), sink
}

// WithAdmission returns a context carrying the admission, and fills the sink
// when an outer layer left one.
func WithAdmission(ctx context.Context, admission Admission) context.Context {
	if sink, ok := ctx.Value(admissionSinkKey{}).(*Admission); ok {
		*sink = admission
	}
	return context.WithValue(ctx, admissionKey{}, admission)
}

// AdmissionFrom returns what the request was granted. False means the handler
// ran outside Middleware — a wiring mistake, not an anonymous request, since
// Middleware never calls through without authenticating.
func AdmissionFrom(ctx context.Context) (Admission, bool) {
	admission, ok := ctx.Value(admissionKey{}).(Admission)
	return admission, ok
}
