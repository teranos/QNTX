// Package reach is who reaches what, and the only way a route is served.

// The mux is in here and never leaves. Open hands back an http.Handler, so no
// caller holds a thing a route can be put on, and no caller can make a grant.
package reach

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// Each line is an `as`. REACH is what the line is about, the quoted paths are
// what is said of it, and the levels are the context it holds in.

// Several paths share a line because predicates are a list, and so are the
// levels.

// ANYONE is not a level anybody holds. It means the route is served without
// asking who is calling.
const reachTable = `
# The model is: root can do anything, and everyone else cannot unless explicitly decided.
# No fork — root gets everything, everyone else does not. Undeclared should not mean
# everyone. Not defined is no access. And this should not be something you can forget.

REACH is '/' '/health' '/.well-known/did.json'                            of ANYONE

# Logging in cannot ask you to be logged in.
REACH is '/auth/login' '/auth/status'                                     of ANYONE
REACH is '/auth/login/begin' '/auth/login/finish'                         of ANYONE
REACH is '/auth/register/begin' '/auth/register/finish'                   of ANYONE
REACH is '/auth/logout'                                                   of ANYONE
REACH is '/auth/forget' '/auth/forget/begin'                              of ANYONE
REACH is '/auth/laye/challenge' '/auth/laye/verify'                       of ANYONE
REACH is '/auth/binding/providers' '/auth/binding/start'                  of ANYONE
REACH is '/auth/binding/go' '/auth/binding/callback'                      of ANYONE
REACH is '/auth/binding/result'                                           of ANYONE
REACH is '/auth/door/home' '/auth/door/home/result'                       of ANYONE
# Who sent the person home, answered from the ticket they hold, so the face
# can name a client. A stranger holding no ticket is told nothing.
REACH is '/auth/door/journey'                                             of ANYONE
# A client is a door (ADR-025): it sends a stranger here for the passkey, and
# the code goes back by ticket. Both are the way home, for a client.
REACH is '/auth/authorize' '/auth/authorize/done'                         of ANYONE
# The client comes for its token with the code and its secret, holding no
# session: the secret is the credential, checked at the endpoint itself.
REACH is '/auth/token'                                                    of ANYONE
# Discovery documents (RFC 8414, RFC 9728): how a client that has never seen
# this node finds its doors without being configured by hand. Read before it
# sends anybody anywhere, so by a stranger.
REACH is '/.well-known/oauth-authorization-server'                        of ANYONE
REACH is '/.well-known/oauth-protected-resource'                          of ANYONE
REACH is '/auth/user/arrival' '/auth/user/arrive'                         of ANYONE

# The switch on the person (ADR-031). Gated by the handler and not by this
# line, so a caller with no session gets 403 from there: a person who is off is
# admitted at no gate, and has to reach this to turn themselves back on.
REACH is '/i/disable' '/i/enable'                                         of ANYONE

# ⍟'s own paths: who you are, and where you stand.
REACH is '/i/'                                                            of ROOT SUPER TOKEN ATTESTOR PUBLIC_REGISTRATION
REACH is '/i/standing'                                                    of ROOT SUPER TOKEN ATTESTOR PUBLIC_REGISTRATION

# First-time setup: the ways in this node offers, and claiming it.
REACH is '/setup' '/setup/claim'                                          of ANYONE

# A staand is a market's public receive point (ADR-035).
REACH is '/s/'                                                            of ANYONE

# An element module is UI, and a page importing it carries no session. Every
# call the module goes on to make is gated on its own line above.
REACH is '/g/'                                                            of ANYONE

# Tokens (ADR-025) and Users (ADR-031). A write on either is a session's
# alone, gated by the handler and not by this line.
REACH is '/auth/tokens' '/auth/tokens/'                                   of ROOT SUPER
REACH is '/auth/users' '/auth/users/'                                     of ROOT SUPER

REACH is '/api/attestations'                                              of ROOT SUPER TOKEN ATTESTOR
# The standing guard is the handler's, not this line's.
REACH is '/api/namespaces' '/api/namespaces/'                             of ROOT SUPER

# The lines the gate reads (ADR-034). Writing one is gated at the
# attestation handler, not by this line.
REACH is '/api/roles'                                                     of ROOT SUPER

# Who reaches what at runtime, granted and revoked (the reach signum).
REACH is '/api/reach'                                                     of ROOT

# What the node serves, as MCP tools (ADR-038). A connector's token is the
# person who said yes, and every tool call is gated on its own path's line.
REACH is '/mcp' '/mcp/'                                                   of ROOT SUPER

# A line that names a signum is about its sigils, over every surface, whatever
# path the plugin bound them to (ReachingSigil).
REACH is 'datapunt'                                                       of ROOT SUPER

# A longer path wins over the prefix above, so widening that line does not
# widen this one.
REACH is '/api/namespaces/default/nuke'                                   of ROOT

# Stands (ADR-035) and what arrives at them (ADR-036).
REACH is '/api/staands'                                                   of ROOT SUPER
REACH is '/api/staands/metrics' '/api/staands/visits'                     of ROOT SUPER
REACH is '/api/staands/activity'                                          of ROOT SUPER

# What arrives on a socket is gated per attestation by mayRead, not here.
REACH is '/ws' '/ws/llm'                                                  of ROOT SUPER
REACH is '/am/version'                                                    of ROOT SUPER
REACH is '/am/syscap'                                                     of ROOT

# Generated by make openapi and held to this table by a test, so a line added
# here without regenerating fails the build.
REACH is '/openapi.json'                                                  of ROOT SUPER
REACH is '/logs/download'                                                 of ROOT
REACH is '/api/timeseries/usage'                                          of ROOT SUPER
# What the record cost, read back from where the numbers were sent. The node
# signs the question with a credential that reads the whole organisation, so
# this is not a line to open wider than the two who already see the box.
REACH is '/api/db/series'                                                 of ROOT SUPER
REACH is '/api/dev' '/api/debug' '/api/crash-test'                        of ROOT
REACH is '/api/prose' '/api/prose/'                                       of ROOT
REACH is '/api/pulse/executions/'                                         of ROOT SUPER
REACH is '/api/pulse/schedules' '/api/pulse/schedules/'                   of ROOT SUPER
REACH is '/api/pulse/jobs' '/api/pulse/jobs/'                             of ROOT SUPER
REACH is '/api/prompt/'                                                   of ROOT
REACH is '/api/plugins' '/api/plugins/'                                   of ROOT SUPER
REACH is '/api/plugins/elements' '/api/plugins/routes'                      of ROOT
REACH is '/api/plugins/{name}/logs'                                       of ROOT
REACH is '/api/plugins/{name}/config'                                     of ROOT
REACH is '/am/statusline' '/am/statusline/'                               of ROOT SUPER
REACH is '/api/types' '/api/types/'                                       of ROOT
# A watcher acts inside a namespace, and the standing table is on these paths
# too. A watcher nobody may read is one that fires unseen.
REACH is '/api/watchers' '/api/watchers/'                                 of ROOT SUPER
REACH is '/api/watchers/queue/stats'                                      of ROOT SUPER
REACH is '/api/element-config'                                              of ROOT
REACH is '/api/canvas/elements' '/api/canvas/elements/'                       of ROOT
REACH is '/api/canvas/compositions' '/api/canvas/compositions/'           of ROOT
REACH is '/api/canvas/minimized-windows'                                  of ROOT SUPER
REACH is '/api/canvas/minimized-windows/'                                 of ROOT SUPER
REACH is '/api/canvas/export' '/api/canvas/export-dom'                    of ROOT
REACH is '/api/files' '/api/files/'                                       of ROOT
REACH is '/api/python/execute'                                            of ROOT
REACH is '/api/search/semantic'                                           of ROOT
REACH is '/api/embeddings/generate' '/api/embeddings/batch'               of ROOT
REACH is '/api/embeddings/clusters'                                       of ROOT
REACH is '/api/embeddings/clusters/samples'                               of ROOT
REACH is '/api/embeddings/clusters/members'                               of ROOT
REACH is '/api/embeddings/clusters/memberships'                           of ROOT
REACH is '/api/embeddings/cluster' '/api/embeddings/by-source'            of ROOT
REACH is '/api/embeddings/cluster-timeline'                               of ROOT
REACH is '/api/embeddings/info' '/api/embeddings/unembedded'              of ROOT
REACH is '/api/embeddings/project' '/api/embeddings/projections'          of ROOT
`

// reachSubject is what every line here is about. A line about anything else is
// in the wrong file.
const reachSubject = "REACH"

// Subject is the same word for the store: a runtime line is about REACH too.
const Subject = reachSubject

// Paths is every path the const table names, for a caller that has to answer
// on all of them before Open will build. The table is the node's; a caller
// learns its paths and not its grants.
func Paths() []string {
	rows, err := readReaches(reachTable)
	if err != nil {
		// The table is a const and TestTheTableReads holds it readable; a
		// node with an unreadable table never gets past Open.
		return nil
	}
	return sorted(routesIn(rows))
}

// Reached is who reaches each path the const table names, as the line wrote
// it: the levels, or ANYONE.
//
// Paths is what a caller gets at runtime — the table is the node's, and a
// caller learns its paths and not its grants. This is for the document
// generator in cmd/openapi, which runs over the source before there is a node.
// It reads through the same parser the mux does, so a document cannot say
// something the mux does not do.
func Reached() (map[string][]string, error) {
	rows, err := readReaches(reachTable)
	if err != nil {
		return nil, err
	}
	reached := map[string][]string{}
	for path, row := range routesIn(rows) {
		reached[path] = slices.Clone(row.named)
	}
	return reached, nil
}

// anyone is the context for a route served without asking who is calling.
const anyone auth.Level = "ANYONE"

// levels is every context a line may name. Anything else is a typo, and a typo
// that parsed would quietly widen or narrow a route.
var levels = map[auth.Level]bool{
	anyone:                       true,
	auth.LevelRoot:               true,
	auth.LevelSuper:              true,
	auth.LevelToken:              true,
	auth.LevelAttestor:           true,
	auth.LevelPublicRegistration: true,
}

// runtimeLevels is every level a runtime line may name, and only on a plugin's
// routes: the node's own paths stay the const table's, which is static.
var runtimeLevels = map[auth.Level]bool{
	auth.LevelPublicRegistration: true,
	anyone:                       true,
}

// aRow is what one line says about one route.
type aRow struct {
	anyone bool
	reach  auth.Reach
	// named is the contexts as the line wrote them, ROOT and ANYONE included.
	// The mux does not read it — reach is what admits — and Reached hands it to
	// a document that says what the node serves.
	named []string
}

// A Line is what a runtime reach line says, read out of the store. Same shape
// as a line in the const: REACH, the paths, and who reaches them. Who is a
// role, or PUBLIC_REGISTRATION or ANYONE on a plugin's path: the const is the floor.

// By is who may grant those roles, as written after `by`. Kept on the row and
// not yet asked (phase 4 of #899).
type Line struct {
	Paths []string
	Roles []string
	By    []string
	// Revoked is a line that takes its pairs away rather than giving them:
	// reach:revoked stood beside the paths.
	Revoked bool
	// Actor is who wrote the line: the node put it first among the actors.
	Actor string
	At    time.Time
}

// Runtime is the store's lines and the one question about them the reach
// package cannot answer on its own: whether an actor is ROOT. A ROOT line for
// a path beats every other actor's whatever the clock says; among equals the
// latest wins. The same rule as a grant.
type Runtime struct {
	Lines  []Line
	IsRoot func(actor string) bool
	// Plugin is whether a path a line names is a plugin's route, the only kind
	// a runtime line may open to a level. Nil is a node with no plugins.
	Plugin func(path string) bool
}

// Unopenable is every path this line would open to a level that is not a
// plugin's route, or that the compiled-in table names: runtime never
// supersedes it. The write is refused over them.
func (l Line) Unopenable(plugin func(path string) bool) []string {
	// Taking back opens nothing, so it is never refused.
	if l.Revoked {
		return nil
	}
	var level bool
	for _, role := range l.Roles {
		level = level || runtimeLevels[auth.Level(role)]
	}
	if !level {
		return nil
	}
	compiled, err := readReaches(reachTable)
	if err != nil {
		// TestTheTableReads holds the const readable; unreadable, nothing opens.
		return slices.Clone(l.Paths)
	}
	var outside []string
	for _, path := range l.Paths {
		if !opensAtRuntime(path, compiled, plugin) {
			outside = append(outside, path)
		}
	}
	return outside
}

// opensAtRuntime is whether a runtime line may open a path to a level: a
// plugin's route that the compiled-in table does not name.
func opensAtRuntime(path string, compiled map[string]aRow, plugin func(path string) bool) bool {
	_, named := compiled[path]
	return !named && plugin != nil && plugin(path)
}

// ReadLine reads a stored attestation as a runtime reach line, refusing what
// the const would refuse the other way round: a level or ANYONE as a context.
// The write path asks this before a line is stored; the read path asks it
// again, because a line in the store is not a line the node has to serve.
func ReadLine(subjects, predicates, contexts, actors []string, at time.Time) (Line, error) {
	if len(subjects) != 1 || strings.ToUpper(subjects[0]) != reachSubject {
		return Line{}, errors.Newf("a reach line is about %s, and this one is about %v", reachSubject, subjects)
	}
	line := Line{At: at}
	for _, predicate := range predicates {
		if predicate == auth.PredicateReachRevoked {
			line.Revoked = true
			continue
		}
		line.Paths = append(line.Paths, predicate)
	}
	if len(line.Paths) == 0 {
		return Line{}, errors.New("a reach line names no path")
	}
	if len(contexts) == 0 {
		return Line{}, errors.New("a reach line names no role")
	}
	for _, named := range contexts {
		role := strings.ToUpper(named)
		if levels[auth.Level(role)] && !runtimeLevels[auth.Level(role)] {
			return Line{}, errors.Newf("a runtime line names %s, and only the const table names that level", named)
		}
		line.Roles = append(line.Roles, role)
	}
	if len(actors) > 0 {
		line.Actor = actors[0]
		line.By = actors[1:]
	}
	return line, nil
}

// addRuntime lays the store's lines over the const rows. Lines settle per
// pair, a path with a role: the latest line about a pair wins, ROOT first, and
// a line about a different pair is untouched. A pair whose winning line is
// revoked is gone; the rest reach. A path the const names keeps its levels
// and gains the roles.
//
// The second return is who may grant each role: what the winning lines said
// after `by`, per role, from every pair that role still holds.
func addRuntime(rows map[string]aRow, runtime Runtime) map[string][]string {
	granters := map[string][]string{}
	// What the compiled-in table said, before any runtime line is laid over it.
	compiled := maps.Clone(rows)
	for at, line := range winning(runtime) {
		if line.Revoked {
			continue
		}
		row := rows[at.path]
		// A level is added as a level, never as a role of that name, and only on
		// a plugin's route. Nobody grants a level, so no granter is kept for it.
		if level := auth.Level(at.role); runtimeLevels[level] {
			if opensAtRuntime(at.path, compiled, runtime.Plugin) {
				if level == anyone {
					row.anyone = true
				} else {
					row.reach = row.reach.With(auth.Also(level))
				}
				rows[at.path] = row
			}
			continue
		}
		if !slices.Contains(row.reach.Roles(), at.role) {
			row.reach = row.reach.AndRoles(at.role)
		}
		rows[at.path] = row
		for _, by := range line.By {
			by = strings.ToUpper(by)
			if !slices.Contains(granters[at.role], by) {
				granters[at.role] = append(granters[at.role], by)
			}
		}
	}
	return granters
}

// pair is what one reach line says one thing about: a path, for a role.
type pair struct {
	path string
	role string
}

// winning is the one line that holds per pair.
func winning(runtime Runtime) map[pair]Line {
	won := map[pair]Line{}
	for _, line := range runtime.Lines {
		for _, path := range line.Paths {
			for _, role := range line.Roles {
				at := pair{path: path, role: role}
				standing, seen := won[at]
				if !seen || outranks(line, standing, runtime.IsRoot) {
					won[at] = line
				}
			}
		}
	}
	return won
}

// outranks is how two runtime lines about one path are settled: ROOT first,
// then the latest. Two lines at the same instant settle for the one seen
// first, which is the store's order.
func outranks(line, standing Line, isRoot func(string) bool) bool {
	root := isRoot != nil && isRoot(line.Actor)
	standingRoot := isRoot != nil && isRoot(standing.Actor)
	if root != standingRoot {
		return root
	}
	return line.At.After(standing.At)
}

// readReaches parses the table through the same parser the CLI uses, so the
// file is the notation rather than something shaped like it.

// A line that does not read is a lie about what the node serves, so it is an
// error and not a skipped line.
func readReaches(table string) (map[string]aRow, error) {
	rows := map[string]aRow{}
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}

		// `by` is who may set a grant and within what bounds. Nothing reads it
		// yet, and the parser fills an actor in from whoever is running the
		// process — so a line using it would parse and mean nothing.
		if slices.Contains(fields, "by") {
			return nil, errors.Newf("%q names an actor, and nothing here reads one yet", line)
		}

		said, err := parser.ParseAsCommand(fields)
		if err != nil {
			return nil, errors.Wrapf(err, "%q does not read as an attestation", line)
		}
		// Actors are filled in by the parser from whoever is running the
		// process. This file says nothing about who; it says what reaches.
		if len(said.Subjects) != 1 || said.Subjects[0] != reachSubject {
			return nil, errors.Newf("%q is about %v, and this file is about %s",
				line, said.Subjects, reachSubject)
		}
		if len(said.Predicates) == 0 {
			return nil, errors.Newf("%q names no path", line)
		}
		if len(said.Contexts) == 0 {
			return nil, errors.Newf("%q names no level", line)
		}

		var open bool
		var also []auth.Level
		for _, named := range said.Contexts {
			level := auth.Level(named)
			if !levels[level] {
				return nil, errors.Newf("%q names %s, which is not a level", line, named)
			}
			switch level {
			case anyone:
				open = true
			case auth.LevelRoot:
				// Never listed on a Reach. ROOT reaches everything.
			default:
				also = append(also, level)
			}
		}

		// The paths are predicates, which the parser leaves as written. A
		// subject is canonicalised to upper case and a path is not:
		// /.well-known/did.json is not /.WELL-KNOWN/DID.JSON.
		for _, route := range said.Predicates {
			if _, twice := rows[route]; twice {
				return nil, errors.Newf("%s is in the table twice", route)
			}
			rows[route] = aRow{anyone: open, reach: auth.Also(also...), named: said.Contexts}
		}
	}
	return rows, nil
}
