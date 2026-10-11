package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// MailServer implements MailService (ADR-041). A plugin names a User and a
// template; QNTX finds the User's primary address, fills the template, sends
// the mail and attests it. A plugin never holds an address or a credential to
// send with.
//
// "this is an auth question, deferred, assume only ROOT has governance over gRPC calls"
//
// So the service token is the whole gate here, as it is for every service a
// plugin calls, and source is what the plugin says it is.

// Predicates for what the mail service does. Written to where the node keeps
// what it knows of itself, because a mail names a User, and no User is visible
// below SUPER (ADR-031).
const (
	PredicateMailTemplate = "mail:template"
	PredicateMailSent     = "mail:sent"
	// A mail the transport refused. It was still a mail the node tried to send.
	PredicateMailFailed = "mail:failed"
)

// MailRecipient is who a mail goes to, as the mail service reads them: the
// address mail to them goes to, or why no mail goes to them.
type MailRecipient struct {
	ID         string
	address    named.Text
	unsendable error
}

// UserRecipient is a User as mail reads one: their primary address, unless
// they are switched off or gave none (ADR-041).
func UserRecipient(u access.User) MailRecipient {
	if u.SwitchedOff() {
		return MailRecipient{ID: u.ID, unsendable: errors.Newf("User %s is switched off by %s", u.ID, u.DisabledBy)}
	}
	address, err := named.TextOf("the email address of User "+u.ID, u.PrimaryEmail())
	return MailRecipient{ID: u.ID, address: address, unsendable: errors.Wrapf(err, "User %s has no email address", u.ID)}
}

// AddressRecipient is someone mail reaches at an address they gave, under the
// id the mail is recorded about: an invitation, before there is a User.
func AddressRecipient(id, email string) MailRecipient {
	address, err := named.TextOf("the email address of "+id, email)
	return MailRecipient{ID: id, address: address, unsendable: err}
}

// Address is where mail to them goes, or why none does.
func (r MailRecipient) Address() (named.Text, error) {
	return r.address, r.unsendable
}

// MailRecipients finds one User by id. False is no User holding it.
type MailRecipients func(userID string) (MailRecipient, bool, error)

// OutgoingMail is a filled mail, addressed, and the bodies it is sent in.
type OutgoingMail struct {
	From    string
	To      string
	Subject string
	Bodies  named.Items[MailBody]
	Inline  []InlineImage
}

// InlineImage is an image the html shows by cid:<ContentID>.
type InlineImage struct {
	ContentID   string
	ContentType string
	FileName    string
	Data        []byte
}

// NodeMail is a mail the node writes itself, whole (ADR-042): no plugin, and
// no template to fill. Name is what the node calls it, recorded where a
// plugin's mail records its template. Bodies are what it is sent in, each
// named; a node mail naming none is refused.
type NodeMail struct {
	Name    string
	Subject string
	Bodies  []MailBody
	Inline  []InlineImage
}

// NodeSource is the source of what the node mails in its own name.
const NodeSource = "qntx"

// MailTransport delivers a mail and names the id it was given.
type MailTransport interface {
	Send(ctx context.Context, m OutgoingMail) (messageID string, err error)
}

// MailWiring is what the node hands the mail service once it has it: the
// Users exist after auth, the node's DID after nodedid, and plugins may call
// before either. What the node runs without is had with why.
type MailWiring struct {
	From       Had[named.Text]                      // mail.from
	Transport  Had[MailTransport]                   // the transport am.toml enables
	Recipients Had[MailRecipients]                  // the node's Users
	Records    func() (ats.AttestationStore, error) // where templates and mail are attested
	Actor      Had[named.Text]                      // the node's DID: who sent the mail
}

// MailFrom is mail.from, or that am.toml names none, so nothing is sent.
func MailFrom(from string) Had[named.Text] {
	address, err := named.TextOf("mail.from in am.toml", from)
	if err != nil {
		return HavingNot[named.Text](errors.Wrap(err, "no address to send from: set mail.from in am.toml"))
	}
	return Having(address)
}

// MailActor is the node's DID, who writes what the mail service records.
func MailActor(did string) Had[named.Text] {
	actor, err := named.TextOf("the node's DID", did)
	if err != nil {
		return HavingNot[named.Text](errors.Wrap(err, "the mail service was wired without the node, so nobody writes what it records"))
	}
	return Having(actor)
}

// MailServer is the MailService gRPC server.
type MailServer struct {
	protocol.UnimplementedMailServiceServer
	authToken       string
	logger          *zap.SugaredLogger
	wired           atomic.Pointer[mailWired]
	versionResolver atomic.Pointer[VersionResolver]
}

// mailWired is what the service sends with, or why it has nothing yet.
type mailWired struct {
	wiring MailWiring
	err    error // the service not wired yet
}

// NewMailServer creates the mail service. It sends nothing until Wire.
func NewMailServer(authToken string, logger *zap.SugaredLogger) *MailServer {
	s := &MailServer{authToken: authToken, logger: logger}
	s.wired.Store(&mailWired{err: errors.New("the mail service is not wired yet: the node has not finished starting")})
	s.SetVersionResolver(noVersions)
	return s
}

// Wire hands the service what it sends with.
func (s *MailServer) Wire(w MailWiring) {
	s.wired.Store(&mailWired{wiring: w})
}

// wiredWith is what Wire handed the service, or that it has not been yet.
func (s *MailServer) wiredWith() (*MailWiring, error) {
	wired := s.wired.Load()
	if err := wired.err; err != nil {
		return nil, err
	}
	return &wired.wiring, nil
}

// SetVersionResolver stamps source_version on what the service attests.
func (s *MailServer) SetVersionResolver(resolver VersionResolver) {
	s.versionResolver.Store(&resolver)
}

// SetTemplate keeps one of a plugin's templates under a name, attested.
func (s *MailServer) SetTemplate(_ context.Context, req *protocol.SetMailTemplateRequest) (*protocol.SetMailTemplateResponse, error) {
	refuse := func(err error) (*protocol.SetMailTemplateResponse, error) {
		s.logger.Warnw("Mail template not kept", "plugin", req.Source, "name", req.Name, "error", err)
		return &protocol.SetMailTemplateResponse{Success: false, Error: err.Error()}, nil
	}

	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return refuse(err)
	}
	source, err := named.TextOf("source, the plugin the template belongs to,", req.Source)
	if err != nil {
		return refuse(err)
	}
	name, err := named.TextOf("name", req.Name)
	if err != nil {
		return refuse(err)
	}
	if isQNTXTemplate(name.String()) {
		return refuse(errors.Newf("%s is one of QNTX's own templates, which a Send names without setting it", name))
	}
	parsed, err := parseMail(req.Template)
	if err != nil {
		return refuse(errors.Wrapf(err, "template %s of %s", name, source))
	}

	w, err := s.wiredWith()
	if err != nil {
		return refuse(errors.Wrap(err, "the mail service has nowhere to keep a template yet"))
	}
	store, err := w.Records()
	if err != nil {
		return refuse(errors.Wrapf(err, "template %s of %s was not kept", name, source))
	}

	ref := templateRef(source.String(), name.String())
	kept := parsed.source
	// The bodies are kept as the named list they were sent as.
	bodies := make([]any, 0, len(kept.GetBodies()))
	for i, body := range kept.GetBodies() {
		entry, err := keptBody(i, body)
		if err != nil {
			return refuse(errors.Wrapf(err, "template %s", ref))
		}
		bodies = append(bodies, entry)
	}
	id, err := s.attest(store, w.Actor, ref, PredicateMailTemplate, source.String(), source.String(), map[string]any{
		"plugin":  source.String(),
		"name":    name.String(),
		"subject": kept.GetSubject(),
		"bodies":  bodies,
	})
	if err != nil {
		return refuse(errors.Wrapf(err, "template %s was not attested", ref))
	}

	s.logger.Infow("Mail template kept", "template", ref, "attestation", id)
	return &protocol.SetMailTemplateResponse{Success: true, AttestationId: id}, nil
}

// attest writes one record of the mail service, as the node: the node's own
// records are written whole, with the node as their actor, the way auth writes
// what happens at the door.
func (s *MailServer) attest(store ats.AttestationStore, node Had[named.Text], subject, predicate, context, source string, attrs map[string]any) (string, error) {
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, context, store.AttestationExists)
	if err != nil {
		return "", errors.Wrapf(err, "no id could be minted for %s of %s", predicate, subject)
	}
	actor, err := node.Get()
	if err != nil {
		return "", errors.Wrapf(err, "%s of %s is not written", predicate, subject)
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{context},
		Actors:     []string{actor.String()},
		Timestamp:  now,
		Source:     source,
		Attributes: s.stamped(source, attrs),
		CreatedAt:  now,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// Send mails one User from a template, and attests what was sent.
func (s *MailServer) Send(ctx context.Context, req *protocol.SendMailRequest) (*protocol.SendMailResponse, error) {
	refuse := func(err error) (*protocol.SendMailResponse, error) {
		s.logger.Warnw("Mail not sent", "plugin", req.Source, "user", req.UserId, "template", req.Template, "error", err)
		return &protocol.SendMailResponse{Success: false, Error: err.Error()}, nil
	}

	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return refuse(err)
	}
	source, err := named.TextOf("source, the plugin sending,", req.Source)
	if err != nil {
		return refuse(err)
	}
	user, err := named.TextOf("user_id, the User mail goes to,", req.UserId)
	if err != nil {
		return refuse(err)
	}
	// QNTX's neutral template is named like any other.
	name, err := named.TextOf("template (QNTX's own are "+NeutralTemplateName+" and "+DarkTemplateName+")", req.Template)
	if err != nil {
		return refuse(err)
	}

	w, to, err := s.ready(user.String())
	if err != nil {
		return refuse(err)
	}

	template, ref, err := s.template(w, source.String(), name.String())
	if err != nil {
		return refuse(err)
	}
	filled, err := fillMail(template, req.Values)
	if err != nil {
		return refuse(errors.Wrapf(err, "template %s", ref))
	}

	inline, err := inlineImages(req.Inline)
	if err != nil {
		return refuse(err)
	}

	mail := OutgoingMail{From: w.from.String(), To: to.String(), Subject: filled.Subject, Bodies: filled.Bodies, Inline: inline}
	messageID, attestationID, sendErr := s.deliver(ctx, w, user.String(), source.String(), ref, mail)
	if sendErr != nil {
		return &protocol.SendMailResponse{Success: false, Error: sendErr.Error(), MessageId: messageID, AttestationId: attestationID}, nil //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
	}
	return &protocol.SendMailResponse{Success: true, MessageId: messageID, AttestationId: attestationID}, nil
}

// MaxMailImageBytes caps what a plugin's mail carries in images, all of them
// together. The mail service's gRPC server takes the default 4 MiB a message,
// and the rest of the request has to fit beside the images.
const MaxMailImageBytes = 3 << 20

// pngSignature is the eight bytes every PNG begins with.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// inlineImages is what a plugin's mail carries inline, or why it carries none:
// image/png only, each under a cid of its own, all of them under the cap.
func inlineImages(images []*protocol.MailImage) ([]InlineImage, error) {
	var inline []InlineImage
	seen := map[string]bool{}
	total := 0
	for i, img := range images {
		cid, err := named.TextOf(fmt.Sprintf("the content_id of inline image %d, by which the html shows it as cid:<content_id>,", i), img.ContentId)
		if err != nil {
			return nil, err
		}
		if seen[cid.String()] {
			return nil, errors.Newf("inline image %d repeats content_id %s", i, cid)
		}
		seen[cid.String()] = true
		if img.ContentType != "image/png" {
			return nil, errors.Newf("inline image %s is %q: only image/png is accepted", cid, img.ContentType)
		}
		if !bytes.HasPrefix(img.Data, pngSignature) {
			return nil, errors.Newf("inline image %s is not a PNG: its %d bytes do not begin with the PNG signature", cid, len(img.Data))
		}
		total += len(img.Data)
		if total > MaxMailImageBytes {
			return nil, errors.Newf("inline images come to more than %d bytes by image %s", MaxMailImageBytes, cid)
		}
		fileName, err := named.TextOf("the file_name of inline image "+cid.String(), img.FileName)
		if err != nil {
			return nil, err
		}
		inline = append(inline, InlineImage{ContentID: cid.String(), ContentType: img.ContentType, FileName: fileName.String(), Data: img.Data})
	}
	return inline, nil
}

// SendAsNode mails a User something the node wrote itself (ADR-042). It is
// held to what a plugin's mail is held to — a transport, an address to send
// from, a User who is on and has an address — and attested the same way.
func (s *MailServer) SendAsNode(ctx context.Context, userID string, m NodeMail) (messageID, attestationID string, err error) {
	bodies, err := mailBodies("node mail "+m.Name, m.Bodies)
	if err != nil {
		s.logger.Warnw("Node mail not sent", "mail", m.Name, "user", userID, "error", err)
		return "", "", err
	}
	w, to, err := s.ready(userID)
	if err != nil {
		s.logger.Warnw("Node mail not sent", "mail", m.Name, "user", userID, "error", err)
		return "", "", err
	}
	mail := OutgoingMail{From: w.from.String(), To: to.String(), Subject: m.Subject, Bodies: bodies, Inline: m.Inline}
	return s.deliver(ctx, w, userID, NodeSource, m.Name, mail)
}

// SendAsNodeTo is SendAsNode to a User read before, so a mail saying the store
// the Users are in does not answer is not held up behind that store.
func (s *MailServer) SendAsNodeTo(ctx context.Context, u MailRecipient, m NodeMail) (messageID, attestationID string, err error) {
	bodies, err := mailBodies("node mail "+m.Name, m.Bodies)
	if err != nil {
		s.logger.Warnw("Node mail not sent", "mail", m.Name, "user", u.ID, "error", err)
		return "", "", err
	}
	w, err := s.wiring()
	if err != nil {
		s.logger.Warnw("Node mail not sent", "mail", m.Name, "user", u.ID, "error", err)
		return "", "", err
	}
	to, err := u.Address()
	if err != nil {
		s.logger.Warnw("Node mail not sent", "mail", m.Name, "user", u.ID, "error", err)
		return "", "", err
	}
	mail := OutgoingMail{From: w.from.String(), To: to.String(), Subject: m.Subject, Bodies: bodies, Inline: m.Inline}
	return s.deliver(ctx, w, u.ID, NodeSource, m.Name, mail)
}

// ready is the wiring a send needs and the address it goes to, or why there is
// none.
func (s *MailServer) ready(userID string) (*mailSending, named.Text, error) {
	w, err := s.wiring()
	if err != nil {
		return nil, named.Text{}, err
	}
	to, err := s.recipient(w, userID)
	if err != nil {
		return nil, named.Text{}, err
	}
	return w, to, nil
}

// mailSending is what a send goes out by, read once when the send begins: the
// address it is from, the transport, the Users, the store its record is kept
// in, and the node who records it.
type mailSending struct {
	from       named.Text
	transport  MailTransport
	recipients MailRecipients
	store      ats.AttestationStore
	actor      Had[named.Text]
}

// wiring is what a send needs besides the User, or why there is none.
func (s *MailServer) wiring() (*mailSending, error) {
	w, err := s.wiredWith()
	if err != nil {
		return nil, err
	}
	transport, err := w.Transport.Get()
	if err != nil {
		return nil, err
	}
	from, err := w.From.Get()
	if err != nil {
		return nil, err
	}
	recipients, err := w.Recipients.Get()
	if err != nil {
		return nil, err
	}
	store, err := w.Records()
	if err != nil {
		return nil, errors.Wrap(err, "no mail is sent that cannot be attested")
	}
	return &mailSending{from: from, transport: transport, recipients: recipients, store: store, actor: w.Actor}, nil
}

// deliver hands a filled mail to the transport and attests what became of it:
// mail:sent with the transport's id, or mail:failed with its refusal.
func (s *MailServer) deliver(ctx context.Context, w *mailSending, userID, source, ref string, mail OutgoingMail) (string, string, error) {
	messageID, sendErr := w.transport.Send(ctx, mail)

	attrs := map[string]any{
		"plugin":   source,
		"template": ref,
		"to":       mail.To,
		"from":     mail.From,
		"subject":  mail.Subject,
	}
	// Each body the mail went out in is kept under what it is.
	for _, body := range mail.Bodies.All() {
		attrs[string(body.Kind)] = body.Content
	}
	// "i would have expected to be able to click the main and see exactly what was sent."
	//
	// The images are kept whole beside the html that shows them, so the mail
	// can be shown again as it went out, graphs and all.
	// A mail that carried none keeps none.
	images := make([]any, 0, len(mail.Inline))
	for _, img := range mail.Inline {
		sum := sha256.Sum256(img.Data)
		images = append(images, map[string]any{
			"content_id":   img.ContentID,
			"content_type": img.ContentType,
			"file_name":    img.FileName,
			"sha256":       hex.EncodeToString(sum[:]),
			"data":         base64.StdEncoding.EncodeToString(img.Data),
		})
	}
	attrs["images"] = images
	predicate := PredicateMailSent
	if sendErr != nil {
		predicate = PredicateMailFailed
		attrs["error"] = sendErr.Error()
	} else {
		attrs["message_id"] = messageID
	}

	attestationID, attestErr := s.attest(w.store, w.actor, userID, predicate, ref, source, attrs)
	if attestErr != nil {
		// The mail left or failed either way; what is lost is the record of it,
		// and that is said with everything the record would have held, and
		// answered: no mail is sent that cannot be attested.
		s.logger.Errorw("Mail not attested: the store refused it",
			"predicate", predicate, "user", userID, "template", ref,
			"message_id", messageID, "attributes", attrs, "error", attestErr, "transport_error", sendErr)
		return messageID, "", errors.Wrapf(attestErr, "the %s of mail to User %s is not attested", predicate, userID)
	}

	if sendErr != nil {
		s.logger.Warnw("Mail refused by the transport",
			"source", source, "user", userID, "template", ref, "attestation", attestationID, "error", sendErr)
		return "", attestationID, errors.Wrapf(sendErr, "mail to User %s was not sent", userID)
	}

	s.logger.Infow("Mail sent",
		"source", source, "user", userID, "template", ref,
		"message_id", messageID, "attestation", attestationID)
	return messageID, attestationID, nil
}

// recipient is the address a User's mail goes to, or why there is none.
func (s *MailServer) recipient(w *mailSending, userID string) (named.Text, error) {
	u, found, err := w.recipients(userID)
	if err != nil {
		return named.Text{}, errors.Wrapf(err, "User %s could not be read", userID)
	}
	if !found {
		return named.Text{}, errors.Newf("no User %s", userID)
	}
	return u.Address()
}

// template is what a Send is filled from: one of QNTX's own by its name, else
// the newest the plugin set under that name.
func (s *MailServer) template(w *mailSending, plugin, name string) (*protocol.MailTemplate, string, error) {
	if isQNTXTemplate(name) {
		own, err := qntxTemplate(name)
		return own, name, err
	}
	ref := templateRef(plugin, name)
	kept, found, err := NewestMailTemplate(w.store, plugin, name)
	if err != nil {
		return nil, ref, err
	}
	if !found {
		return nil, ref, errors.Newf("plugin %s has set no template %s", plugin, name)
	}
	return kept, ref, nil
}

// NewestMailTemplate is the newest template a plugin set under a name.
func NewestMailTemplate(store ats.AttestationStore, plugin, name string) (*protocol.MailTemplate, bool, error) {
	ref := templateRef(plugin, name)
	kept, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects:   []string{ref},
		Predicates: []string{PredicateMailTemplate},
		Limit:      1000,
	})
	if err != nil {
		return nil, false, errors.Wrapf(err, "template %s could not be read", ref)
	}
	var newest *types.As
	found := false
	for _, as := range kept {
		// A store may match a subject loosely; a template is its exact name.
		if !slices.Contains(as.Subjects, ref) {
			continue
		}
		if !found || as.Timestamp.After(newest.Timestamp) {
			newest, found = as, true
		}
	}
	if !found {
		return nil, false, nil
	}
	template, err := TemplateOf(newest)
	if err != nil {
		return nil, false, err
	}
	return template, true, nil
}

// keptBody is one of a template's bodies as its record keeps it: what it is,
// and its source.
func keptBody(i int, body *protocol.MailTemplateBody) (map[string]any, error) {
	switch is := body.GetIs().(type) {
	case *protocol.MailTemplateBody_Html:
		return map[string]any{"kind": string(MailHTML), "source": is.Html}, nil
	case *protocol.MailTemplateBody_Text:
		return map[string]any{"kind": string(MailText), "source": is.Text}, nil
	}
	return nil, errors.Newf("body %d names neither html nor text", i)
}

// TemplateOf reads a template back out of the attestation it was kept as: its
// subject and the bodies it named. A template kept before its bodies were
// named is refused, saying so: set it again.
func TemplateOf(as *types.As) (*protocol.MailTemplate, error) {
	subject, ok := as.Attributes["subject"].(string)
	if !ok {
		return nil, errors.Newf("template record %s keeps no subject", as.ID)
	}
	listed, keptBodies := as.Attributes["bodies"]
	if !keptBodies {
		return nil, errors.Newf("template record %s was kept before a template's bodies were named: the plugin sets it again", as.ID)
	}
	bodies, ok := listed.([]any)
	if !ok {
		return nil, errors.Newf("template record %s keeps its bodies as %T, not a list", as.ID, listed)
	}
	template := &protocol.MailTemplate{Subject: subject}
	for i, entry := range bodies {
		body, err := readKeptBody(as.ID, i, entry)
		if err != nil {
			return nil, err
		}
		template.Bodies = append(template.Bodies, body)
	}
	return template, nil
}

// readKeptBody is one body a template record keeps, as the template named it.
func readKeptBody(id string, i int, entry any) (*protocol.MailTemplateBody, error) {
	kept, ok := entry.(map[string]any)
	if !ok {
		return nil, errors.Newf("template record %s keeps body %d as %T, not a body", id, i, entry)
	}
	kind, ok := kept["kind"].(string)
	if !ok {
		return nil, errors.Newf("template record %s keeps body %d without saying what it is", id, i)
	}
	source, ok := kept["source"].(string)
	if !ok {
		return nil, errors.Newf("template record %s keeps body %d without its source", id, i)
	}
	switch MailBodyKind(kind) {
	case MailHTML:
		return TemplateHTML(source), nil
	case MailText:
		return TemplateText(source), nil
	}
	return nil, errors.Newf("template record %s keeps body %d as %q, which is neither html nor text", id, i, kind)
}

// templateRef names a plugin's template: the plugin, then its name.
func templateRef(plugin, name string) string {
	return plugin + "/" + name
}

// stamped adds the version of the plugin a record came from.
func (s *MailServer) stamped(source string, attrs map[string]any) map[string]any {
	if version, known := (*s.versionResolver.Load())(source); known {
		attrs["source_version"] = version
	}
	return attrs
}
