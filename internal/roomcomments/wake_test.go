package roomcomments

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An app's comment on a pull request Claude Code opened is read with the app
// that wrote it and the description it is on, whichever of the three events
// carried it.
func TestACommentCarriesItsAppAndThePullRequestsDescription(t *testing.T) {
	description := "Why.\n\n🤖 " + ClaudeFooter + "(https://claude.com/claude-code)"
	for event, action := range map[string]string{"issue_comment": "created", "pull_request_review_comment": "created", "pull_request_review": "submitted"} {
		t.Run(event, func(t *testing.T) {
			words := map[string]any{"id": 7, "body": "A finding.", "user": map[string]any{"login": "qodo-code-review[bot]"}, "updated_at": "2026-11-09T20:00:00Z", "performed_via_github_app": map[string]any{"slug": "qodo-code-review"}}
			payload := map[string]any{
				"action": action, "repository": map[string]any{"full_name": "teranos/QNTX"},
				"issue":        map[string]any{"number": 9, "body": description, "pull_request": map[string]any{}},
				"pull_request": map[string]any{"number": 9, "body": description},
				"comment":      words, "review": words,
			}
			b, err := json.Marshal(payload)
			require.NoError(t, err)
			c, err := Parse(event, b)
			require.NoError(t, err)
			require.NotNil(t, c)
			assert.Equal(t, "qodo-code-review", c.App)
			assert.True(t, c.OfClaude())
			assert.True(t, c.Wakes(Guests{Everyone: {"qodo-code-review"}}, "Node Room"))
		})
	}
}

func TestWhoIsOnTheList(t *testing.T) {
	claude := "🤖 " + ClaudeFooter
	qodo := Comment{App: "qodo-code-review", PRBody: claude}

	assert.True(t, qodo.Wakes(Guests{Everyone: {"qodo-code-review"}}, "Node Room"), "on the list for every room")
	assert.True(t, qodo.Wakes(Guests{"node room": {"Qodo-Code-Review"}}, "Node Room"), "on the list for the room, by name and slug in any case")
	assert.False(t, qodo.Wakes(Guests{"Other Room": {"qodo-code-review"}}, "Node Room"), "on another room's list only")
	assert.False(t, qodo.Wakes(Guests{Everyone: {"someone-else"}}, "Node Room"), "not on the list")
	assert.False(t, qodo.Wakes(nil, "Node Room"), "no list")

	assert.False(t, Comment{App: "", PRBody: claude}.Wakes(Guests{Everyone: {""}}, "Node Room"), "a person has no app, and an empty slug is no guest")
	assert.False(t, Comment{App: "qodo-code-review", PRBody: "Not Claude's."}.Wakes(Guests{Everyone: {"qodo-code-review"}}, "Node Room"), "a pull request Claude Code did not open")
}
