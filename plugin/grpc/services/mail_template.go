package services

import (
	"bytes"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"github.com/teranos/QNTX/internal/named"
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
	html, err := DrawMail(MailWindow{Title: "{{.subject}}", TitleAlone: true, Sections: []MailSection{{HTML: body, Headless: true}}})
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

// parsedMail is a template's parts, parsed, and the template they were parsed
// from, which is what is kept. A value a part names and a Send does not hold
// is an error: missingkey=error on every part.
type parsedMail struct {
	source  *protocol.MailTemplate
	subject *texttemplate.Template
	body    named.Items[mailPart]
}

// mailPart is one part of a body a mail is sent in, its html or its text.
type mailPart struct {
	fill func(values map[string]string, into *FilledMail) error
}

// parseMail is a template's parts, parsed, or why it cannot be kept: no
// subject, no body, or a part that does not parse. A request that sent no
// template sent no subject either. A template is kept as it was sent, and a
// Send parses it again from the record.
func parseMail(t *protocol.MailTemplate) (parsedMail, error) {
	subjectSource, err := named.TextOf("a template's subject", strings.TrimSpace(t.GetSubject()))
	if err != nil {
		return parsedMail{}, err
	}
	subject, err := texttemplate.New("subject").Option("missingkey=error").Parse(subjectSource.String())
	if err != nil {
		return parsedMail{}, errors.Wrap(err, "the subject does not parse")
	}

	// A part the template leaves blank is a part its mail is not sent in.
	var parts []mailPart
	if html, htmlErr := named.TextOf("html", strings.TrimSpace(t.GetHtml())); htmlErr == nil {
		parsed, err := htmltemplate.New("html").Option("missingkey=error").Parse(html.String())
		if err != nil {
			return parsedMail{}, errors.Wrap(err, "the html does not parse")
		}
		parts = append(parts, mailPart{fill: func(values map[string]string, into *FilledMail) error {
			var out bytes.Buffer
			if err := parsed.Execute(&out, values); err != nil {
				return errors.Wrap(err, "the html could not be filled")
			}
			into.HTML = out.String()
			return nil
		}})
	}
	if text, textErr := named.TextOf("text", strings.TrimSpace(t.GetText())); textErr == nil {
		parsed, err := texttemplate.New("text").Option("missingkey=error").Parse(text.String())
		if err != nil {
			return parsedMail{}, errors.Wrap(err, "the text does not parse")
		}
		parts = append(parts, mailPart{fill: func(values map[string]string, into *FilledMail) error {
			filled, err := fillText("text", parsed, values)
			into.Text = filled
			return err
		}})
	}
	body, err := named.ItemsOf("a template's html or text body", parts)
	if err != nil {
		return parsedMail{}, err
	}
	return parsedMail{source: t, subject: subject, body: body}, nil
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
	filledSubject, err := named.TextOf("the filled subject", strings.TrimSpace(subject))
	if err != nil {
		return FilledMail{}, err
	}
	if strings.ContainsAny(filledSubject.String(), "\r\n") {
		return FilledMail{}, errors.Newf("the filled subject spans lines: %q", filledSubject)
	}

	filled := FilledMail{Subject: filledSubject.String()}
	for _, part := range parsed.body.All() {
		if err := part.fill(values, &filled); err != nil {
			return FilledMail{}, err
		}
	}
	return filled, nil
}

func fillText(part string, parsed *texttemplate.Template, values map[string]string) (string, error) {
	var out bytes.Buffer
	if err := parsed.Execute(&out, values); err != nil {
		return "", errors.Wrapf(err, "the %s could not be filled", part)
	}
	return out.String(), nil
}
