package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/named"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

const (
	mailToken = "the-node-token"
	mailFrom  = "Garden <mail@garden.test>"
	mailActor = "did:key:z6Mkgardennode"
)

// sentBox is a transport that keeps what it was handed, or refuses all of it.
type sentBox struct {
	sent    []OutgoingMail
	refuses error
}

func (b *sentBox) Send(_ context.Context, m OutgoingMail) (string, error) {
	if b.refuses != nil {
		return "", b.refuses
	}
	b.sent = append(b.sent, m)
	return "ses-0001", nil
}

var tim = UserRecipient(access.User{ID: "UStim", EmailAddresses: []string{"tim@defacile.nl", "tim@elders.nl"}})

// A mail service wired the way the node wires it: a transport, the Users, and
// the store it attests into.
func wiredMail(t *testing.T, transport MailTransport, users ...MailRecipient) (*MailServer, ats.AttestationStore) {
	t.Helper()
	return wiredMailBy(t, Having(transport), users...)
}

// wiredMailBy is wiredMail with the transport had, or the node running without
// one.
func wiredMailBy(t *testing.T, transport Had[MailTransport], users ...MailRecipient) (*MailServer, ats.AttestationStore) {
	t.Helper()
	store, _ := qntxtest.CreateTestStore(t)
	held := map[string]MailRecipient{}
	for _, u := range users {
		held[u.ID] = u
	}
	s := NewMailServer(mailToken, zap.NewNop().Sugar())
	// Every plugin these tests mail for runs, at 0.1.0.
	s.SetVersionResolver(func(string) (string, bool) { return "0.1.0", true })
	s.Wire(MailWiring{
		From:      MailFrom(mailFrom),
		Transport: transport,
		Recipients: Having[MailRecipients](func(id string) (MailRecipient, bool, error) {
			u, found := held[id]
			return u, found, nil
		}),
		Records: func() (ats.AttestationStore, error) { return store, nil },
		Actor:   MailActor(mailActor),
	})
	return s, store
}

func attested(t *testing.T, store ats.AttestationStore, predicate string) []*types.As {
	t.Helper()
	found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{predicate}, Limit: 100})
	require.NoError(t, err)
	return found
}

// bodyIn is the body of a kind a mail went out in.
func bodyIn(t *testing.T, m OutgoingMail, kind MailBodyKind) string {
	t.Helper()
	body, err := BodyOf(m.Bodies.All(), kind)
	require.NoError(t, err)
	return body
}

// A template names each body it is sent in, at least one, none twice.
func TestATemplateNamesItsBodies(t *testing.T) {
	s, _ := wiredMail(t, &sentBox{}, tim)
	for what, template := range map[string]*protocol.MailTemplate{
		"bodies":                {Subject: "x"},
		"text body twice":       {Subject: "x", Bodies: []*protocol.MailTemplateBody{TemplateText("a"), TemplateText("b")}},
		"neither html nor text": {Subject: "x", Bodies: []*protocol.MailTemplateBody{{}}},
		"names nothing":         {Subject: "x", Bodies: []*protocol.MailTemplateBody{TemplateHTML("  ")}},
	} {
		set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
			AuthToken: mailToken, Source: "garden", Name: "reminder", Template: template,
		})
		require.NoError(t, err)
		assert.False(t, set.Success, what)
		assert.Contains(t, set.Error, what)
	}
}

// "nil is nil": a template kept before its bodies were named is not read as
// one with none.
func TestATemplateKeptBeforeItsBodiesWereNamedIsRefused(t *testing.T) {
	_, err := TemplateOf(&types.As{ID: "ASold", Attributes: map[string]any{"subject": "x", "html": "<p>y</p>"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before a template's bodies were named")
}

// A node mail names what it is sent in.
func TestANodeMailNamingNoBodyIsNotSent(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)
	_, _, err := s.SendAsNode(context.Background(), "UStim", NodeMail{Name: "report.weekly", Subject: "Week 39"})
	require.Error(t, err)
	assert.Empty(t, box.sent)
}

func neutralSend(user string, values map[string]string) *protocol.SendMailRequest {
	return &protocol.SendMailRequest{AuthToken: mailToken, Source: "garden", UserId: user, Template: NeutralTemplateName, Values: values}
}

// "goes to their primary email address if there are multiple."
// "its attested"
func TestTimIsMailedAtHisPrimaryAddressFromTheNeutralTemplate(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{
		"subject": "Welkom", "body": "Je bent geregistreerd.",
		"link": "https://garden.test/app", "link_label": "Download de app",
	}))
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	assert.Equal(t, "ses-0001", resp.MessageId)

	require.Len(t, box.sent, 1)
	mail := box.sent[0]
	assert.Equal(t, "tim@defacile.nl", mail.To)
	assert.Equal(t, mailFrom, mail.From)
	assert.Equal(t, "Welkom", mail.Subject)
	assert.Contains(t, bodyIn(t, mail, MailText), "Je bent geregistreerd.")
	assert.Contains(t, bodyIn(t, mail, MailText), "https://garden.test/app")
	assert.Contains(t, bodyIn(t, mail, MailHTML), "Je bent geregistreerd.")
	assert.Contains(t, bodyIn(t, mail, MailHTML), `href="https://garden.test/app"`)
	assert.Contains(t, bodyIn(t, mail, MailHTML), "Download de app")

	sent := attested(t, store, PredicateMailSent)
	require.Len(t, sent, 1)
	assert.Equal(t, resp.AttestationId, sent[0].ID)
	assert.Equal(t, []string{"UStim"}, sent[0].Subjects)
	assert.Equal(t, []string{mailActor}, sent[0].Actors)
	assert.Equal(t, "garden", sent[0].Source)
	assert.Equal(t, "tim@defacile.nl", sent[0].Attributes["to"])
	assert.Equal(t, "Welkom", sent[0].Attributes["subject"])
	assert.Equal(t, "ses-0001", sent[0].Attributes["message_id"])
	assert.Equal(t, NeutralTemplateName, sent[0].Attributes["template"])
}

// "the plugin owns the template but qntx does provide a neutral template and code for how to set it"
func TestAPluginSetsItsOwnTemplateAndMailsFromIt(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: mailToken, Source: "garden", Name: "harvest-ready",
		Template: &protocol.MailTemplate{
			Subject: "Harvest ready on {{.date}}",
			Bodies:  []*protocol.MailTemplateBody{TemplateText("Pick on {{.date}}."), TemplateHTML("<p>Pick on {{.date}}.</p>")},
		},
	})
	require.NoError(t, err)
	require.True(t, set.Success, set.Error)

	kept := attested(t, store, PredicateMailTemplate)
	require.Len(t, kept, 1)
	assert.Equal(t, set.AttestationId, kept[0].ID)
	assert.Equal(t, "garden", kept[0].Attributes["plugin"])
	assert.Equal(t, "harvest-ready", kept[0].Attributes["name"])

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: mailToken, Source: "garden", UserId: "UStim",
		Template: "harvest-ready", Values: map[string]string{"date": "Monday 29 September"},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	require.Len(t, box.sent, 1)
	assert.Equal(t, "Harvest ready on Monday 29 September", box.sent[0].Subject)
	assert.Equal(t, "<p>Pick on Monday 29 September.</p>", bodyIn(t, box.sent[0], MailHTML))
	assert.Equal(t, "garden/harvest-ready", attested(t, store, PredicateMailSent)[0].Attributes["template"])
}

// Setting a template again is changing it: the newest one under a name is the
// one a mail is filled from.
func TestTheNewestTemplateUnderANameIsTheOneFilled(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)

	for _, subject := range []string{"Eerste", "Tweede"} {
		set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
			AuthToken: mailToken, Source: "garden", Name: "reminder",
			Template: &protocol.MailTemplate{Subject: subject, Bodies: []*protocol.MailTemplateBody{TemplateText("Tot morgen.")}},
		})
		require.NoError(t, err)
		require.True(t, set.Success, set.Error)
	}

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: mailToken, Source: "garden", UserId: "UStim", Template: "reminder",
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	assert.Equal(t, "Tweede", box.sent[0].Subject)
}

// A plugin's templates are its own: naming another plugin's is naming one it
// never set.
func TestAPluginMailsOnlyFromTemplatesItSet(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: mailToken, Source: "garden", Name: "welcome",
		Template: &protocol.MailTemplate{Subject: "Welkom", Bodies: []*protocol.MailTemplateBody{TemplateText("Hallo.")}},
	})
	require.NoError(t, err)
	require.True(t, set.Success, set.Error)

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: mailToken, Source: "orchard", UserId: "UStim", Template: "welcome",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "orchard")
	assert.Contains(t, resp.Error, "welcome")
	assert.Empty(t, box.sent)
	assert.Empty(t, attested(t, store, PredicateMailSent))
}

// A value the template names and the send does not is a refusal, never a hole
// in a mail that went out.
func TestAMailMissingAValueIsNotSent(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{"subject": "Welkom"}))
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "body")
	assert.Empty(t, box.sent)
	assert.Empty(t, attested(t, store, PredicateMailSent))
}

// A subject is one line. A value carrying a line break into it is refused.
func TestASubjectSpanningLinesIsNotSent(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{
		"subject": "Welkom\nBcc: someone@else.test", "body": "Hallo.",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Empty(t, box.sent)
}

// A template that does not parse is refused when it is set, not when a mail is
// owed.
func TestATemplateThatDoesNotParseIsNotKept(t *testing.T) {
	s, store := wiredMail(t, &sentBox{}, tim)

	set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: mailToken, Source: "garden", Name: "broken",
		Template: &protocol.MailTemplate{Subject: "Harvest {{.date", Bodies: []*protocol.MailTemplateBody{TemplateText("Hallo.")}},
	})
	require.NoError(t, err)
	assert.False(t, set.Success)
	assert.Contains(t, set.Error, "subject")
	assert.Empty(t, attested(t, store, PredicateMailTemplate))
}

// "ses being enabled for use with email service can be enabled in the am.toml"
func TestNoTransportEnabledNamesTheKeyThatEnablesOne(t *testing.T) {
	s, _ := wiredMailBy(t, HavingNot[MailTransport](errors.New("no mail transport is enabled: set mail.ses.enabled = true in am.toml")), tim)

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{"subject": "Welkom", "body": "Hallo."}))
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "mail.ses.enabled")
}

func TestAMailIsRefusedForAUserItCannotReach(t *testing.T) {
	switchedOff := UserRecipient(access.User{ID: "USjenny", EmailAddresses: []string{"jenny@defacile.nl"}, DisabledBy: "USjenny"})
	addressless := UserRecipient(access.User{ID: "USspike"})
	box := &sentBox{}
	s, store := wiredMail(t, box, switchedOff, addressless)

	for user, says := range map[string]string{
		"USnobody": "no User USnobody",
		"USjenny":  "switched off",
		"USspike":  "no email address",
	} {
		resp, err := s.Send(context.Background(), neutralSend(user, map[string]string{"subject": "Welkom", "body": "Hallo."}))
		require.NoError(t, err)
		assert.False(t, resp.Success, user)
		assert.Contains(t, resp.Error, says, user)
	}
	assert.Empty(t, box.sent)
	assert.Empty(t, attested(t, store, PredicateMailSent))
}

// A mail the transport refused was still a mail QNTX tried to send, and that
// is attested too.
func TestAMailTheTransportRefusedIsAttestedAsFailed(t *testing.T) {
	box := &sentBox{refuses: errors.New("MessageRejected: Email address is not verified")}
	s, store := wiredMail(t, box, tim)

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{"subject": "Welkom", "body": "Hallo."}))
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "MessageRejected")

	failed := attested(t, store, PredicateMailFailed)
	require.Len(t, failed, 1)
	assert.Equal(t, resp.AttestationId, failed[0].ID)
	assert.Contains(t, failed[0].Attributes["error"], "MessageRejected")
	assert.Empty(t, attested(t, store, PredicateMailSent))
}

// A plugin standing in a namespace reaches the node's services with the token
// minted for it there (ADR-046), and the mail service is one of them: on q
// cleanAPI's template was refused on every start, and Mail could not read it.
func TestAPluginsOwnTokenKeepsItsTemplateAndMailsFromIt(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)
	s.SetPluginStores(standingIn("cleanAPI-token", &namedStore{name: "clean"}))

	set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: "cleanAPI-token", Source: "garden", Name: "harvest-ready",
		Template: &protocol.MailTemplate{Subject: "Ready", Bodies: []*protocol.MailTemplateBody{TemplateText("Pick.")}},
	})
	require.NoError(t, err)
	require.True(t, set.Success, set.Error)

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: "cleanAPI-token", Source: "garden", UserId: "UStim", Template: "harvest-ready",
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	require.Len(t, box.sent, 1)

	stranger, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: "stranger", Source: "garden", Name: "harvest-ready",
		Template: &protocol.MailTemplate{Subject: "Ready", Bodies: []*protocol.MailTemplateBody{TemplateText("Pick.")}},
	})
	require.NoError(t, err)
	assert.False(t, stranger.Success)
}

func TestAWrongTokenSendsNothing(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: "guessed", Source: "garden", UserId: "UStim",
		Values: map[string]string{"subject": "Welkom", "body": "Hallo."},
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Empty(t, box.sent)
}

// "let's say QNTX also has it's own built in messages it would like to send sometimes via email"
func TestTheNodeMailsAUserInItsOwnName(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	messageID, attestationID, err := s.SendAsNode(context.Background(), "UStim", NodeMail{
		Name: "report.weekly", Subject: "Week 39",
		Bodies: []MailBody{TextBody("The week."), HTMLBody(`<p>The week.</p><img src="cid:cpu">`)},
		Inline: []InlineImage{{ContentID: "cpu", ContentType: "image/png", FileName: "cpu.png", Data: []byte{0x89, 'P', 'N', 'G'}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "ses-0001", messageID)

	require.Len(t, box.sent, 1)
	assert.Equal(t, "tim@defacile.nl", box.sent[0].To)
	assert.Equal(t, mailFrom, box.sent[0].From)
	require.Len(t, box.sent[0].Inline, 1)
	assert.Equal(t, "cpu", box.sent[0].Inline[0].ContentID)

	sent := attested(t, store, PredicateMailSent)
	require.Len(t, sent, 1)
	assert.Equal(t, attestationID, sent[0].ID)
	assert.Equal(t, NodeSource, sent[0].Source)
	assert.Equal(t, "report.weekly", sent[0].Attributes["template"])
	assert.Equal(t, NodeSource, sent[0].Attributes["plugin"])

	// "i would have expected to be able to click the main and see exactly what was sent."
	//
	// The images are kept whole, so the mail can be shown again as it went out.
	images, ok := sent[0].Attributes["images"].([]any)
	require.True(t, ok, "the images are kept: %#v", sent[0].Attributes["images"])
	require.Len(t, images, 1)
	cpu := images[0].(map[string]any)
	assert.Equal(t, "cpu", cpu["content_id"])
	assert.Equal(t, "image/png", cpu["content_type"])
	assert.Equal(t, "iVBORw==", cpu["data"])
}

// The node is held to what a plugin is held to: no address, no mail.
func TestTheNodeDoesNotMailAUserWithoutAnAddress(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, UserRecipient(access.User{ID: "USroot"}))

	_, _, err := s.SendAsNode(context.Background(), "USroot", NodeMail{Name: "report.weekly", Subject: "Week 39", Bodies: []MailBody{TextBody("The week.")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no email address")
	assert.Empty(t, box.sent)
	assert.Empty(t, attested(t, store, PredicateMailSent))
}

// "but i do still want the dark themed qntx tokens css email template"
func TestAPluginMailsFromQNTXsDarkTemplateByName(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: mailToken, Source: "garden", UserId: "UStim", Template: DarkTemplateName,
		Values: map[string]string{"subject": "Welkom", "body": "Hallo.", "link": "https://garden.test/app"},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	require.Len(t, box.sent, 1)
	// A QNTX window on the canvas, carried by bgcolor where a client drops styles.
	l, err := readLook()
	require.NoError(t, err)
	assert.Contains(t, bodyIn(t, box.sent[0], MailHTML), `bgcolor="`+l.canvas+`"`)
	assert.Contains(t, bodyIn(t, box.sent[0], MailHTML), `bgcolor="`+l.window+`"`)
	assert.Contains(t, bodyIn(t, box.sent[0], MailHTML), "Welkom</td>", "the subject titles the window")
	assert.Contains(t, bodyIn(t, box.sent[0], MailHTML), "Hallo.")
	assert.Contains(t, bodyIn(t, box.sent[0], MailHTML), `href="https://garden.test/app"`)

	set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: mailToken, Source: "garden", Name: DarkTemplateName,
		Template: &protocol.MailTemplate{Subject: "x", Bodies: []*protocol.MailTemplateBody{TemplateText("y")}},
	})
	require.NoError(t, err)
	assert.False(t, set.Success, "QNTX's own template names are QNTX's")
}

// Plugins can call before the node has handed the service what it needs.
func TestAMailServiceNotYetWiredSaysSo(t *testing.T) {
	s := NewMailServer(mailToken, zap.NewNop().Sugar())

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{"subject": "Welkom", "body": "Hallo."}))
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.Error)
}

// A PNG as far as the mail service looks: the signature, then anything.
var tinyPNG = append(append([]byte{}, pngSignature...), 'I', 'H', 'D', 'R')

// "plugins can send images"
func TestAPluginMailsAnImageItsHTMLShows(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	set, err := s.SetTemplate(context.Background(), &protocol.SetMailTemplateRequest{
		AuthToken: mailToken, Source: "garden", Name: "bed",
		Template: &protocol.MailTemplate{
			Subject: "Je bed", Bodies: []*protocol.MailTemplateBody{TemplateText("Je bed."), TemplateHTML(`<p>Je bed.</p><img src="cid:bed">`)},
		},
	})
	require.NoError(t, err)
	require.True(t, set.Success, set.Error)

	resp, err := s.Send(context.Background(), &protocol.SendMailRequest{
		AuthToken: mailToken, Source: "garden", UserId: "UStim", Template: "bed",
		Inline: []*protocol.MailImage{{ContentId: "bed", ContentType: "image/png", FileName: "bed.png", Data: tinyPNG}},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	require.Len(t, box.sent, 1)
	assert.Contains(t, bodyIn(t, box.sent[0], MailHTML), `src="cid:bed"`)
	require.Len(t, box.sent[0].Inline, 1)
	assert.Equal(t, InlineImage{ContentID: "bed", ContentType: "image/png", FileName: "bed.png", Data: tinyPNG}, box.sent[0].Inline[0])

	sent := attested(t, store, PredicateMailSent)
	require.Len(t, sent, 1)
	images, ok := sent[0].Attributes["images"].([]any)
	require.True(t, ok, "the images are kept: %#v", sent[0].Attributes["images"])
	require.Len(t, images, 1)
	assert.Equal(t, "bed", images[0].(map[string]any)["content_id"])
}

// "Add a size cap and accept image/png only."
func TestAPluginMailWithAnImageItMayNotSendIsNotSent(t *testing.T) {
	over := append(append([]byte{}, pngSignature...), make([]byte, MaxMailImageBytes)...)
	for name, tc := range map[string]struct {
		images []*protocol.MailImage
		says   string
	}{
		"a jpeg": {
			[]*protocol.MailImage{{ContentId: "bed", ContentType: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff}}},
			"only image/png",
		},
		"png in name only": {
			[]*protocol.MailImage{{ContentId: "bed", ContentType: "image/png", Data: []byte("<svg/>")}},
			"not a PNG",
		},
		"over the cap": {
			[]*protocol.MailImage{{ContentId: "bed", ContentType: "image/png", Data: over}},
			"more than",
		},
		"over the cap together": {
			[]*protocol.MailImage{
				{ContentId: "a", ContentType: "image/png", FileName: "x.png", Data: over[:MaxMailImageBytes/2+1]},
				{ContentId: "b", ContentType: "image/png", FileName: "x.png", Data: over[:MaxMailImageBytes/2+1]},
			},
			"more than",
		},
		"no content_id": {
			[]*protocol.MailImage{{ContentType: "image/png", Data: tinyPNG}},
			"content_id of inline image 0",
		},
		"no file_name": {
			[]*protocol.MailImage{{ContentId: "bed", ContentType: "image/png", Data: tinyPNG}},
			"file_name of inline image bed",
		},
		"one content_id twice": {
			[]*protocol.MailImage{
				{ContentId: "bed", ContentType: "image/png", FileName: "bed.png", Data: tinyPNG},
				{ContentId: "bed", ContentType: "image/png", FileName: "bed.png", Data: tinyPNG},
			},
			"repeats content_id bed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			box := &sentBox{}
			s, store := wiredMail(t, box, tim)

			req := neutralSend("UStim", map[string]string{"subject": "Welkom", "body": "Hallo."})
			req.Inline = tc.images
			resp, err := s.Send(context.Background(), req)
			require.NoError(t, err)
			assert.False(t, resp.Success)
			assert.Contains(t, resp.Error, tc.says)
			assert.Empty(t, box.sent)
			assert.Empty(t, attested(t, store, PredicateMailSent))
		})
	}
}

// QNTX's neutral template is named like any other: a Send naming none is
// refused, not filled from it.
func TestASendNamingNoTemplateIsRefused(t *testing.T) {
	box := &sentBox{}
	s, store := wiredMail(t, box, tim)

	req := neutralSend("UStim", map[string]string{"subject": "Welkom", "body": "Hallo."})
	req.Template = ""
	resp, err := s.Send(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, NeutralTemplateName)
	assert.Empty(t, box.sent)
	assert.Empty(t, attested(t, store, PredicateMailSent))
}

// "no mail is sent that cannot be attested": a mail that went out and was not
// recorded is answered as a failure, naming the id the transport gave it.
func TestAMailTheStoreRefusesToRecordIsAnsweredAsAFailure(t *testing.T) {
	box := &sentBox{}
	s, _ := wiredMail(t, box, tim)
	w, err := s.wiredWith()
	require.NoError(t, err)
	unwritten := *w
	unwritten.Actor = HavingNot[named.Text](errors.New("the node has no DID"))
	s.Wire(unwritten)

	resp, err := s.Send(context.Background(), neutralSend("UStim", map[string]string{"subject": "Welkom", "body": "Hallo."}))
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "not attested")
	assert.Equal(t, "ses-0001", resp.MessageId)
	assert.Len(t, box.sent, 1)
}
