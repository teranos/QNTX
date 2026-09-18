package auth

import "slices"

// What a route lets in.

// ROOT reaches everything that is reachable. It is never listed — that is what
// ROOT is.

// Every other level reaches a route because a line granted it, and so does
// every role. An empty Reach grants nothing, which is what empty is.
type Reach struct {
	also  []Level
	roles []string
}

// Also grants reach to levels beside ROOT.
func Also(levels ...Level) Reach {
	return Reach{also: levels}
}

// AndRoles grants reach to roles as well. A role is a word a runtime line
// named; who holds it is a line in the store, not a level anyone is born at.
func (re Reach) AndRoles(roles ...string) Reach {
	return Reach{also: re.also, roles: append(slices.Clone(re.roles), roles...)}
}

// Beyond names the levels this route admits besides ROOT.
func (re Reach) Beyond() []Level {
	return slices.Clone(re.also)
}

// Roles names the roles this route admits.
func (re Reach) Roles() []string {
	return slices.Clone(re.roles)
}

// With is two reaches put together: whoever either admits, each named once.
// More than one line can be about one sigil (ADR-039): its path's, its own and
// its signum's.
func (re Reach) With(other Reach) Reach {
	together := Reach{also: slices.Clone(re.also), roles: slices.Clone(re.roles)}
	for _, level := range other.also {
		if !slices.Contains(together.also, level) {
			together.also = append(together.also, level)
		}
	}
	for _, role := range other.roles {
		if !slices.Contains(together.roles, role) {
			together.roles = append(together.roles, role)
		}
	}
	return together
}

// Admits reports whether an admission goes through. It is the gate's own
// question (Middleware asks it through reaches), asked by something that lists
// rather than answers: a caller is shown the tools they reach. One decision,
// so the list and the gate cannot disagree.
func (re Reach) Admits(a Admission) bool {
	return re.reaches(a.level, a.roles)
}

// reaches reports whether an admission goes through: by its level, or by any
// role it holds.
func (re Reach) reaches(level Level, held []string) bool {
	if level == LevelRoot {
		return true
	}
	if slices.Contains(re.also, level) {
		return true
	}
	for _, role := range held {
		if slices.Contains(re.roles, role) {
			return true
		}
	}
	return false
}
