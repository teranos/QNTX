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

// wakeAfterComment tells the agent the room, when the comment just said into
// it is one the agent is to be woken by.
func (s *QNTXServer) wakeAfterComment(target roomcomments.Target, c roomcomments.Comment) {
	if !c.Wakes(wakeApps(), nodeRoomName) {
		return
	}
	speaker := c.Speaker()
	if c.Quiet() {
		// A review is many comments: the agent is told when none has come for
		// the quiet stretch, and each one that does moves that later.
		if err := s.dueWakes().Defer(s.lifetime(), target, wakeAgent, speaker, c.At, time.Now().Add(wakeQuiet())); err != nil {
			s.logger.Errorw("A review comment was said into the Node Room and the agent's wake was not kept", "agent", wakeAgent, "room", target.Room, "error", err)
		}
		return
	}
	s.tellNow(target, speaker, c.At)
}

// tellNow tells the agent the room, one wake of it at a time.
func (s *QNTXServer) tellNow(target roomcomments.Target, speaker, since string) {
	wakes.ask(target.Provider+"/"+target.Room+"/"+wakeAgent, speaker, func(speaker string) {
		if err := s.tellAgentTheRoom(target, wakeAgent, speaker, since); err != nil {
			s.logger.Errorw("A PR comment was said into the Node Room and the agent was not told", "agent", wakeAgent, "room", target.Room, "error", err)
		}
	})
}

// wakeQuiet is how long the room is to go without a review comment before the
// agent is told them. The config key is github.wake_quiet, as a duration.
func wakeQuiet() time.Duration {
	if quiet := config.GetViper().GetDuration("github.wake_quiet"); quiet > 0 {
		return quiet
	}
	return 30 * time.Minute
}

// dueWakes is the wakes waiting for a quiet room, in the node's own file.
func (s *QNTXServer) dueWakes() roomcomments.Due {
	return roomcomments.Due{DB: s.nodeDB}
}

// tellDueAgents tells each agent whose room has been quiet long enough. A wake
// is held for another quiet stretch while its agent is told, and removed once
// it was: one that could not be carried out, or that the node stopped in the
// middle of, comes again when the hold is out.
func (s *QNTXServer) tellDueAgents() {
	for {
		select {
		case <-s.lifetime().Done():
			return
		case <-time.After(30 * time.Second):
		}
		if s.nodeDB == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(s.lifetime(), 10*time.Second)
		due := s.dueWakes()
		ready, err := due.Ready(ctx, time.Now())
		if err != nil {
			s.logger.Errorw("The wakes waiting for a quiet room were not read", "error", err)
		}
		for _, w := range ready {
			held, took, err := due.Claim(ctx, w, time.Now().Add(wakeQuiet()))
			if err != nil {
				s.logger.Errorw("A wake waiting for a quiet room was not claimed", "room", w.Target.Room, "agent", w.Agent, "error", err)
				continue
			}
			if took {
				s.tellWhenQuiet(held)
			}
		}
		cancel()
	}
}

func (s *QNTXServer) tellWhenQuiet(w roomcomments.Wake) {
	wakes.ask(w.Target.Provider+"/"+w.Target.Room+"/"+w.Agent, w.Speaker, func(speaker string) {
		if err := s.tellAgentTheRoom(w.Target, w.Agent, speaker, w.Since); err != nil {
			// The wake is still held: it comes again when the hold is out,
			// and a comment that came since keeps its own speaker and time.
			s.logger.Errorw("The room went quiet and the agent was not told", "agent", w.Agent, "room", w.Target.Room, "error", err)
			return
		}
		// Not the node's lifetime: a node that is stopping still removes what it told.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.lifetime()), 10*time.Second)
		defer cancel()
		if err := s.dueWakes().Done(ctx, w); err != nil {
			s.logger.Errorw("The agent was told the quiet room and its wake was not removed", "agent", w.Agent, "room", w.Target.Room, "error", err)
		}
	})
}

// tellAgentTheRoom runs a turn of the Node Agent told everything said in the
// room since it was last told, and posts its answer into the room as it. What
// it was told is kept before the turn runs: what is said during the turn is
// the next one's.
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
	for _, m := range read.GetMessages() {
		said = append(said, roomcomments.Said{Sender: m.GetSenderName(), Body: m.GetBody(), At: m.GetAt()})
	}
	if len(said) == 0 {
		return nil
	}
	if err := told.Keep(ctx, target.Room, agent, said[len(said)-1].At); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(withSpeaker(lifetime, speaker), http.MethodPost, "/github/comments", nil)
	if err != nil {
		return errors.Wrap(err, "the asking did not build")
	}
	if err := s.answerInRoom(request, target.Provider, rooms, target.Room, "", agent, "GitHub", roomcomments.Digest(said, read.GetTruncated())); err != nil {
		// Its answer did not reach the room, so it is told the same again.
		back, cancel := context.WithTimeout(context.WithoutCancel(lifetime), 10*time.Second)
		defer cancel()
		if kept := told.Back(back, target.Room, agent, marked); kept != nil {
			return errors.Wrapf(err, "and %v", kept)
		}
		return err
	}
	return nil
}
