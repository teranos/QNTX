package qntxinbox

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raw(lines ...string) []byte {
	return []byte(strings.Join(lines, "\r\n"))
}

func TestAPlainMailIsReadWhole(t *testing.T) {
	m, err := parse(raw(
		"Received: from mail.example.com by inbound-smtp.eu-central-1.amazonaws.com with SMTP id abc for timothy@example.com; Thu, 01 Oct 2026 21:00:00 +0000",
		"From: Ada Lovelace <ada@example.com>",
		"To: timothy@example.com",
		"Cc: contact@example.com",
		"Subject: =?UTF-8?Q?Caf=C3=A9_tomorrow?=",
		"Message-ID: <one@example.com>",
		"In-Reply-To: <zero@example.com>",
		"References: <zero@example.com>",
		"Date: Thu, 01 Oct 2026 21:00:00 +0000",
		"X-SES-Spam-Verdict: PASS",
		"X-SES-Virus-Verdict: PASS",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Hello Tim,",
		"see you.",
	))
	require.NoError(t, err)
	assert.Equal(t, "Ada Lovelace <ada@example.com>", m.From)
	assert.Equal(t, []string{"timothy@example.com"}, m.To)
	assert.Equal(t, []string{"contact@example.com"}, m.Cc)
	assert.Equal(t, "Café tomorrow", m.Subject)
	assert.Equal(t, "<one@example.com>", m.MessageID)
	assert.Equal(t, "<zero@example.com>", m.InReplyTo)
	assert.Equal(t, "Hello Tim,\r\nsee you.", m.Text)
	assert.False(t, m.Spam)
	assert.False(t, m.Virus)
	assert.ElementsMatch(t, []string{"timothy@example.com", "contact@example.com"}, m.Recipients())
}

// A blind copy reaches an address the headers never name; SES says it in Received.
func TestABlindCopyIsReachedThroughReceived(t *testing.T) {
	m, err := parse(raw(
		"Received: from x by inbound-smtp.eu-central-1.amazonaws.com with SMTP id abc for hidden@example.com; Thu, 01 Oct 2026 21:00:00 +0000",
		"From: ada@example.com",
		"To: someone@else.org",
		"Subject: hi",
		"",
		"body",
	))
	require.NoError(t, err)
	assert.Contains(t, m.Recipients(), "hidden@example.com")
}

func TestTheTextOfAnAlternativeIsItsPlainPart(t *testing.T) {
	m, err := parse(raw(
		"From: ada@example.com",
		"To: timothy@example.com",
		"Subject: hi",
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="b1"`,
		"",
		"--b1",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"Caf=C3=A9 at nine=",
		" sharp",
		"--b1",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>Café at nine sharp</p>",
		"--b1--",
	))
	require.NoError(t, err)
	assert.Equal(t, "Café at nine sharp", m.Text)
}

func TestABase64AndLatin1BodyIsDecoded(t *testing.T) {
	m, err := parse(raw(
		"From: ada@example.com",
		"To: timothy@example.com",
		"Subject: hi",
		"Content-Type: text/plain; charset=iso-8859-1",
		"Content-Transfer-Encoding: base64",
		"",
		"Q2Fm6SBvcGVu",
	))
	require.NoError(t, err)
	assert.Equal(t, "Café open", m.Text)
}

func TestAnHTMLOnlyMailIsReadAsItsText(t *testing.T) {
	m, err := parse(raw(
		"From: ada@example.com",
		"To: timothy@example.com",
		"Subject: hi",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>Hello</p><p>there &amp; back</p></body></html>",
	))
	require.NoError(t, err)
	assert.Equal(t, "Hello there & back", m.Text)
}

func TestSESVerdictsAreRead(t *testing.T) {
	m, err := parse(raw(
		"From: ada@example.com",
		"To: timothy@example.com",
		"Subject: hi",
		"X-SES-Spam-Verdict: FAIL",
		"X-SES-Virus-Verdict: FAIL",
		"",
		"x",
	))
	require.NoError(t, err)
	assert.True(t, m.Spam)
	assert.True(t, m.Virus)
}
