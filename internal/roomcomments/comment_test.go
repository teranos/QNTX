package roomcomments

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func delivery(t *testing.T, event, repo, action, body string) []byte {
	t.Helper()
	words := map[string]any{"id": 123, "body": body, "user": map[string]any{"login": "qodo-code-review[bot]"}, "updated_at": "2026-11-08T20:00:00Z", "path": "src/rooms.ts", "line": 42}
	payload := map[string]any{"action": action, "repository": map[string]any{"full_name": repo}, "issue": map[string]any{"number": 100, "pull_request": map[string]any{}}, "pull_request": map[string]any{"number": 100}, "comment": words, "review": words}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return b
}

// "When a PR comment lands on QNTX, QNTX-Web, QNTX-App"
func TestPRCommentKindsAndRepositories(t *testing.T) {
	for _, repo := range []string{"teranos/QNTX", "teranos/QNTX-Web", "teranos/QNTX-App"} {
		for event, fragment := range map[string]string{"issue_comment": "issuecomment-", "pull_request_review_comment": "discussion_r", "pull_request_review": "pullrequestreview-"} {
			t.Run(repo+"/"+event, func(t *testing.T) {
				action := "created"
				if event == "pull_request_review" {
					action = "submitted"
				}
				c, err := Parse(event, delivery(t, event, strings.ToLower(repo), action, "Please keep the recording."))
				require.NoError(t, err)
				require.NotNil(t, c)
				assert.Equal(t, repo, c.Repo)
				assert.Equal(t, "https://github.com/"+repo+"/pull/100#"+fragment+"123", c.URL)
				assert.Contains(t, c.Message(), "qodo-code-review\\[bot\\]")
				assert.Contains(t, c.Message(), "Please keep the recording.")
			})
		}
	}
}

func TestOtherEventsAndOrdinaryIssuesStayOut(t *testing.T) {
	for _, tc := range []struct{ event, repo, action, body string }{
		{"push", "teranos/QNTX", "created", "comment"},
		{"issue_comment", "someone/QNTX", "created", "comment"},
		{"issue_comment", "teranos/QNTX", "deleted", "comment"},
		{"pull_request_review", "teranos/QNTX", "dismissed", "comment"},
		{"pull_request_review", "teranos/QNTX", "submitted", "  "},
	} {
		c, err := Parse(tc.event, delivery(t, tc.event, tc.repo, tc.action, tc.body))
		require.NoError(t, err)
		assert.Nil(t, c)
	}
	for _, pr := range []string{`"pull_request":null`, `"ordinary_issue":{}`} {
		body := strings.Replace(string(delivery(t, "issue_comment", "teranos/QNTX", "created", "comment")), `"pull_request":{}`, pr, 1)
		c, err := Parse("issue_comment", []byte(body))
		require.NoError(t, err)
		assert.Nil(t, c)
	}
	_, err := Parse("issue_comment", []byte("{"))
	require.Error(t, err)
}

func TestRedeliveryAndEditsHaveStableDistinctIdentities(t *testing.T) {
	b := delivery(t, "issue_comment", "teranos/QNTX", "created", "first")
	one, err := Parse("issue_comment", b)
	require.NoError(t, err)
	again, err := Parse("issue_comment", b)
	require.NoError(t, err)
	assert.Equal(t, one.Key, again.Key)
	edit, err := Parse("issue_comment", delivery(t, "issue_comment", "teranos/QNTX", "edited", "second"))
	require.NoError(t, err)
	assert.NotEqual(t, one.Key, edit.Key)
	assert.Contains(t, edit.Message(), "updated their comment")
	later := strings.ReplaceAll(string(delivery(t, "issue_comment", "teranos/QNTX", "edited", "second")), "20:00:00Z", "21:00:00Z")
	restored, err := Parse("issue_comment", []byte(later))
	require.NoError(t, err)
	assert.NotEqual(t, edit.Key, restored.Key)
}

// "For example given the Qodo comments we see on many of our PR’s"
func TestQodoPresentationIsReadableAndTheOriginalIsUnchanged(t *testing.T) {
	body := "<h3>PR Code Suggestions</h3>\n<details><summary>Keep audio &amp; text</summary>\n<dl><dd><pre>Preserve the recording</pre></dd></dl>\nUse `<audio>` here.\n```html\n<audio controls src=\"clip.webm\"></audio>\n```\n</details>\n<!-- hidden metadata -->"
	c, err := Parse("issue_comment", delivery(t, "issue_comment", "teranos/QNTX-Web", "created", body))
	require.NoError(t, err)
	assert.Equal(t, body, c.Body)
	message := c.Message()
	for _, want := range []string{"PR Code Suggestions", "Keep audio & text", "Preserve the recording", "Use `<audio>` here.", "```html\n<audio controls src=\"clip.webm\"></audio>\n```", "[View comment on GitHub](" + c.URL + ")"} {
		assert.Contains(t, message, want)
	}
	for _, unwanted := range []string{"<h3>", "<details>", "<summary>", "<dd>", "hidden metadata"} {
		assert.NotContains(t, message, unwanted)
	}
}
