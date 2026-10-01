package qntxinbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"slices"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

const (
	// as <message> is mail:received of <address>
	PredicateMailReceived = "mail:received"
	// as <message> is mail:dropped of <address>
	PredicateMailDropped = "mail:dropped"

	MailboxInbox = "inbox"
	MailboxJunk  = "junk"
	MailboxSent  = "sent"

	inboundPrefix = "inbound/"
	filedPrefix   = "filed/"
	unfiledPrefix = "unfiled/"

	handlerReceive = "inbox.receive"
)

// GetSchedules has Pulse run receiving.
func (p *Plugin) GetSchedules() []*protocol.ScheduleInfo {
	return []*protocol.ScheduleInfo{{
		HandlerName:      handlerReceive,
		IntervalSeconds:  p.receiveEvery,
		EnabledByDefault: true,
		Description:      "File the mail SES stored under the User who holds its address",
	}}
}

// GetHandlerNames are the jobs Pulse hands this plugin.
func (p *Plugin) GetHandlerNames() []string { return []string{handlerReceive} }

// ExecuteJob runs one receiving.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, _ string, _ []byte) ([]byte, []*protocol.JobLogEntry, error) {
	if handlerName != handlerReceive {
		return nil, nil, protocol.ErrUnknownHandler(handlerName)
	}
	got, err := p.receive(ctx, p.own())
	result, merr := json.Marshal(got)
	if merr != nil {
		return nil, nil, errors.Wrap(merr, "the receiving's tally did not marshal")
	}
	return result, nil, err
}

// tally is what one receiving did.
type tally struct {
	Filed   int `json:"filed"`
	Dropped int `json:"dropped"`
	Unfiled int `json:"unfiled"`
}

// receive files every stored message under the Users holding the addresses it
// reached, then moves it out of inbound/. A message reaching nobody's address
// is moved to unfiled/ and not filed.
func (p *Plugin) receive(ctx context.Context, st store) (tally, error) {
	var t tally
	keys, err := p.bag.List(ctx, inboundPrefix)
	if err != nil {
		return t, err
	}
	var failed []error
	for _, key := range keys {
		filed, err := p.receiveOne(ctx, st, key, &t)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		to := unfiledPrefix
		if filed {
			to = filedPrefix
		}
		if err := p.bag.Move(ctx, key, to+strings.TrimPrefix(key, inboundPrefix)); err != nil {
			failed = append(failed, err)
		}
	}
	return t, stderrors.Join(failed...)
}

func (p *Plugin) receiveOne(ctx context.Context, st store, key string, t *tally) (bool, error) {
	raw, err := p.bag.Get(ctx, key)
	if err != nil {
		return false, err
	}
	name := strings.TrimPrefix(key, inboundPrefix)
	m, err := parse(raw)
	if err != nil {
		t.Unfiled++
		p.log().Warnw("a stored message is not mail and is not filed", "object", key, "error", err)
		return false, nil
	}
	sum := sha256.Sum256(raw)
	filed := false
	for _, address := range m.Recipients() {
		user, err := holder(st, address)
		if err != nil {
			return false, err
		}
		if user == "" {
			continue
		}
		filed = true
		already, err := st.GetAttestations(ats.AttestationFilter{Subjects: []string{name}, Predicates: []string{PredicateMailReceived, PredicateMailDropped}, Contexts: []string{address}, Limit: 1})
		if err != nil {
			return false, errors.Wrapf(err, "whether %s was filed for %s did not read", name, address)
		}
		if len(already) > 0 {
			continue
		}
		attributes := map[string]interface{}{
			"user":        user,
			"from":        m.From,
			"to":          strings.Join(m.To, ", "),
			"cc":          strings.Join(m.Cc, ", "),
			"subject":     m.Subject,
			"date":        m.Date,
			"message_id":  m.MessageID,
			"in_reply_to": m.InReplyTo,
			"references":  m.References,
			"blob":        filedPrefix + name,
			"sha256":      hex.EncodeToString(sum[:]),
		}
		predicate := PredicateMailReceived
		switch {
		case m.Virus:
			predicate = PredicateMailDropped
			attributes["reason"] = "X-SES-Virus-Verdict: FAIL"
			t.Dropped++
		case m.Spam:
			attributes["mailbox"] = MailboxJunk
			attributes["text"] = m.Text
			t.Filed++
		default:
			attributes["mailbox"] = MailboxInbox
			attributes["text"] = m.Text
			t.Filed++
		}
		if _, err := st.GenerateAndCreateAttestation(ctx, &types.AsCommand{
			Subjects:   []string{name},
			Predicates: []string{predicate},
			Contexts:   []string{address},
			Source:     p.Metadata().Name,
			Attributes: attributes,
		}); err != nil {
			return false, errors.Wrapf(err, "%s is not attested as %s of %s", name, predicate, address)
		}
	}
	if !filed {
		t.Unfiled++
	}
	return filed, nil
}

// holder is the User an address is mail:address of, the latest grant's, or
// empty when nobody holds it.
func holder(st store, address string) (string, error) {
	grants, err := st.GetAttestations(ats.AttestationFilter{Subjects: []string{address}, Predicates: []string{PredicateMailAddress}, Limit: 100})
	if err != nil {
		return "", errors.Wrapf(err, "who holds %s did not read", address)
	}
	if len(grants) == 0 {
		return "", nil
	}
	latest := slices.MaxFunc(grants, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	if len(latest.Contexts) == 0 {
		return "", nil
	}
	return latest.Contexts[0], nil
}
