package qntxinbox

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

const (
	// as <SES message id> is mail:sent of <address>
	PredicateMailSent = "mail:sent"
	// "ROOT should be able to read other user's mail as well, but doing so is an attested event."
	PredicateMailRead = "mail:read"
)

// SubmitEmail sends a text mail through SES from an address the caller holds,
// and attests it as sent.
func (p *Plugin) SubmitEmail(ctx context.Context, req *protocol.SubmitEmailRequest) (_ *protocol.SubmitEmailResponse, err error) {
	c, err := handed(ctx)
	if err != nil {
		return nil, err
	}
	from := strings.ToLower(req.GetFrom())
	if len(req.GetTo()) == 0 {
		return nil, &refused{http.StatusBadRequest, "to names nobody"}
	}
	for _, to := range req.GetTo() {
		if !strings.Contains(to, "@") {
			return nil, &refused{http.StatusBadRequest, quoted(to) + " is not an address"}
		}
	}

	st, closeStore, err := p.dial(ctx, p.storeEndpoint, c.token)
	if err != nil {
		return nil, err
	}
	defer func() { err = sqlclose.With(err, closeStore(), "the connection to the store at "+p.storeEndpoint) }()

	user, err := holder(st, from)
	if err != nil {
		return nil, err
	}
	if user == "" || user != c.user {
		return nil, &refused{http.StatusForbidden, "mail is sent only from an address the sender holds, and " + from + " is not the caller's"}
	}

	id, err := p.sender.Send(ctx, outgoing{From: from, To: req.GetTo(), Subject: req.GetSubject(), Text: req.GetTextBody()})
	if err != nil {
		return nil, err
	}
	as, err := st.GenerateAndCreateAttestation(ctx, &types.AsCommand{
		Subjects:   []string{id},
		Predicates: []string{PredicateMailSent},
		Contexts:   []string{from},
		Actors:     []string{c.asker},
		Source:     p.Metadata().Name,
		Attributes: map[string]interface{}{
			"mailbox": MailboxSent,
			"user":    user,
			"from":    from,
			"to":      strings.Join(req.GetTo(), ", "),
			"subject": req.GetSubject(),
			"text":    req.GetTextBody(),
		},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "the mail SES sent as %s is not attested as sent from %s", id, from)
	}
	return &protocol.SubmitEmailResponse{Sent: email(as)}, nil
}

// QueryEmails is one mailbox of an address, newest first: the caller's own,
// or any for ROOT, whose reading of another User's mail is attested.
func (p *Plugin) QueryEmails(ctx context.Context, req *protocol.QueryEmailsRequest) (_ *protocol.QueryEmailsResponse, err error) {
	c, err := handed(ctx)
	if err != nil {
		return nil, err
	}
	address := strings.ToLower(req.GetAddress())
	predicate := PredicateMailReceived
	switch req.GetInMailbox() {
	case MailboxInbox, MailboxJunk:
	case MailboxSent:
		predicate = PredicateMailSent
	default:
		return nil, &refused{http.StatusBadRequest, "mailbox is inbox, junk or sent, not " + quoted(req.GetInMailbox())}
	}

	st, closeStore, err := p.dial(ctx, p.storeEndpoint, c.token)
	if err != nil {
		return nil, err
	}
	defer func() { err = sqlclose.With(err, closeStore(), "the connection to the store at "+p.storeEndpoint) }()

	user, err := holder(st, address)
	if err != nil {
		return nil, err
	}
	if user == "" || user != c.user {
		if c.level != string(auth.LevelRoot) {
			return nil, &refused{http.StatusForbidden, address + " is not the caller's"}
		}
		if _, err := st.GenerateAndCreateAttestation(ctx, &types.AsCommand{
			Subjects:   []string{address},
			Predicates: []string{PredicateMailRead},
			Contexts:   []string{user},
			Actors:     []string{c.asker},
			Source:     p.Metadata().Name,
			Attributes: map[string]interface{}{"mailbox": req.GetInMailbox(), "reader": c.user},
		}); err != nil {
			return nil, errors.Wrapf(err, "ROOT's reading of %s is not attested, so it is not read", address)
		}
	}

	held, err := st.GetAttestations(ats.AttestationFilter{Predicates: []string{predicate}, Contexts: []string{address}, Limit: 1000})
	if err != nil {
		return nil, errors.Wrapf(err, "the %s of %s did not read", req.GetInMailbox(), address)
	}
	slices.SortFunc(held, func(a, b *types.As) int { return b.Timestamp.Compare(a.Timestamp) })
	resp := &protocol.QueryEmailsResponse{}
	for _, as := range held {
		if attribute(as, "mailbox") == req.GetInMailbox() {
			resp.List = append(resp.List, email(as))
		}
	}
	return resp, nil
}

func attribute(as *types.As, key string) string {
	s, _ := as.Attributes[key].(string)
	return s
}

func email(as *types.As) *protocol.Email {
	var to []string
	for _, a := range strings.Split(attribute(as, "to"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			to = append(to, a)
		}
	}
	return &protocol.Email{
		Id:         as.ID,
		BlobId:     attribute(as, "blob"),
		MailboxIds: []string{attribute(as, "mailbox")},
		From:       attribute(as, "from"),
		To:         to,
		Subject:    attribute(as, "subject"),
		TextBody:   attribute(as, "text"),
		ReceivedAt: as.Timestamp.UTC().Format(time.RFC3339),
	}
}

func (p *Plugin) send(w http.ResponseWriter, r *http.Request) {
	var req protocol.SubmitEmailRequest
	if err := decode(r, &req); err != nil {
		http.Error(w, "the body is not a JSON object of from, to, subject and text_body: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := p.SubmitEmail(callOf(r), &req)
	p.answer(w, resp, err)
}

func (p *Plugin) mailbox(w http.ResponseWriter, r *http.Request) {
	resp, err := p.QueryEmails(callOf(r), &protocol.QueryEmailsRequest{
		Address:   r.URL.Query().Get("address"),
		InMailbox: r.URL.Query().Get("mailbox"),
	})
	p.answer(w, resp, err)
}
