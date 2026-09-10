// Package reach is who reaches what, and the only way a route is served.

// The mux is in here and never leaves. Open hands back an http.Handler, so no
// caller holds a thing a route can be put on, and no caller can make a grant.
package reach

import (
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

# Logging in cannot ask you to be logged in. Every one of these is a stranger
# at the door, and saying so is a grant like any other.
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
REACH is '/auth/user/arrival' '/auth/user/arrive'                         of ANYONE

# The switch on the person (ADR-031). Session-gated by the handler: a person
# who is off is admitted at no gate, and has to reach this to turn back on.
REACH is '/i/disable' '/i/enable'                                         of ANYONE

# Who the node thinks you are, answered to you and to nobody about anybody
# else. Every rung that can be logged in is named, because being logged in is
# the whole of what it asks — a stranger gets this table's refusal instead.
REACH is '/i/'                                                            of ROOT SUPER TOKEN ATTESTOR PUBLIC_REGISTRATION

# A node nobody owns has nothing to protect but the door, and seeing the ways
# in is not passing through one.
REACH is '/setup' '/setup/claim'                                          of ANYONE

# A staand is a market's public receive point (ADR-035). The pixel answers
# anyone; default-deny still means only a raised (namespace, slug) records.
REACH is '/s/'                                                            of ANYONE

# A glyph module is UI, and UI is not a boundary — every call it makes is
# gated here against whoever made it. A page imports it, and an import carries
# no session, so asking for one would refuse every reader including its own
# node. What is served is what was published; a glyph not yet public is an
# attestation this route does not read.
REACH is '/g/'                                                            of ANYONE

# Minting is ROOT handing a credential to a machine. It was the one route a
# public registration could reach that let it name its own level.
REACH is '/auth/tokens' '/auth/tokens/'                                   of ROOT

# ROOT over every User (ADR-031): the list, and the switch on each of them.
REACH is '/auth/users' '/auth/users/'                                     of ROOT

REACH is '/api/attestations'                                              of ROOT SUPER TOKEN ATTESTOR
REACH is '/api/namespaces'                                                of ROOT SUPER

# The stands glyph lists, creates and deletes stands (ADR-035). ROOT and a SUPER
# token both reach it; the definition lands in system either way.
REACH is '/api/staands'                                                   of ROOT SUPER

# A breakdown reads one stand's arrivals grouped by one dimension (ADR-036). It
# reads what the list above already reads, so it reaches no further.
REACH is '/api/staands/metrics' '/api/staands/visits'                     of ROOT SUPER
REACH is '/api/staands/activity'                                          of ROOT SUPER

REACH is '/ws' '/ws/llm'                                                  of ROOT
# What build is running. SUPER reads it for the same reason it reads the route
# list: a caller operating the node is not who this was kept from. syscap stays
# ROOT's — what a binary was built with is a different question from what it is.
REACH is '/am/version'                                                    of ROOT SUPER
REACH is '/am/syscap'                                                     of ROOT

# What the node serves, in the form a machine reads. SUPER reads it because
# SUPER is ROOT handing its own reach to a token it made (ADR-027), and a caller
# that may create a namespace and list the plugins is not who this was kept from.
REACH is '/openapi.json'                                                  of ROOT SUPER
REACH is '/logs/download'                                                 of ROOT
REACH is '/api/timeseries/usage'                                          of ROOT
REACH is '/am/config'                                                     of ROOT
REACH is '/api/dev' '/api/debug' '/api/crash-test'                        of ROOT
REACH is '/api/prose' '/api/prose/'                                       of ROOT
REACH is '/api/pulse/executions/'                                         of ROOT
REACH is '/api/pulse/schedules' '/api/pulse/schedules/'                   of ROOT
REACH is '/api/pulse/jobs' '/api/pulse/jobs/'                             of ROOT
REACH is '/api/prompt/'                                                   of ROOT
REACH is '/api/plugins' '/api/plugins/'                                   of ROOT SUPER
REACH is '/api/plugins/glyphs' '/api/plugins/routes'                      of ROOT
REACH is '/api/plugins/{name}/logs'                                       of ROOT
REACH is '/api/plugins/{name}/config'                                     of ROOT
REACH is '/am/statusline' '/am/statusline/'                               of ROOT SUPER
REACH is '/api/types' '/api/types/'                                       of ROOT
# A watcher acts inside a namespace and SUPER is what crosses them, so what a
# node watches is not what was being kept from it. The standing table is here
# too, and a watcher nobody may read is one that fires unseen.
REACH is '/api/watchers' '/api/watchers/'                                 of ROOT SUPER
REACH is '/api/watchers/queue/stats'                                      of ROOT SUPER
REACH is '/api/glyph-config'                                              of ROOT
REACH is '/api/canvas/glyphs' '/api/canvas/glyphs/'                       of ROOT
REACH is '/api/canvas/compositions' '/api/canvas/compositions/'           of ROOT
REACH is '/api/canvas/minimized-windows'                                  of ROOT
REACH is '/api/canvas/minimized-windows/'                                 of ROOT
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
	return sorted(rows)
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
	for path, row := range rows {
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
// as a line in the const: REACH, the paths, and who reaches them — except who
// is a role and not a level, because the const is the floor and a line written
// at runtime adds roles only.

// By is who may grant those roles, as written after `by`. Kept on the row and
// not yet asked (phase 4 of #899).
type Line struct {
	Paths []string
	Roles []string
	By    []string
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
}

// ReadLine reads a stored attestation as a runtime reach line, refusing what
// the const would refuse the other way round: a level or ANYONE as a context.
// The write path asks this before a line is stored; the read path asks it
// again, because a line in the store is not a line the node has to serve.
func ReadLine(subjects, predicates, contexts, actors []string, at time.Time) (Line, error) {
	if len(subjects) != 1 || strings.ToUpper(subjects[0]) != reachSubject {
		return Line{}, errors.Newf("a reach line is about %s, and this one is about %v", reachSubject, subjects)
	}
	if len(predicates) == 0 {
		return Line{}, errors.New("a reach line names no path")
	}
	if len(contexts) == 0 {
		return Line{}, errors.New("a reach line names no role")
	}
	line := Line{Paths: predicates, At: at}
	for _, named := range contexts {
		role := strings.ToUpper(named)
		if levels[auth.Level(role)] {
			return Line{}, errors.Newf("a runtime line names %s, and only the const table names a level", named)
		}
		line.Roles = append(line.Roles, role)
	}
	if len(actors) > 0 {
		line.Actor = actors[0]
		line.By = actors[1:]
	}
	return line, nil
}

// addRuntime lays the store's lines over the const rows. Per path, the winning
// line is the whole truth of which roles reach it: a newer line supersedes an
// older one, and no line is ever taken back by a word. A path the const names
// keeps its levels and gains the roles.
//
// The second return is who may grant each role: what the winning lines said
// after `by`, per role, from every path that role reaches.
func addRuntime(rows map[string]aRow, runtime Runtime) map[string][]string {
	won := map[string]Line{}
	for _, line := range runtime.Lines {
		for _, path := range line.Paths {
			standing, seen := won[path]
			if !seen || outranks(line, standing, runtime.IsRoot) {
				won[path] = line
			}
		}
	}
	granters := map[string][]string{}
	for path, line := range won {
		row := rows[path]
		row.reach = row.reach.AndRoles(line.Roles...)
		rows[path] = row
		for _, role := range line.Roles {
			for _, by := range line.By {
				by = strings.ToUpper(by)
				if !slices.Contains(granters[role], by) {
					granters[role] = append(granters[role], by)
				}
			}
		}
	}
	return granters
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
