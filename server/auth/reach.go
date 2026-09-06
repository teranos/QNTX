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
