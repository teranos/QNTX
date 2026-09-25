package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
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

// MailRecipient is what the mail service reads of a User.
type MailRecipient struct {
	ID         string
	Email      string // The User's primary address; empty is a User who gave none.
	DisabledBy string // Who switched the User off; empty is a User who is on.
}

// MailRecipients finds one User by id. False is no User holding it.
type MailRecipients func(userID string) (MailRecipient, bool, error)

// OutgoingMail is a filled mail, addressed.
type OutgoingMail struct {
	From    string
	To      string
	Subject string
	HTML    string
	Text    string
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
// plugin's mail records its template.
type NodeMail struct {
	Name    string
	Subject string
	HTML    string
	Text    string
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
// before either.
type MailWiring struct {
	From       string                      // mail.from. Empty sends nothing.
	Transport  MailTransport               // Nil is no transport enabled in am.toml.
	Recipients MailRecipients              // Nil is a node that keeps no Users.
	Records    func() ats.AttestationStore // Where templates and mail are attested.
	Actor      string                      // The node's DID: who sent the mail.
}

// MailServer is the MailService gRPC server.
type MailServer struct {
	protocol.UnimplementedMailServiceServer
	authToken       string
	logger          *zap.SugaredLogger
	wired           atomic.Pointer[MailWiring]
	versionResolver atomic.Pointer[VersionResolver]
}

// NewMailServer creates the mail service. It sends nothing until Wire.
func NewMailServer(authToken string, logger *zap.SugaredLogger) *MailServer {
	return &MailServer{authToken: authToken, logger: logger}
}

// Wire hands the service what it sends with.
func (s *MailServer) Wire(w MailWiring) {
	s.wired.Store(&w)
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
	if req.Source == "" {
		return refuse(errors.New("source is required: the plugin the template belongs to"))
	}
	if req.Name == "" {
		return refuse(errors.New("name is required"))
	}
	if req.Name == NeutralTemplateName {
		return refuse(errors.Newf("%s is QNTX's own template, and a Send naming no template gets it", NeutralTemplateName))
	}
	if err := checkMailTemplate(req.Template); err != nil {
		return refuse(errors.Wrapf(err, "template %s of %s", req.Name, req.Source))
	}

	w := s.wired.Load()
	if w == nil || w.Records == nil {
		return refuse(errors.New("the mail service has nowhere to keep a template yet"))
	}
	store := w.Records()
	if store == nil {
		return refuse(errors.New("the node holds no store to keep the template in"))
	}

	ref := templateRef(req.Source, req.Name)
	id, err := s.attest(store, w.Actor, ref, PredicateMailTemplate, req.Source, req.Source, map[string]any{
		"plugin":  req.Source,
		"name":    req.Name,
		"subject": req.Template.Subject,
		"html":    req.Template.Html,
		"text":    req.Template.Text,
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
func (s *MailServer) attest(store ats.AttestationStore, actor, subject, predicate, context, source string, attrs map[string]any) (string, error) {
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, context, store.AttestationExists)
	if err != nil {
		return "", errors.Wrapf(err, "no id could be minted for %s of %s", predicate, subject)
	}
	if actor == "" {
		actor = "did:key:unknown"
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{context},
		Actors:     []string{actor},
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
	if req.Source == "" {
		return refuse(errors.New("source is required: the plugin sending"))
	}
	if req.UserId == "" {
		return refuse(errors.New("user_id is required: mail goes to a User"))
	}

	w, to, err := s.ready(req.UserId)
	if err != nil {
		return refuse(err)
	}

	template, ref, err := s.template(w, req.Source, req.Template)
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

	mail := OutgoingMail{From: w.From, To: to, Subject: filled.Subject, HTML: filled.HTML, Text: filled.Text, Inline: inline}
	messageID, attestationID, sendErr := s.deliver(ctx, w, req.UserId, req.Source, ref, mail)
	if sendErr != nil {
		return &protocol.SendMailResponse{Success: false, Error: sendErr.Error(), AttestationId: attestationID}, nil //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
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
		if img.ContentId == "" {
			return nil, errors.Newf("inline image %d has no content_id: the html shows it by cid:<content_id>", i)
		}
		if seen[img.ContentId] {
			return nil, errors.Newf("inline image %d repeats content_id %s", i, img.ContentId)
		}
		seen[img.ContentId] = true
		if img.ContentType != "image/png" {
			return nil, errors.Newf("inline image %s is %q: only image/png is accepted", img.ContentId, img.ContentType)
		}
		if !bytes.HasPrefix(img.Data, pngSignature) {
			return nil, errors.Newf("inline image %s is not a PNG: its %d bytes do not begin with the PNG signature", img.ContentId, len(img.Data))
		}
		total += len(img.Data)
		if total > MaxMailImageBytes {
			return nil, errors.Newf("inline images come to more than %d bytes by image %s", MaxMailImageBytes, img.ContentId)
		}
		fileName := img.FileName
		if fileName == "" {
			fileName = img.ContentId + ".png"
		}
		inline = append(inline, InlineImage{ContentID: img.ContentId, ContentType: img.ContentType, FileName: fileName, Data: img.Data})
	}
	return inline, nil
}

// SendAsNode mails a User something the node wrote itself (ADR-042). It is
// held to what a plugin's mail is held to — a transport, an address to send
// from, a User who is on and has an address — and attested the same way.
func (s *MailServer) SendAsNode(ctx context.Context, userID string, m NodeMail) (messageID, attestationID string, err error) {
	w, to, err := s.ready(userID)
	if err != nil {
		s.logger.Warnw("Node mail not sent", "mail", m.Name, "user", userID, "error", err)
		return "", "", err
	}
	mail := OutgoingMail{From: w.From, To: to, Subject: m.Subject, HTML: m.HTML, Text: m.Text, Inline: m.Inline}
	return s.deliver(ctx, w, userID, NodeSource, m.Name, mail)
}

// ready is the wiring a send needs and the address it goes to, or why there is
// none.
func (s *MailServer) ready(userID string) (*MailWiring, string, error) {
	w := s.wired.Load()
	if w == nil {
		return nil, "", errors.New("the mail service is not wired yet: the node has not finished starting")
	}
	if w.Transport == nil {
		return nil, "", errors.New("no mail transport is enabled: set mail.ses.enabled = true in am.toml")
	}
	if w.From == "" {
		return nil, "", errors.New("no address to send from: set mail.from in am.toml")
	}
	if w.Recipients == nil {
		return nil, "", errors.New("this node keeps no Users, so there is nobody to mail")
	}
	if w.Records == nil || w.Records() == nil {
		return nil, "", errors.New("the node holds no store to attest the mail in, so none is sent")
	}
	to, err := s.recipient(w, userID)
	if err != nil {
		return nil, "", err
	}
	return w, to, nil
}

// deliver hands a filled mail to the transport and attests what became of it:
// mail:sent with the transport's id, or mail:failed with its refusal.
func (s *MailServer) deliver(ctx context.Context, w *MailWiring, userID, source, ref string, mail OutgoingMail) (string, string, error) {
	messageID, sendErr := w.Transport.Send(ctx, mail)

	attrs := map[string]any{
		"plugin":   source,
		"template": ref,
		"to":       mail.To,
		"from":     mail.From,
		"subject":  mail.Subject,
		"html":     mail.HTML,
		"text":     mail.Text,
	}
	// "i would have expected to be able to click the main and see exactly what was sent."
	//
	// The images are kept whole beside the html that shows them, so the mail
	// can be shown again as it went out, graphs and all.
	if len(mail.Inline) > 0 {
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
	}
	predicate := PredicateMailSent
	if sendErr != nil {
		predicate = PredicateMailFailed
		attrs["error"] = sendErr.Error()
	} else {
		attrs["message_id"] = messageID
	}

	attestationID, attestErr := s.attest(w.Records(), w.Actor, userID, predicate, ref, source, attrs)
	if attestErr != nil {
		// The mail left or failed either way; what is lost is the record of it,
		// and that is said with everything the record would have held.
		s.logger.Errorw("Mail not attested: the store refused it",
			"predicate", predicate, "user", userID, "template", ref,
			"message_id", messageID, "attributes", attrs, "error", attestErr)
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
func (s *MailServer) recipient(w *MailWiring, userID string) (string, error) {
	u, found, err := w.Recipients(userID)
	if err != nil {
		return "", errors.Wrapf(err, "User %s could not be read", userID)
	}
	if !found {
		return "", errors.Newf("no User %s", userID)
	}
	if u.DisabledBy != "" {
		return "", errors.Newf("User %s is switched off by %s", userID, u.DisabledBy)
	}
	if u.Email == "" {
		return "", errors.Newf("User %s has no email address", userID)
	}
	return u.Email, nil
}

// template is what a Send is filled from: QNTX's neutral template when it names
// none, else the newest the plugin set under that name.
func (s *MailServer) template(w *MailWiring, plugin, name string) (*protocol.MailTemplate, string, error) {
	if name == "" || name == NeutralTemplateName {
		return NeutralTemplate(), NeutralTemplateName, nil
	}
	ref := templateRef(plugin, name)
	kept, found, err := NewestMailTemplate(w.Records(), plugin, name)
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
	kept, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{ref},
		Predicates: []string{PredicateMailTemplate},
		Limit:      1000,
	})
	if err != nil {
		return nil, false, errors.Wrapf(err, "template %s could not be read", ref)
	}
	var newest *types.As
	for _, as := range kept {
		// A store may match a subject loosely; a template is its exact name.
		if !slices.Contains(as.Subjects, ref) {
			continue
		}
		if newest == nil || as.Timestamp.After(newest.Timestamp) {
			newest = as
		}
	}
	if newest == nil {
		return nil, false, nil
	}
	return TemplateOf(newest), true, nil
}

// TemplateOf reads a template back out of the attestation it was kept as.
func TemplateOf(as *types.As) *protocol.MailTemplate {
	part := func(key string) string {
		if v, ok := as.Attributes[key].(string); ok {
			return v
		}
		return ""
	}
	return &protocol.MailTemplate{Subject: part("subject"), Html: part("html"), Text: part("text")}
}

// templateRef names a plugin's template: the plugin, then its name.
func templateRef(plugin, name string) string {
	return plugin + "/" + name
}

// stamped adds the version of the plugin a record came from.
func (s *MailServer) stamped(source string, attrs map[string]any) map[string]any {
	if resolver := s.versionResolver.Load(); resolver != nil && *resolver != nil {
		if v := (*resolver)(source); v != "" {
			attrs["source_version"] = v
		}
	}
	return attrs
}
