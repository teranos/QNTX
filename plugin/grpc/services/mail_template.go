package services

import (
	"bytes"
	"fmt"
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

// neutralText and neutralHTML are the neutral template's bodies.
const neutralText = `{{.body}}
{{with index . "link"}}
{{.}}
{{end}}`

const neutralHTML = `<!DOCTYPE html>
<html>
<body style="margin:0;padding:24px;background:#ffffff;color:#1a1a1a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;font-size:16px;line-height:1.5">
<div style="max-width:560px;margin:0 auto">
<p style="margin:0 0 16px;white-space:pre-line">{{.body}}</p>
{{with index . "link"}}<p style="margin:0"><a href="{{.}}" style="color:#1a1a1a">{{or (index $ "link_label") .}}</a></p>{{end}}
</div>
</body>
</html>
`

// NeutralTemplate is QNTX's own mail: a subject, a body, and a link when the
// send names one. It says nothing about who sent it beyond what the plugin
// fills in.
//
// Values: subject and body are required; link and link_label are not. index
// reads a value without requiring it, so a send may leave those two out.
func NeutralTemplate() *protocol.MailTemplate {
	return &protocol.MailTemplate{
		Subject: `{{.subject}}`,
		Bodies:  []*protocol.MailTemplateBody{TemplateText(neutralText), TemplateHTML(neutralHTML)},
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
		Bodies:  []*protocol.MailTemplateBody{TemplateText(neutralText), TemplateHTML(html)},
	}, nil
}

// TemplateHTML is a template's html body.
func TemplateHTML(source string) *protocol.MailTemplateBody {
	return &protocol.MailTemplateBody{Is: &protocol.MailTemplateBody_Html{Html: source}}
}

// TemplateText is a template's text body.
func TemplateText(source string) *protocol.MailTemplateBody {
	return &protocol.MailTemplateBody{Is: &protocol.MailTemplateBody_Text{Text: source}}
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

// MailBodyKind is what a body a mail is sent in is: its html or its text.
type MailBodyKind string

const (
	MailHTML MailBodyKind = "html"
	MailText MailBodyKind = "text"
)

// MailBody is one body a mail is sent in: what it is, and what it holds.
type MailBody struct {
	Kind    MailBodyKind
	Content string
}

// HTMLBody is a mail's html body.
func HTMLBody(content string) MailBody {
	return MailBody{Kind: MailHTML, Content: content}
}

// TextBody is a mail's text body.
func TextBody(content string) MailBody {
	return MailBody{Kind: MailText, Content: content}
}

// mailBodies are the bodies a mail is sent in, at least one and no kind
// twice: a mail naming two html bodies would go out in one of them.
func mailBodies(what string, bodies []MailBody) (named.Items[MailBody], error) {
	listed, err := named.ItemsOf(what, bodies)
	if err != nil {
		return named.Items[MailBody]{}, err
	}
	seen := map[MailBodyKind]bool{}
	for _, b := range listed.All() {
		if seen[b.Kind] {
			return named.Items[MailBody]{}, errors.Newf("%s names its %s body twice", what, b.Kind)
		}
		seen[b.Kind] = true
	}
	return listed, nil
}

// BodyOf is the body of a kind among a mail's bodies, or that the mail is not
// sent in one.
func BodyOf(bodies []MailBody, kind MailBodyKind) (string, error) {
	for _, b := range bodies {
		if b.Kind == kind {
			return b.Content, nil
		}
	}
	return "", errors.Newf("the mail is not sent in a %s body", kind)
}

// FilledMail is a template filled with a send's values.
type FilledMail struct {
	Subject string
	Bodies  named.Items[MailBody]
}

// parsedMail is a template's parts, parsed, and the template they were parsed
// from, which is what is kept. A value a part names and a Send does not hold
// is an error: missingkey=error on every part.
type parsedMail struct {
	source  *protocol.MailTemplate
	subject *texttemplate.Template
	bodies  named.Items[mailPart]
}

// mailPart is one body a mail is sent in, parsed: filled, it is that body.
type mailPart struct {
	fill func(values map[string]string) (MailBody, error)
}

// parseMail is a template's parts, parsed, or why it cannot be kept: no
// subject, no body, a body named twice or holding nothing, or a part that does
// not parse. A request that sent no template sent no subject either. A
// template is kept as it was sent, and a Send parses it again from the record.
func parseMail(t *protocol.MailTemplate) (parsedMail, error) {
	subjectSource, err := named.TextOf("a template's subject", strings.TrimSpace(t.GetSubject()))
	if err != nil {
		return parsedMail{}, err
	}
	subject, err := texttemplate.New("subject").Option("missingkey=error").Parse(subjectSource.String())
	if err != nil {
		return parsedMail{}, errors.Wrap(err, "the subject does not parse")
	}

	var parts []mailPart
	seen := map[MailBodyKind]bool{}
	for i, body := range t.GetBodies() {
		part, kind, err := parseBody(i, body)
		if err != nil {
			return parsedMail{}, err
		}
		if seen[kind] {
			return parsedMail{}, errors.Newf("body %d names the template's %s body twice", i, kind)
		}
		seen[kind] = true
		parts = append(parts, part)
	}
	bodies, err := named.ItemsOf("a template's bodies (its html, its text, or both)", parts)
	if err != nil {
		return parsedMail{}, err
	}
	return parsedMail{source: t, subject: subject, bodies: bodies}, nil
}

// parseBody is one of a template's bodies, parsed, and what it is.
func parseBody(i int, body *protocol.MailTemplateBody) (mailPart, MailBodyKind, error) {
	switch is := body.GetIs().(type) {
	case *protocol.MailTemplateBody_Html:
		source, err := named.TextOf(fmt.Sprintf("the html of body %d", i), strings.TrimSpace(is.Html))
		if err != nil {
			return mailPart{}, MailHTML, err
		}
		parsed, err := htmltemplate.New("html").Option("missingkey=error").Parse(source.String())
		if err != nil {
			return mailPart{}, MailHTML, errors.Wrapf(err, "the html of body %d does not parse", i)
		}
		return mailPart{fill: func(values map[string]string) (MailBody, error) {
			var out bytes.Buffer
			if err := parsed.Execute(&out, values); err != nil {
				return MailBody{}, errors.Wrap(err, "the html could not be filled")
			}
			return HTMLBody(out.String()), nil
		}}, MailHTML, nil
	case *protocol.MailTemplateBody_Text:
		source, err := named.TextOf(fmt.Sprintf("the text of body %d", i), strings.TrimSpace(is.Text))
		if err != nil {
			return mailPart{}, MailText, err
		}
		parsed, err := texttemplate.New("text").Option("missingkey=error").Parse(source.String())
		if err != nil {
			return mailPart{}, MailText, errors.Wrapf(err, "the text of body %d does not parse", i)
		}
		return mailPart{fill: func(values map[string]string) (MailBody, error) {
			filled, err := fillText("text", parsed, values)
			return TextBody(filled), err
		}}, MailText, nil
	}
	return mailPart{}, "", errors.Newf("body %d names neither html nor text", i)
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

	var filled []MailBody
	for _, part := range parsed.bodies.All() {
		body, err := part.fill(values)
		if err != nil {
			return FilledMail{}, err
		}
		filled = append(filled, body)
	}
	bodies, err := named.ItemsOf("the filled bodies", filled)
	if err != nil {
		return FilledMail{}, err
	}
	return FilledMail{Subject: filledSubject.String(), Bodies: bodies}, nil
}

func fillText(part string, parsed *texttemplate.Template, values map[string]string) (string, error) {
	var out bytes.Buffer
	if err := parsed.Execute(&out, values); err != nil {
		return "", errors.Wrapf(err, "the %s could not be filled", part)
	}
	return out.String(), nil
}
