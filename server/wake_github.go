package server

// "it's like a VIP room: you need to be on the list"
// "if it comes from an App I authorised"
// "it ends up in the room first, and then everything that was in the room since
// the agent was told gets told all at once as one turn"

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/roomcomments"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
)

type speakerKey struct{}

// withSpeaker is ctx with who said what the turn is told, for a turn no
// admitted caller started. Only the node's own wake for a comment sets it.
func withSpeaker(ctx context.Context, speaker string) context.Context {
	return context.WithValue(ctx, speakerKey{}, speaker)
}

func speakerOf(ctx context.Context) string {
	speaker, _ := ctx.Value(speakerKey{}).(string)
	return speaker
}

// wakeApps is the list of GitHub Apps whose comments wake an agent, read from
// the config table github.wake_apps. A key is a room's name, or the asterisk
// for every room; a value is a list of app slugs. Qodo is on it for every
// room until the node says otherwise.
func wakeApps() roomcomments.Guests {
	listed := config.GetViper().GetStringMapStringSlice("github.wake_apps")
	if len(listed) == 0 {
		return roomcomments.Guests{roomcomments.Everyone: {"qodo-code-review"}}
	}
	guests := make(roomcomments.Guests, len(listed))
	for room, apps := range listed {
		guests[strings.ToLower(room)] = apps
	}
	return guests
}

// wakeAgent is the Node Agent a comment on a Claude Code pull request wakes.
const wakeAgent = agentLineClaude

// roomWakes holds one wake of an agent in a room at a time. A comment that
// arrives while the agent is being told asks for one more, after it: what was
// said meanwhile is told together, as one turn.
type roomWakes struct {
	mu      sync.Mutex
	running map[string]bool
	again   map[string]string
}

var wakes = &roomWakes{running: map[string]bool{}, again: map[string]string{}}

func (w *roomWakes) ask(key, speaker string, tell func(speaker string)) {
	w.mu.Lock()
	if w.running[key] {
		w.again[key] = speaker
		w.mu.Unlock()
		return
	}
	w.running[key] = true
	w.mu.Unlock()
	sacred.Go("rooms.github.wake", func() {
		for {
			tell(speaker)
			w.mu.Lock()
			next, more := w.again[key]
			delete(w.again, key)
			if !more {
				delete(w.running, key)
				w.mu.Unlock()
				return
			}
			speaker = next
			w.mu.Unlock()
		}
	})
}

// findingsSeen is, by comment, the sum of what it found when it last woke the
// agent and the agent took the turn. An edit finding the same wakes no one. It
// lives as long as the node: after a restart the first edit wakes once more.
type findingsSeen struct {
	mu   sync.Mutex
	seen map[string]string
}

var findings = &findingsSeen{seen: map[string]string{}}

func (f *findingsSeen) same(c roomcomments.Comment) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[c.URL] == c.Findings()
}

// keep is said once the agent took the turn the comment woke: a turn refused
// leaves the next edit, finding the same, to wake it again.
func (f *findingsSeen) keep(c roomcomments.Comment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen[c.URL] = c.Findings()
}

// wakeAfterComment tells the agent the room, when the comment just said into
// it is one the agent is to be woken by.
func (s *QNTXServer) wakeAfterComment(target roomcomments.Target, c roomcomments.Comment) {
	if !c.Wakes(wakeApps(), nodeRoomName) {
		return
	}
	if findings.same(c) {
		return
	}
	wakes.ask(target.Provider+"/"+target.Room+"/"+wakeAgent, "github-app:"+c.App, func(speaker string) {
		if err := s.tellAgentTheRoom(target, wakeAgent, speaker, c.At); err != nil {
			s.logger.Errorw("A PR comment was said into the Node Room and the agent was not told", "agent", wakeAgent, "room", target.Room, "error", err)
			return
		}
		findings.keep(c)
	})
}

// tellAgentTheRoom runs a turn of the Node Agent told everything said in the
// room since it was last told, and posts its answer into the room as it. What
// it was told is kept once it took the turn, not before: a turn it refused
// leaves it to be told again.
//
// An agent never told is told from since, the time of the comment that woke it:
// a room reads oldest first, so reading it all would tell the oldest.
func (s *QNTXServer) tellAgentTheRoom(target roomcomments.Target, agent, speaker, since string) error {
	if s.nodeDB == nil {
		return errors.New("the node has no operational store for what an agent was told")
	}
	lifetime := s.lifetime()
	ctx, cancel := context.WithTimeout(lifetime, 30*time.Second)
	defer cancel()
	_, rooms, refused := s.roomsOf(target.Provider)
	if refused != nil {
		return errors.New(refused.Says)
	}
	if !s.nodeAgentsInNodeRoom(ctx, target.Provider, rooms, target.Room)[agent] {
		return errors.Newf("%s is no Node Agent in the Node Room", agent)
	}
	told := roomcomments.Told{DB: s.nodeDB}
	marked, err := told.Mark(ctx, target.Room, agent)
	if err != nil {
		return err
	}
	from := since
	if marked != "" {
		if from, err = roomcomments.ReadFrom(marked); err != nil {
			return err
		}
	}
	read, err := rooms.ReadRoom(ctx, &protocol.ReadRoomRequest{Provider: target.Provider, Room: target.Room, Since: from, Limit: 100})
	if err != nil {
		return errors.Wrap(err, "the Node Room was not read")
	}
	var said []roomcomments.Said
	var last string
	for _, m := range read.GetMessages() {
		heard := roomcomments.Said{Sender: m.GetSenderName(), Body: m.GetBody(), At: m.GetAt()}
		last = heard.At
		if !roomcomments.Unheard(heard, agent) {
			said = append(said, heard)
		}
	}
	if len(said) == 0 {
		if last == "" {
			return nil
		}
		return told.Keep(ctx, target.Room, agent, last)
	}
	request, err := http.NewRequestWithContext(withSpeaker(lifetime, speaker), http.MethodPost, "/github/comments", nil)
	if err != nil {
		return errors.Wrap(err, "the asking did not build")
	}
	if !s.answerInRoom(request, target.Provider, rooms, target.Room, "", agent, "GitHub", roomcomments.Digest(said, read.GetTruncated())) {
		return errors.Newf("%s did not take the turn, so what it was to be told is told again with the next", agent)
	}
	// Kept once the agent took the turn: what is said during it is the next one's.
	return told.Keep(context.WithoutCancel(ctx), target.Room, agent, last)
}
