package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "make sure that the mails also use like a nicer.   QNTX Mail template"
func TestANodeMailIsDrawnAsAQNTXWindow(t *testing.T) {
	m := DrawnNodeMail("google-cloud", "What the node spends at Google Cloud has resumed", "The budget freed up.", "https://node.test/x?a=1&b=2", "Connect costs")
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
	m := DrawnNodeMail("google-cloud", "Halted", "ROOT presses Connect costs again.", "", "")
	for _, body := range m.Bodies {
		assert.NotContains(t, body.Content, "href=")
		assert.Contains(t, body.Content, "ROOT presses Connect costs again.")
	}
}
