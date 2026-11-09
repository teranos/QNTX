package server

// "it's like a VIP room: you need to be on the list"
// "if it comes from an App I authorised"

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/roomcomments"
	"github.com/teranos/QNTX/server/sigil"
)

type speakerKey struct{}

// withSpeaker is ctx with who said what the turn is told, for a turn no
// admitted caller started. Only a verified delivery sets it.
func withSpeaker(ctx context.Context, speaker string) context.Context {
	return context.WithValue(ctx, speakerKey{}, speaker)
}

func speakerOf(ctx context.Context) string {
	speaker, _ := ctx.Value(speakerKey{}).(string)
	return speaker
}

// wakeApps is the list of GitHub Apps whose comments wake an agent, by room:
//
//	[github.wake_apps]
//	"*" = ["qodo-code-review"]
//	"Node Room" = ["another-app"]
//
// Qodo is on it for every room until the node says otherwise.
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

// wakeForComment starts a turn of the Claude Code agent, told the comment as
// said by the app that wrote it, when the comment is one it is to be woken by.
// The turn runs under the node's own lifetime, not the delivery's: GitHub is
// answered at once.
func (s *QNTXServer) wakeForComment(c *roomcomments.Comment) {
	if !c.Wakes(wakeApps(), nodeRoomName) {
		return
	}
	go func() {
		asked := withSpeaker(s.lifetime(), "github-app:"+c.App)
		request, err := http.NewRequestWithContext(asked, http.MethodPost, "/github/comments", nil)
		if err != nil {
			s.logger.Errorw("A PR comment was to wake the agent and the asking did not build", "comment", c.URL, "error", err)
			return
		}
		asked = sigil.WithCaller(asked, request)
		if _, refused := s.harnessSay(asked, s.claudeHarness(), sigil.Sent{"says": c.Says()}); refused != nil {
			s.logger.Errorw("A PR comment was to wake the agent and the agent did not answer", "comment", c.URL, "app", c.App, "says", refused.Says)
			return
		}
		s.logger.Infow("A PR comment woke the agent", "comment", c.URL, "app", c.App)
	}()
}
