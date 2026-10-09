package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// "that is a ROOT question about governance and controls and monitoring that belongs in its own window element in the tray like others"

// kept is a transport that keeps what it was handed.
type kept struct{ mails []services.OutgoingMail }

func (k *kept) Send(_ context.Context, m services.OutgoingMail) (string, error) {
	k.mails = append(k.mails, m)
	return "ses-" + m.Subject, nil
}

// refusing is a transport SES stands behind on a bad day.
type refusing struct{}

func (refusing) Send(context.Context, services.OutgoingMail) (string, error) {
	return "", assert.AnError
}

// mailingNode is a node with a system store and a mail service attesting into
// it, the way sub_plugins wires one.
func mailingNode(t *testing.T, transport services.MailTransport) (*QNTXServer, *services.MailServer) {
	t.Helper()
	s := rootKnowingServer(t)
	mail := services.NewMailServer("token", zap.NewNop().Sugar())
	mail.Wire(services.MailWiring{
		From:      "Garden <mail@garden.test>",
		Transport: transport,
		Recipients: func(id string) (services.MailRecipient, bool, error) {
			return services.MailRecipient{ID: id, Email: "tim@defacile.nl"}, true, nil
		},
		Records: s.mailRecords,
		Actor:   "did:key:z6Mkgardennode",
	})
	return s, mail
}

// asRoot is a context ROOT asks in.
func asRoot() context.Context {
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	return auth.WithAdmission(context.Background(), root)
}

// holds asks a sigil's real answer whether it carries what the sigil gives.
func holds(t *testing.T, signum sigil.Signum, name string, answer any) {
	t.Helper()
	answered, err := answeredOf(signum)
	require.NoError(t, err)
	require.NoError(t, answered.Check())
	body, err := answerJSON(answer)
	require.NoError(t, err)
	for _, held := range answered.GetSigils() {
		if held.GetName() != name {
			continue
		}
		// A message is held to the schema its sigil promises; anything else to
		// the fields it lists.
		if _, message := answer.(proto.Message); message {
			require.NoError(t, heldTo(promisedBy(t, held).schema, body))
			return
		}
		require.NoError(t, sigil.Holds(held, body))
		return
	}
	t.Fatalf("the mail signum holds no sigil %s", name)
}

func TestTheMailSignumSaysWhatItHolds(t *testing.T) {
	s := rootKnowingServer(t)
	require.NoError(t, s.mailSignum().Check())
}

// "assume only ROOT has governance over gRPC calls"
func TestOnlyRootReachesTheMailWindow(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	for _, held := range rootKnowingServer(t).mailSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
}

// "its attested", and ROOT reads it back: sent and refused alike, newest first.
func TestTheMailWindowListsWhatWasSentAndWhatWasRefused(t *testing.T) {
	s, mail := mailingNode(t, &kept{})

	for _, subject := range []string{"Welkom", "Tot morgen"} {
		resp, err := mail.Send(context.Background(), &protocol.SendMailRequest{
			AuthToken: "token", Source: "garden", UserId: "UStim",
			Values: map[string]string{"subject": subject, "body": "Hallo."},
		})
		require.NoError(t, err)
		require.True(t, resp.Success, resp.Error)
	}

	refused := services.NewMailServer("token", zap.NewNop().Sugar())
	refused.Wire(services.MailWiring{
		From: "Garden <mail@garden.test>", Transport: refusing{},
		Recipients: func(id string) (services.MailRecipient, bool, error) {
			return services.MailRecipient{ID: id, Email: "tim@defacile.nl"}, true, nil
		},
		Records: s.mailRecords, Actor: "did:key:z6Mkgardennode",
	})
	resp, err := refused.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: "token", Source: "garden", UserId: "UStim",
		Values: map[string]string{"subject": "Geweigerd", "body": "Hallo."},
	})
	require.NoError(t, err)
	require.False(t, resp.Success)

	answer, refusal := s.mailSent(asRoot(), sigil.Sent{})
	require.Nil(t, refusal, refusal.GetSays())
	holds(t, s.mailSignum(), "sent", answer)

	mails := answer.(*protocol.MailSent).GetMails()
	require.Len(t, mails, 3)
	assert.Equal(t, "Geweigerd", mails[0].GetSubject())
	assert.False(t, mails[0].GetSent())
	assert.NotEmpty(t, mails[0].GetError())
	assert.Equal(t, "Tot morgen", mails[1].GetSubject())
	assert.True(t, mails[1].GetSent())
	assert.Equal(t, "ses-Tot morgen", mails[1].GetMessageId())
	assert.Equal(t, "UStim", mails[1].GetUser())
	assert.Equal(t, "tim@defacile.nl", mails[1].GetTo())
	assert.Equal(t, "garden", mails[1].GetPlugin())
	assert.Equal(t, services.NeutralTemplateName, mails[1].GetTemplate())

	limited, refusal := s.mailSent(asRoot(), sigil.Sent{"limit": "1"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Len(t, limited.(*protocol.MailSent).GetMails(), 1)
}

// "the plugin owns the template but qntx does provide a neutral template and code for how to set it"
func TestTheMailWindowShowsTheNeutralTemplateAndEachPluginsNewest(t *testing.T) {
	s, mail := mailingNode(t, &kept{})

	for _, subject := range []string{"Eerste", "Tweede"} {
		set, err := mail.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
			AuthToken: "token", Source: "garden", Name: "reminder",
			Template: &protocol.MailTemplate{Subject: subject, Text: "Tot morgen."},
		})
		require.NoError(t, err)
		require.True(t, set.Success, set.Error)
	}

	answer, refusal := s.mailTemplates(asRoot(), sigil.Sent{})
	require.Nil(t, refusal, refusal.GetSays())
	holds(t, s.mailSignum(), "templates", answer)

	neutral := answer.(*protocol.MailTemplates).GetNeutral()
	assert.Equal(t, services.NeutralTemplateName, neutral.GetName())
	assert.Equal(t, services.NeutralTemplate().Html, neutral.GetHtml())

	// "why not? i want it to be listed there as well"
	dark := answer.(*protocol.MailTemplates).GetDark()
	own, err := services.DarkTemplate()
	require.NoError(t, err)
	assert.Equal(t, services.DarkTemplateName, dark.GetName())
	assert.Equal(t, own.Html, dark.GetHtml())

	templates := answer.(*protocol.MailTemplates).GetTemplates()
	require.Len(t, templates, 1, "a template set twice is one template")
	assert.Equal(t, "garden", templates[0].GetPlugin())
	assert.Equal(t, "reminder", templates[0].GetName())
	assert.Equal(t, "Tweede", templates[0].GetSubject())
}

// "i would have expected to be able to click the main and see exactly what was sent."
func TestOneMailIsReadBackWholeAsItWasSent(t *testing.T) {
	s, mail := mailingNode(t, &kept{})

	_, attestationID, err := mail.SendAsNode(context.Background(), "UStim", services.NodeMail{
		Name: "report.weekly", Subject: "Week 39", Text: "The week.",
		HTML:   `<p>The week.</p><img src="cid:cpu">`,
		Inline: []services.InlineImage{{ContentID: "cpu", ContentType: "image/png", FileName: "cpu.png", Data: []byte{0x89, 'P', 'N', 'G'}}},
	})
	require.NoError(t, err)

	answer, refusal := s.mailMessage(asRoot(), sigil.Sent{"id": attestationID, "user": "UStim"})
	require.Nil(t, refusal, refusal.GetSays())
	holds(t, s.mailSignum(), "message", answer)

	m := answer.(*protocol.MailMessage).GetMail()
	assert.Equal(t, attestationID, m.GetId())
	assert.Equal(t, "Garden <mail@garden.test>", m.GetFrom())
	assert.Equal(t, "tim@defacile.nl", m.GetTo())
	assert.Equal(t, "Week 39", m.GetSubject())
	assert.Equal(t, `<p>The week.</p><img src="cid:cpu">`, m.GetHtml())
	assert.Equal(t, "The week.", m.GetText())
	assert.True(t, m.GetSent())
	require.Len(t, m.GetImages(), 1)
	image := m.GetImages()[0]
	assert.Equal(t, []string{"cpu", "image/png", "iVBORw=="}, []string{image.GetContentId(), image.GetContentType(), image.GetData()})

	_, refusal = s.mailMessage(asRoot(), sigil.Sent{"id": "AS-NOBODY", "user": "UStim"})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.NotFound, refusal.GetWhy())
}

// The node's own mail is not filled from a template, and the window says what
// it is instead of leaving it out.
func TestTheTemplatesNameTheNodesOwnMail(t *testing.T) {
	s := rootKnowingServer(t)
	answer, refusal := s.mailTemplates(asRoot(), sigil.Sent{})
	require.Nil(t, refusal, refusal.GetSays())
	holds(t, s.mailSignum(), "templates", answer)
	own := answer.(*protocol.MailTemplates).GetNode()
	require.Len(t, own, 1)
	assert.Equal(t, reportHandlerName, own[0].GetName())
}

// "ses being enabled for use with email service can be enabled in the am.toml"
//
// Off, the window says so and SES is not asked.
func TestTheMailWindowSaysWhenSESIsNotEnabled(t *testing.T) {
	s := rootKnowingServer(t)
	s.mailConfig = config.MailConfig{From: "Garden <mail@garden.test>"}

	answer, refusal := s.mailAccount(asRoot(), sigil.Sent{})
	require.Nil(t, refusal, refusal.GetSays())
	holds(t, s.mailSignum(), "account", answer)

	said := answer.(*protocol.MailAccount)
	assert.Equal(t, "Garden <mail@garden.test>", said.GetFrom())
	assert.False(t, said.GetSes().GetEnabled())
	assert.Nil(t, said.GetAccount())
	assert.Contains(t, said.GetUnanswered(), "mail.ses.enabled")
}

// "goes to their primary email address if there are multiple."
func TestTheNodeHandsMailTheFirstAddressAUserSupplied(t *testing.T) {
	s := rootKnowingServer(t)
	users, _, err := auth.OpenUserTable(s.nodeDB, nil)
	require.NoError(t, err)
	require.NoError(t, users.Put(auth.User{
		ID: "UStim", Level: auth.LevelPublicRegistration,
		EmailAddresses: []string{"tim@defacile.nl", "tim@werk.nl"},
	}))
	s.authHandler, err = auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, users, false, []string{rootAccount}, nil)
	require.NoError(t, err)

	tim, found, err := s.mailRecipient("UStim")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "tim@defacile.nl", tim.Email)
}
