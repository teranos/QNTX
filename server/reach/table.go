// Package reach is who reaches what, and the only way a route is served.

// The mux is in here and never leaves. Open hands back an http.Handler, so no
// caller holds a thing a route can be put on, and no caller can make a grant.
package reach

import (
	"slices"
	"strings"

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
REACH is '/auth/user/arrival' '/auth/user/arrive'                         of ANYONE

# A node nobody owns has nothing to protect but the door, and seeing the ways
# in is not passing through one.
REACH is '/setup' '/setup/claim'                                          of ANYONE

# Minting is ROOT handing a credential to a machine. It was the one route a
# public registration could reach that let it name its own level.
REACH is '/auth/tokens' '/auth/tokens/'                                   of ROOT

REACH is '/api/attestations'                                              of ROOT SUPER TOKEN ATTESTOR
REACH is '/api/namespaces'                                                of ROOT SUPER
REACH is '/api/doors/draft' '/api/doors/standing'                        of ROOT

REACH is '/ws' '/ws/llm'                                                  of ROOT
REACH is '/api/version'                                                   of ROOT
REACH is '/logs/download'                                                 of ROOT
REACH is '/api/timeseries/usage'                                          of ROOT
REACH is '/api/config'                                                    of ROOT
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
REACH is '/statusline' '/statusline/'                                     of ROOT SUPER
REACH is '/api/types' '/api/types/'                                       of ROOT
REACH is '/api/watchers' '/api/watchers/'                                 of ROOT
REACH is '/api/watchers/queue/stats'                                      of ROOT
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
			rows[route] = aRow{anyone: open, reach: auth.Also(also...)}
		}
	}
	return rows, nil
}
