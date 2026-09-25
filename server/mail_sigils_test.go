package server

import (
	"context"
	"encoding/json"
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
	body, err := json.Marshal(answer)
	require.NoError(t, err)
	for _, held := range signum.GetSigils() {
		if held.GetName() == name {
			require.NoError(t, sigil.Holds(held, body))
			return
		}
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

	mails := answer.(map[string]any)["mails"].([]mailRow)
	require.Len(t, mails, 3)
	assert.Equal(t, "Geweigerd", mails[0].Subject)
	assert.False(t, mails[0].Sent)
	assert.NotEmpty(t, mails[0].Error)
	assert.Equal(t, "Tot morgen", mails[1].Subject)
	assert.True(t, mails[1].Sent)
	assert.Equal(t, "ses-Tot morgen", mails[1].MessageID)
	assert.Equal(t, "UStim", mails[1].User)
	assert.Equal(t, "tim@defacile.nl", mails[1].To)
	assert.Equal(t, "garden", mails[1].Plugin)
	assert.Equal(t, services.NeutralTemplateName, mails[1].Template)

	limited, refusal := s.mailSent(asRoot(), sigil.Sent{"limit": "1"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Len(t, limited.(map[string]any)["mails"].([]mailRow), 1)
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

	neutral := answer.(map[string]any)["neutral"].(mailTemplateRow)
	assert.Equal(t, services.NeutralTemplateName, neutral.Name)
	assert.Equal(t, services.NeutralTemplate().Html, neutral.HTML)

	// "why not? i want it to be listed there as well"
	dark := answer.(map[string]any)["dark"].(mailTemplateRow)
	own, err := services.DarkTemplate()
	require.NoError(t, err)
	assert.Equal(t, services.DarkTemplateName, dark.Name)
	assert.Equal(t, own.Html, dark.HTML)

	templates := answer.(map[string]any)["templates"].([]mailTemplateRow)
	require.Len(t, templates, 1, "a template set twice is one template")
	assert.Equal(t, "garden", templates[0].Plugin)
	assert.Equal(t, "reminder", templates[0].Name)
	assert.Equal(t, "Tweede", templates[0].Subject)
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

	m := answer.(map[string]any)["mail"].(mailMessage)
	assert.Equal(t, attestationID, m.ID)
	assert.Equal(t, "Garden <mail@garden.test>", m.From)
	assert.Equal(t, "tim@defacile.nl", m.To)
	assert.Equal(t, "Week 39", m.Subject)
	assert.Equal(t, `<p>The week.</p><img src="cid:cpu">`, m.HTML)
	assert.Equal(t, "The week.", m.Text)
	assert.True(t, m.Sent)
	require.Len(t, m.Images, 1)
	assert.Equal(t, mailImage{ContentID: "cpu", ContentType: "image/png", Data: "iVBORw=="}, m.Images[0])

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
	own := answer.(map[string]any)["node"].([]nodeMailRow)
	require.Len(t, own, 1)
	assert.Equal(t, reportHandlerName, own[0].Name)
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

	said := answer.(map[string]any)
	assert.Equal(t, "Garden <mail@garden.test>", said["from"])
	assert.Equal(t, mailSES{Enabled: false}, said["ses"])
	assert.Nil(t, said["account"])
	assert.Contains(t, said["unanswered"], "mail.ses.enabled")
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
