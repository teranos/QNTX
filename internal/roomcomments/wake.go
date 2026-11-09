package roomcomments

import (
	"fmt"
	"strings"
)

// "it's like a VIP room: you need to be on the list"
// "if it comes from an App I authorised"

// ClaudeFooter is what Claude Code ends the description of a pull request it
// opened with.
const ClaudeFooter = "Generated with [Claude Code]"

// Everyone is the room name that stands for every room.
const Everyone = "*"

// Guests is, by room name, the slugs of the GitHub Apps whose comments may
// wake an agent: under Everyone for every room, under a room's name for that
// room alone.
type Guests map[string][]string

// Admits is whether the app is on the list for the room.
func (g Guests) Admits(room, app string) bool {
	if app == "" {
		return false
	}
	for name, apps := range g {
		if name != Everyone && !strings.EqualFold(name, room) {
			continue
		}
		for _, listed := range apps {
			if strings.EqualFold(listed, app) {
				return true
			}
		}
	}
	return false
}

// OfClaude is whether the comment is on a pull request Claude Code opened.
func (c Comment) OfClaude() bool {
	return strings.Contains(c.PRBody, ClaudeFooter)
}

// Wakes is whether the comment is to start a turn of the agent that owns the
// pull request: an app on the room's list wrote it, on a pull request Claude
// Code opened.
func (c Comment) Wakes(g Guests, room string) bool {
	return c.OfClaude() && g.Admits(room, c.App)
}

// Says is what the turn is told.
func (c Comment) Says() string {
	where := ""
	if c.Path != "" {
		where = fmt.Sprintf(" on %s", c.Path)
		if c.Line > 0 {
			where += fmt.Sprintf(" line %d", c.Line)
		}
	}
	return fmt.Sprintf("%s commented%s on %s #%d: %s\n\n%s", c.App, where, c.Repo, c.Number, c.URL, c.Body)
}
