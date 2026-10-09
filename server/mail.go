package server

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
)

// Mail is what the node mails on plugins' behalf (ADR-041): the service
// plugins call, and the signum ROOT watches it through.
//
// "that is a ROOT question about governance and controls and monitoring that belongs in its own window element in the tray like others"

const (
	mailPath          = "/api/mail"
	mailTemplatesPath = "/api/mail/templates"
	mailAccountPath   = "/api/mail/account"
	mailReportPath    = "/api/mail/report"
	mailMessagePath   = "/api/mail/message"

	// mailSentByDefault is how many mails the window is handed when it names
	// no limit.
	mailSentByDefault = 100
)

// mailWiring is what the mail service sends with: the address and transport
// am.toml names, the node's Users, where it attests, and the node as the one
// who sent. What it was wired with is kept, so the window says what sends.
func (s *QNTXServer) mailWiring() services.MailWiring {
	s.mailConfig = s.deps.cfg.Mail
	w := services.MailWiring{
		From:    s.mailConfig.From,
		Records: s.mailRecords,
		Actor:   s.nodeDIDOrUnknown(),
	}
	if s.mailConfig.SES.Enabled {
		w.Transport = services.SESTransport{Region: s.mailConfig.SES.Region}
	}
	if s.authHandler != nil {
		w.Recipients = s.mailRecipient
	}
	s.logger.Infow("Mail service wired",
		"ses", s.mailConfig.SES.Enabled,
		"ses_region", s.mailConfig.SES.Region,
		"from", s.mailConfig.From,
		"keeps_users", s.authHandler != nil)
	return w
}

// mailRecords is where the mail service keeps the templates it fills and the
// mail it sent: with what the node knows of itself, because a mail names a User
// and no User is visible below SUPER (ADR-031).
func (s *QNTXServer) mailRecords() ats.AttestationStore {
	return s.held.TheNodesOwnRecords()
}

// mailRecipient is a User as the mail service reads one: their primary address,
// and whether they are switched off.
func (s *QNTXServer) mailRecipient(id string) (services.MailRecipient, bool, error) {
	u, found, err := s.authHandler.UserByID(id)
	if err != nil || !found {
		return services.MailRecipient{}, found, err
	}
	return services.MailRecipient{ID: u.ID, Email: u.PrimaryEmail(), DisabledBy: u.DisabledBy}, true, nil
}

// nodeDIDOrUnknown is who the node is when it writes as itself.
func (s *QNTXServer) nodeDIDOrUnknown() string {
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		return "did:key:unknown"
	}
	return s.nodeDID.DID
}

func (s *QNTXServer) mailSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "mail",
			Description: "Mail the node sends on plugins' behalf: every mail sent or refused, one mail whole, the templates mail is filled from, the account it goes through, and the weekly report to ROOT.",
			Tags:        []string{"mail", "templates", "report"},
			Sigils: []*protocol.Sigil{
				{
					Name: "sent",
					Does: "Every mail the node sent on a plugin's behalf, and every one the transport refused, newest first.",
					Takes: []*protocol.Param{{Name: "limit", Kind: sigil.Count,
						Says: "How many mails at most. Naming none is one hundred."}},
					Answer: "protocol.MailSent",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: mailPath},
				},
				{
					Name:   "templates",
					Does:   "The templates mail is filled from: QNTX's neutral and dark ones, and the newest each plugin set under each name.",
					Answer: "protocol.MailTemplates",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: mailTemplatesPath},
				},
				{
					Name:   "account",
					Does:   "What the node sends mail through: the address it sends from, whether SES is enabled in am.toml, and what SES says of the account now.",
					Answer: "protocol.MailAccount",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: mailAccountPath},
				},
				{
					Name:   "report",
					Does:   "Send the weekly report to the ROOT User now: the one the node sends on its own schedule (ADR-042).",
					Answer: "protocol.MailReport",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: mailReportPath},
				},
				{
					Name: "message",
					Does: "One mail whole, as it was sent: who it went to and from, its subject, its html and text, and the images the html shows.",
					Takes: []*protocol.Param{
						{Name: "id", Required: true, Says: "The mail's attestation, as the sent list names it."},
						{Name: "user", Required: true, Says: "The User it went to, as the sent list names it."},
					},
					Answer: "protocol.MailMessage",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: mailMessagePath},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"sent":      s.mailSent,
			"templates": s.mailTemplates,
			"account":   s.mailAccount,
			"report":    s.mailReport,
			"message":   s.mailMessage,
		},
	}
}

func (s *QNTXServer) mailSent(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	limit := mailSentByDefault
	if said := sent["limit"]; said != "" {
		n, err := strconv.Atoi(said)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "limit", Says: "limit is a count, and " + said + " is not one"}
		}
		limit = min(n, storage.MaxAttestationLimit)
	}

	store := s.mailRecords()
	if store == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no store the mail service attests into"}
	}

	// One query per predicate: a store may read several in one filter as all
	// of them rather than any (staand.go).
	// Ordered by the time itself: the written time orders by whatever offset
	// it carries.
	type mail struct {
		at  time.Time
		row *protocol.MailRow
	}
	var mails []mail
	for _, predicate := range []string{services.PredicateMailSent, services.PredicateMailFailed} {
		found, err := store.GetAttestations(ats.AttestationFilter{Predicates: []string{predicate}, Limit: limit})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the mail " + predicate + " could not be read: " + err.Error()}
		}
		for _, as := range found {
			if !slices.Contains(as.Predicates, predicate) {
				continue
			}
			mails = append(mails, mail{at: as.Timestamp, row: mailRowOf(as, predicate == services.PredicateMailSent)})
		}
	}
	slices.SortFunc(mails, func(a, b mail) int { return b.at.Compare(a.at) })
	if len(mails) > limit {
		mails = mails[:limit]
	}
	answer := &protocol.MailSent{Mails: make([]*protocol.MailRow, 0, len(mails))}
	for _, m := range mails {
		answer.Mails = append(answer.Mails, m.row)
	}
	return answer, nil
}

func mailRowOf(as *types.As, sent bool) *protocol.MailRow {
	row := &protocol.MailRow{
		Id:        as.ID,
		At:        as.Timestamp.Format(time.RFC3339Nano),
		To:        attr(as, "to"),
		Plugin:    attr(as, "plugin"),
		Template:  attr(as, "template"),
		Subject:   attr(as, "subject"),
		Sent:      sent,
		MessageId: attr(as, "message_id"),
		Error:     attr(as, "error"),
	}
	if len(as.Subjects) > 0 {
		row.User = as.Subjects[0]
	}
	return row
}

func (s *QNTXServer) mailTemplates(_ context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	neutral := services.NeutralTemplate()
	dark, err := services.DarkTemplate()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the dark template could not be drawn: " + err.Error()}
	}
	// Both take what the neutral template takes: the dark one is it, drawn as a window.
	values := []string{"subject", "body", "link (not required)", "link_label (not required)"}
	// QNTX's own were never set, so their time is none: Go's zero time, as
	// the view has always been given it.
	var never time.Time
	answer := &protocol.MailTemplates{
		Neutral: &protocol.MailTemplateRow{
			At: never.Format(time.RFC3339Nano), Plugin: "qntx", Name: services.NeutralTemplateName,
			Subject: neutral.Subject, Html: neutral.Html, Text: neutral.Text,
			Values: values,
		},
		Dark: &protocol.MailTemplateRow{
			At: never.Format(time.RFC3339Nano), Plugin: "qntx", Name: services.DarkTemplateName,
			Subject: dark.Subject, Html: dark.Html, Text: dark.Text,
			Values: values,
		},
		Node: []*protocol.NodeMail{{
			Name: reportHandlerName,
			Says: "The weekly report to the ROOT User (ADR-042), written whole by the node. What it looked like is the mail itself, under Sent.",
		}},
	}

	store := s.mailRecords()
	if store == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no store the mail service attests into"}
	}
	found, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{services.PredicateMailTemplate},
		Limit:      ats.EveryRow,
	})
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the mail templates could not be read: " + err.Error()}
	}

	// The newest under each plugin and name is the one a mail is filled from.
	newest := map[string]*types.As{}
	for _, as := range found {
		if !slices.Contains(as.Predicates, services.PredicateMailTemplate) || len(as.Subjects) == 0 {
			continue
		}
		ref := as.Subjects[0]
		if held, ok := newest[ref]; !ok || as.Timestamp.After(held.Timestamp) {
			newest[ref] = as
		}
	}
	templates := []*protocol.MailTemplateRow{}
	for _, as := range newest {
		kept := services.TemplateOf(as)
		templates = append(templates, &protocol.MailTemplateRow{
			Id: as.ID, At: as.Timestamp.Format(time.RFC3339Nano),
			Plugin: attr(as, "plugin"), Version: attr(as, "source_version"), Name: attr(as, "name"),
			Subject: kept.Subject, Html: kept.Html, Text: kept.Text,
			Values: []string{},
		})
	}
	slices.SortFunc(templates, func(a, b *protocol.MailTemplateRow) int {
		if a.GetPlugin() != b.GetPlugin() {
			if a.GetPlugin() < b.GetPlugin() {
				return -1
			}
			return 1
		}
		if a.GetName() < b.GetName() {
			return -1
		}
		if a.GetName() > b.GetName() {
			return 1
		}
		return 0
	})
	answer.Templates = templates
	return answer, nil
}

func (s *QNTXServer) mailAccount(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	ses := &protocol.MailSES{Enabled: s.mailConfig.SES.Enabled, Region: s.mailConfig.SES.Region}
	answer := &protocol.MailAccount{From: s.mailConfig.From, Ses: ses}
	if !ses.GetEnabled() {
		answer.Unanswered = "SES is not enabled: mail.ses.enabled is false in am.toml, so no mail is sent"
		return answer, nil
	}
	account, err := services.SESTransport{Region: ses.GetRegion()}.Account(ctx)
	if err != nil {
		// SES not answering is what this window is for; it is said, not refused.
		s.logger.Warnw("SES did not say what the account is", "region", ses.GetRegion(), "error", err)
		answer.Unanswered = err.Error()
		return answer, nil
	}
	answer.Account = &protocol.SESAccount{
		Region: account.Region, ProductionAccess: account.ProductionAccess, SendingEnabled: account.SendingEnabled,
		EnforcementStatus: account.EnforcementStatus, Max_24HourSend: account.Max24HourSend,
		MaxSendRate: account.MaxSendRate, SentLast_24Hours: account.SentLast24Hours,
	}
	return answer, nil
}

// "i would have expected to be able to click the main and see exactly what was sent."
func (s *QNTXServer) mailMessage(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	store := s.mailRecords()
	if store == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no store the mail service attests into"}
	}
	id, user := sent["id"], sent["user"]
	for _, predicate := range []string{services.PredicateMailSent, services.PredicateMailFailed} {
		found, err := store.GetAttestations(ats.AttestationFilter{
			Predicates: []string{predicate},
			Subjects:   []string{user},
			Limit:      ats.EveryRow,
		})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the mail to " + user + " could not be read: " + err.Error()}
		}
		for _, as := range found {
			if as.ID != id || !slices.Contains(as.Predicates, predicate) {
				continue
			}
			row := mailRowOf(as, predicate == services.PredicateMailSent)
			return &protocol.MailMessage{Mail: &protocol.MailWhole{
				Id: row.GetId(), At: row.GetAt(), User: row.GetUser(), To: row.GetTo(), Plugin: row.GetPlugin(),
				Template: row.GetTemplate(), Subject: row.GetSubject(), Sent: row.GetSent(),
				MessageId: row.GetMessageId(), Error: row.GetError(),
				From: attr(as, "from"), Html: attr(as, "html"), Text: attr(as, "text"), Images: imagesOf(as),
			}}, nil
		}
	}
	return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "id", Says: "no mail " + id + " to " + user}
}

// imagesOf reads back the images a mail was sent with. None is a mail that had
// none, or one attested before they were kept.
func imagesOf(as *types.As) []*protocol.MailKeptImage {
	images := []*protocol.MailKeptImage{}
	kept, ok := as.Attributes["images"].([]any)
	if !ok {
		return images
	}
	for _, entry := range kept {
		img, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		images = append(images, &protocol.MailKeptImage{
			ContentId:   attrOf(img, "content_id"),
			ContentType: attrOf(img, "content_type"),
			Data:        attrOf(img, "data"),
		})
	}
	return images
}

func attrOf(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (s *QNTXServer) mailReport(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	sent, err := s.sendReport(ctx)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return sent, nil
}

// attr is one string attribute of an attestation, or empty.
func attr(as *types.As, key string) string {
	if v, ok := as.Attributes[key].(string); ok {
		return v
	}
	return ""
}
