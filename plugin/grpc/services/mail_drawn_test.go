package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "make sure that the mails also use like a nicer.   QNTX Mail template"
func TestANodeMailIsDrawnAsAQNTXWindow(t *testing.T) {
	m := DrawnNodeMail("google-cloud", "What the node spends at Google Cloud has resumed", "The budget freed up.", MailLink{Label: "Connect costs", URL: "https://node.test/x?a=1&b=2"})
	assert.Equal(t, "google-cloud", m.Name)
	assert.Equal(t, "What the node spends at Google Cloud has resumed", m.Subject)
	text, err := BodyOf(m.Bodies, MailText)
	require.NoError(t, err)
	assert.Contains(t, text, "The budget freed up.")
	assert.Contains(t, text, "https://node.test/x?a=1&b=2")
	page, err := BodyOf(m.Bodies, MailHTML)
	require.NoError(t, err)
	assert.Contains(t, page, `name="color-scheme" content="dark"`, "drawn as the dark template is")
	assert.NotContains(t, page, "<pre>")
	assert.Contains(t, page, "The budget freed up.")
	assert.Contains(t, page, "What the node spends at Google Cloud has resumed", "titled with its subject")
	assert.Contains(t, page, `href="https://node.test/x?a=1&amp;b=2"`)
	assert.Contains(t, page, ">Connect costs</a>")
}

func TestANodeMailWithNoLinkLeadsNowhere(t *testing.T) {
	m := DrawnNodeMail("google-cloud", "Halted", "ROOT presses Connect costs again.")
	for _, body := range m.Bodies {
		assert.NotContains(t, body.Content, "href=")
		assert.Contains(t, body.Content, "ROOT presses Connect costs again.")
	}
}

// 「[Opus 5.5] A mail with several presses draws each.」
func TestANodeMailDrawsEachLink(t *testing.T) {
	m := DrawnNodeMail("google-cloud", "Keys", "Each namespace's key.", MailLink{Label: "Connect a key for garden", URL: "https://node.test/g"}, MailLink{Label: "Connect a key for shed", URL: "https://node.test/s"})
	page, err := BodyOf(m.Bodies, MailHTML)
	require.NoError(t, err)
	assert.Contains(t, page, `<a href="https://node.test/g" style="color:inherit">Connect a key for garden</a>`)
	assert.Contains(t, page, `<a href="https://node.test/s" style="color:inherit">Connect a key for shed</a>`)
}
