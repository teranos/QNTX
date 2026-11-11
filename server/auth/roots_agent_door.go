package auth

import (
	"context"
	"strings"
)

// 「[Opus 5.5] ThroughARootsAgentsDoor is whether a request came through the door of one of ROOT's agents:」
// 「[Opus 5.5] the Node Agent or an element agent, whose sessions are kept in system (ADR-048, ADR-054).」
func ThroughARootsAgentsDoor(ctx context.Context) bool {
	grant, through := doorGrant(ctx)
	return through && (grant.Label == NodeAgentLabel || strings.HasPrefix(grant.Label, ElementAgentLabel+":"))
}
