package access

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
	// LevelUser is the normal user. It reaches no store and sees no system on
	// its own, and belongs to no door.
	LevelUser Level = "USER"
	// LevelOAuth is a kind and not a rung. A client is a door (ADR-025): an
	// app the node lets in on a person's say-so. Its DID is the client id and
	// its raw value the client secret. It authenticates at the token endpoint
	// and nowhere else, so no reach line names it and the middleware refuses
	// it as a bearer.
	LevelOAuth Level = "OAUTH"
	// LevelRefresh is a kind and not a rung. A refresh token is written down
	// the way every token is, so it survives a restart and revocation reaches
	// it — but it is spent at the token endpoint for a new access token and
	// never presented to a route. No reach line names it and the middleware
	// refuses it as a bearer.
	LevelRefresh Level = "REFRESH"
	// LevelGitHub is the GitHub token a namespace spends (ADR-043), a kind and not a rung.
	LevelGitHub Level = "GITHUB"
)
