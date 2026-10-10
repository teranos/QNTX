package services

import (
	"bytes"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

// "the plugin owns the template but qntx does provide a neutral template and code for how to set it"

// NeutralTemplateName is the template a Send naming none is filled from.
const NeutralTemplateName = "neutral"

// NeutralTemplate is QNTX's own mail: a subject, a body, and a link when the
// send names one. It says nothing about who sent it beyond what the plugin
// fills in.
//
// Values: subject and body are required; link and link_label are not. index
// reads a value without requiring it, so a send may leave those two out.
func NeutralTemplate() *protocol.MailTemplate {
	return &protocol.MailTemplate{
		Subject: `{{.subject}}`,
		Text: `{{.body}}
{{with index . "link"}}
{{.}}
{{end}}`,
		Html: `<!DOCTYPE html>
<html>
<body style="margin:0;padding:24px;background:#ffffff;color:#1a1a1a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;font-size:16px;line-height:1.5">
<div style="max-width:560px;margin:0 auto">
<p style="margin:0 0 16px;white-space:pre-line">{{.body}}</p>
{{with index . "link"}}<p style="margin:0"><a href="{{.}}" style="color:#1a1a1a">{{or (index $ "link_label") .}}</a></p>{{end}}
</div>
</body>
</html>
`,
	}
}

// DarkTemplateName is QNTX's dark template, for a Send that names it.
const DarkTemplateName = "dark"

// DarkTemplate is the neutral template drawn as a QNTX window (DrawMail),
// titled with the subject. The same values as the neutral template.
func DarkTemplate() (*protocol.MailTemplate, error) {
	body := `<div style="padding:0 8px;white-space:pre-line">{{.body}}</div>` +
		`{{with index . "link"}}<div style="padding:12px 8px 0"><a href="{{.}}" style="color:inherit">{{or (index $ "link_label") .}}</a></div>{{end}}`
	html, err := DrawMail(MailWindow{Title: "{{.subject}}", Sections: []MailSection{{HTML: body}}})
	if err != nil {
		return nil, err
	}
	return &protocol.MailTemplate{
		Subject: NeutralTemplate().Subject,
		Text:    NeutralTemplate().Text,
		Html:    html,
	}, nil
}

// isQNTXTemplate is whether a name is one of QNTX's own templates, which a Send
// names without setting it, and a plugin cannot set.
func isQNTXTemplate(name string) bool {
	return name == NeutralTemplateName || name == DarkTemplateName
}

// qntxTemplate is one of QNTX's own templates by name.
func qntxTemplate(name string) (*protocol.MailTemplate, error) {
	if name == DarkTemplateName {
		return DarkTemplate()
	}
	return NeutralTemplate(), nil
}

// FilledMail is a template filled with a send's values.
type FilledMail struct {
	Subject string
	HTML    string
	Text    string
}

// parsedMail is a template's three parts, parsed. A value a part names and
// a Send does not hold is an error: missingkey=error on every part.
type parsedMail struct {
	subject *texttemplate.Template
	html    *htmltemplate.Template
	text    *texttemplate.Template
}

// checkMailTemplate refuses a template that cannot be kept. A template is kept
// as it was sent, and a Send parses it again from the record.
func checkMailTemplate(t *protocol.MailTemplate) error {
	_, err := parseMail(t)
	return err
}

// parseMail is a template's parts, parsed, or why it cannot be kept: no
// subject, no body, or a part that does not parse. A request that sent no
// template sent no subject either.
func parseMail(t *protocol.MailTemplate) (parsedMail, error) {
	if strings.TrimSpace(t.GetSubject()) == "" {
		return parsedMail{}, errors.New("a template needs a subject")
	}
	if strings.TrimSpace(t.Html) == "" && strings.TrimSpace(t.Text) == "" {
		return parsedMail{}, errors.New("a template needs an html or a text body, or both")
	}
	subject, err := texttemplate.New("subject").Option("missingkey=error").Parse(t.Subject)
	if err != nil {
		return parsedMail{}, errors.Wrap(err, "the subject does not parse")
	}
	html, err := htmltemplate.New("html").Option("missingkey=error").Parse(t.Html)
	if err != nil {
		return parsedMail{}, errors.Wrap(err, "the html does not parse")
	}
	text, err := texttemplate.New("text").Option("missingkey=error").Parse(t.Text)
	if err != nil {
		return parsedMail{}, errors.Wrap(err, "the text does not parse")
	}
	return parsedMail{subject: subject, html: html, text: text}, nil
}

// fillMail fills every part of a template with values.
func fillMail(t *protocol.MailTemplate, values map[string]string) (FilledMail, error) {
	parsed, err := parseMail(t)
	if err != nil {
		return FilledMail{}, err
	}
	subject, err := fillText("subject", parsed.subject, values)
	if err != nil {
		return FilledMail{}, err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return FilledMail{}, errors.New("the filled subject is empty")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return FilledMail{}, errors.Newf("the filled subject spans lines: %q", subject)
	}

	text, err := fillText("text", parsed.text, values)
	if err != nil {
		return FilledMail{}, err
	}

	// A part the template leaves empty fills empty.
	var html bytes.Buffer
	if err := parsed.html.Execute(&html, values); err != nil {
		return FilledMail{}, errors.Wrap(err, "the html could not be filled")
	}

	return FilledMail{Subject: subject, HTML: html.String(), Text: text}, nil
}

func fillText(part string, parsed *texttemplate.Template, values map[string]string) (string, error) {
	var out bytes.Buffer
	if err := parsed.Execute(&out, values); err != nil {
		return "", errors.Wrapf(err, "the %s could not be filled", part)
	}
	return out.String(), nil
}
