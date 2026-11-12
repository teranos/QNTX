package roomcomments

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
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

// "Wake me only for PRs I hold; let the rest sit in the Room unread"

// Wakes is whether the comment is to start a turn of the agent that owns the
// pull request: an app on the room's list wrote it, on a pull request Claude
// Code opened through the node's App. One a person opened, the person holds.
func (c Comment) Wakes(g Guests, room string) bool {
	return c.OfClaude() && c.PRByApp && g.Admits(room, c.App)
}

// Poster is who the Room shows as having said the comment: the app that wrote
// it, or the person, by the name GitHub gives them.
func (c Comment) Poster() string {
	if c.App != "" {
		return c.App
	}
	return strings.TrimSuffix(c.Author, "[bot]")
}

// "Don't relay Qodo updated their comment edits whose set of findings is
// unchanged"

// passing is what changes in a comment from one review of a pull request to
// the next without its findings changing: the commit it reviewed, times, counts.
var passing = regexp.MustCompile(`\b[0-9a-f]{7,40}\b|[0-9]+`)

// Findings is a sum of what the comment says, less the commit, times and
// counts that change with every review: two versions of a comment with the same
// sum found the same things.
func (c Comment) Findings() string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(passing.ReplaceAllString(c.Body, "")), " ")))
	return hex.EncodeToString(sum[:])
}
