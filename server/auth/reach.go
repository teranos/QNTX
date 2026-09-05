package auth

import "slices"

// What a route lets in.

// ROOT reaches everything that is reachable. It is never listed — that is what
// ROOT is.

// Every other level reaches a route because a line granted it. An empty Reach
// grants nothing, which is what empty is.
type Reach struct {
	also []Level
}

// Also grants reach to levels beside ROOT.
func Also(levels ...Level) Reach {
	return Reach{also: levels}
}

// Beyond names the levels this route admits besides ROOT.
func (re Reach) Beyond() []Level {
	return slices.Clone(re.also)
}

// reaches reports whether an admission at this level goes through.
func (re Reach) reaches(level Level) bool {
	if level == LevelRoot {
		return true
	}
	return slices.Contains(re.also, level)
}
