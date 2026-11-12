package roomcomments

import "strings"

// "it's like a VIP room: you need to be on the list"
// "if it comes from an App I authorised"

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] ClaudeFooter is what Claude Code ends the description of a pull request it」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] opened with.」
const ClaudeFooter = "Generated with [Claude Code]"

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Everyone is the room name that stands for every room.」
const Everyone = "*"

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Guests is, by room name, the slugs of the GitHub Apps whose comments may」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] wake an agent: under Everyone for every room, under a room's name for that」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] room alone.」
type Guests map[string][]string

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Admits is whether the app is on the list for the room.」
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

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] OfClaude is whether the comment is on a pull request Claude Code opened.」
func (c Comment) OfClaude() bool {
	return strings.Contains(c.PRBody, ClaudeFooter)
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Wakes is whether the comment is to start a turn of the agent that owns the」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] pull request: an app on the room's list wrote it, on a pull request Claude」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Code opened.」
func (c Comment) Wakes(g Guests, room string) bool {
	if c.Kind == "pull_request" {
		// 「[Opus 5.5] Ready for review wakes whoever changed it; back to a draft is said」
		// 「[Opus 5.5] into the room and wakes no one.」
		return c.Action == "ready_for_review" && c.OfClaude()
	}
	return c.OfClaude() && g.Admits(room, c.App)
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Speaker is who the turn it starts is said by: the app, or the person who」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] changed the pull request.」
func (c Comment) Speaker() string {
	if c.App != "" {
		return "github-app:" + c.App
	}
	return "github:" + c.Poster()
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Poster is who the Room shows as having said the comment: the app that wrote」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] it, or the person, by the name GitHub gives them.」
func (c Comment) Poster() string {
	if c.App != "" {
		return c.App
	}
	return strings.TrimSuffix(c.Author, "[bot]")
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Quiet is whether the comment is a review's: it wakes the agent when the room」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] has gone quiet, not at once, because a review arrives as many comments.」
func (c Comment) Quiet() bool {
	return c.Kind == "pull_request_review_comment" || c.Kind == "pull_request_review" || c.Kind == "pull_request"
}
