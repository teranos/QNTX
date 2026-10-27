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
	errors "github.com/teranos/sacred-error"
)

// reachSubject is what every line here is about. A line about anything else is
// in the wrong file.
const reachSubject = "REACH"

// Subject is the same word for the store: a runtime line is about REACH too.
const Subject = reachSubject

// Paths is every path the tablet names, for a caller that has to answer
// on all of them before Open will build. The tablet is the node's; a caller
// learns its paths and not its grants.
func Paths() []string {
	rows, err := readReaches(tablet)
	if err != nil {
		// TestTheTableReads holds QNTX's tablet readable; a node with an
		// unreadable tablet never gets past Open.
		return nil
	}
	return sorted(routesIn(rows))
}

// Reached is who reaches each path the tablet names, as the line wrote
// it: the levels, or ANYONE.
//
// Paths is what a caller gets at runtime — the table is the node's, and a
// caller learns its paths and not its grants. This is for the document
// generator in cmd/openapi, which runs over the source before there is a node.
// It reads through the same parser the mux does, so a document cannot say
// something the mux does not do.
func Reached() (map[string][]string, error) {
	rows, err := readReaches(tablet)
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
	auth.LevelUser:               true,
}

// runtimeLevels is every level a runtime line may name, and only on a plugin's
// routes and sigils: the node's own paths stay the tablet's, which is
// compiled in. ROOT is never named; ROOT reaches everything.
var runtimeLevels = map[auth.Level]bool{
	auth.LevelSuper:              true,
	auth.LevelToken:              true,
	auth.LevelAttestor:           true,
	auth.LevelPublicRegistration: true,
	auth.LevelUser:               true,
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
// as a line in the tablet: REACH, the paths, and who reaches them. Who is a
// role, or PUBLIC_REGISTRATION or ANYONE on a plugin's path: the tablet is the floor.

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
	compiled, err := readReaches(tablet)
	if err != nil {
		// TestTheTableReads holds QNTX's tablet readable; unreadable, nothing opens.
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
// the tablet would refuse the other way round: a level or ANYONE as a context.
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
			return Line{}, errors.Newf("a runtime line names %s, and only the tablet names that level", named)
		}
		line.Roles = append(line.Roles, role)
	}
	if len(actors) > 0 {
		line.Actor = actors[0]
		line.By = actors[1:]
	}
	return line, nil
}

// addRuntime lays the store's lines over the tablet's rows. Lines settle per
// pair, a path with a role: the latest line about a pair wins, ROOT first, and
// a line about a different pair is untouched. A pair whose winning line is
// revoked is gone; the rest reach. A path the tablet names keeps its levels
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
