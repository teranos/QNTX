package qntxinbox

import (
	"bytes"
	"encoding/base64"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"unicode"

	"github.com/teranos/errors"
)

const receivedForMarker = " for "

// received is one stored message, as far as a text mail client reads it.
type received struct {
	From       string
	To         []string
	Cc         []string
	Subject    string
	MessageID  string
	InReplyTo  string
	References string
	Date       string
	Text       string
	Spam       bool
	Virus      bool
	envelope   []string
}

// Recipients is every address the message reached: its To and Cc, and the
// addresses SES wrote into Received, which name a blind copy too.
func (m received) Recipients() []string {
	seen := map[string]bool{}
	var all []string
	for _, list := range [][]string{m.To, m.Cc, m.envelope} {
		for _, a := range list {
			a = strings.ToLower(strings.TrimSpace(a))
			if a != "" && !seen[a] {
				seen[a] = true
				all = append(all, a)
			}
		}
	}
	return all
}

func parse(raw []byte) (received, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return received{}, errors.Wrap(err, "the message is not RFC 5322")
	}
	h := msg.Header
	dec := new(mime.WordDecoder)
	decodedHeader := func(name string) string {
		v := h.Get(name)
		if d, err := dec.DecodeHeader(v); err == nil {
			return d
		}
		return v
	}
	m := received{
		From:       decodedHeader("From"),
		To:         addresses(h, "To"),
		Cc:         addresses(h, "Cc"),
		Subject:    decodedHeader("Subject"),
		MessageID:  strings.TrimSpace(h.Get("Message-ID")),
		InReplyTo:  strings.TrimSpace(h.Get("In-Reply-To")),
		References: strings.TrimSpace(h.Get("References")),
		Date:       strings.TrimSpace(h.Get("Date")),
		Spam:       strings.EqualFold(strings.TrimSpace(h.Get("X-SES-Spam-Verdict")), "FAIL"),
		Virus:      strings.EqualFold(strings.TrimSpace(h.Get("X-SES-Virus-Verdict")), "FAIL"),
	}
	for _, line := range h["Received"] {
		if a := receivedFor(line); a != "" {
			m.envelope = append(m.envelope, a)
		}
	}
	text, isHTML, err := textOf(h.Get("Content-Type"), h.Get("Content-Transfer-Encoding"), msg.Body)
	if err != nil {
		return received{}, err
	}
	if isHTML {
		text = textOfHTML(text)
	}
	m.Text = strings.TrimSpace(text)
	return m, nil
}

func addresses(h mail.Header, name string) []string {
	list, err := h.AddressList(name)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

// receivedFor is the recipient a Received line names, up to its semicolon.
func receivedFor(line string) string {
	_, after, found := strings.Cut(line, receivedForMarker)
	if !found {
		return ""
	}
	addr, _, _ := strings.Cut(after, ";")
	addr = strings.Trim(strings.TrimSpace(addr), "<>")
	if !strings.Contains(addr, "@") || strings.ContainsAny(addr, " \t") {
		return ""
	}
	return addr
}

// textOf is a part's text: its plain text, else its HTML, and whether it was HTML.
func textOf(contentType, encoding string, body io.Reader) (string, bool, error) {
	if contentType == "" {
		contentType = "text/plain; charset=us-ascii"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		var htmlText string
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", false, errors.Wrap(err, "a part of the message did not read")
			}
			text, isHTML, err := textOf(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part)
			if err != nil {
				return "", false, err
			}
			if !isHTML && text != "" {
				return text, false, nil
			}
			if isHTML && htmlText == "" {
				htmlText = text
			}
		}
		return htmlText, htmlText != "", nil
	}
	if mediaType != "text/plain" && mediaType != "text/html" {
		return "", false, nil
	}
	data, err := io.ReadAll(transferDecoded(encoding, body))
	if err != nil {
		return "", false, errors.Wrapf(err, "the %s body did not decode", mediaType)
	}
	return inUTF8(params["charset"], data), mediaType == "text/html", nil
}

func transferDecoded(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	}
	return body
}

// inUTF8 reads Latin-1 as the runes its bytes are; anything else is taken as UTF-8.
func inUTF8(charset string, data []byte) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "windows-1252":
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return string(runes)
	}
	return string(data)
}

// textOfHTML is what an HTML body says: tags dropped, entities read, whitespace one space.
func textOfHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune(' ')
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.FieldsFunc(html.UnescapeString(b.String()), unicode.IsSpace), " ")
}
