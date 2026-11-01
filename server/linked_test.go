package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/subsystem"
)

// "QNTX needs a formal contract for pluggable, optional, compiled-in subsystems."
// linkedForTest is one: a signum, a standing row, a built-in, a delivery
// listener. Registered the way a real one is, from the package that links it.
type linkedForTest struct {
	host      subsystem.Host
	delivered []string
}

const linkedName = "fake"

func (f *linkedForTest) Declare() *protocol.Subsystem {
	return &protocol.Subsystem{
		Name: linkedName,
		Signa: []*protocol.Signum{{
			Name:        linkedName,
			Description: "a subsystem linked in for a test",
			Sigils: []*protocol.Sigil{{
				Name:   "ping",
				Does:   "Answers nothing, to show a linked subsystem's sigil is served.",
				Answer: "protocol.Empty",
				Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/fake/ping"},
			}},
		}},
		Standing: []*protocol.WatcherRegistration{{
			Id: "fake-pinged", HandlerName: "fake.ping", Predicates: []string{"fake:pinged"}, MaxFiresPerSecond: 10,
		}},
		HandlerNames: []string{"fake.ping"},
		// What its messages say, as its own says.json would hold it. Empty says
		// nothing in QNTX's .proto, so what is read of it is the subsystem's; a
		// field of QNTX's own, said again here, keeps QNTX's words.
		Says:         map[string]string{"protocol.Empty": "nothing, for a test", "protocol.Sigil.does": "not what QNTX says"},
		GithubEvents: []string{"push"},
	}
}

func (f *linkedForTest) Answers() map[string]map[string]sigil.Answer {
	return map[string]map[string]sigil.Answer{linkedName: {"ping": func(context.Context, sigil.Sent) (any, *protocol.Refusal) {
		return &protocol.Empty{}, nil
	}}}
}

type fakePing struct{}

func (fakePing) Name() string                              { return "fake.ping" }
func (fakePing) Execute(context.Context, *async.Job) error { return nil }
func (f *linkedForTest) Start(host subsystem.Host) error {
	f.host = host
	host.Registry().Register(fakePing{})
	return nil
}
func (f *linkedForTest) Delivered(_ context.Context, event string, body []byte) (any, error) {
	f.delivered = append(f.delivered, event)
	var e struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, err
	}
	return []string{e.Ref}, nil
}

func linkedServer(t *testing.T) (*QNTXServer, *linkedForTest) {
	t.Helper()
	f := &linkedForTest{}
	require.NoError(t, subsystem.Register(f))
	t.Cleanup(func() { subsystem.Withdraw(linkedName) })
	store, db := createTestStore(t)
	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 1)
	require.NoError(t, err)
	return srv, f
}

// The boot step starts every linked subsystem with the node as its host, and
// what it registers there is in the daemon's registry like QNTX's own.
func TestALinkedSubsystemIsStartedWithTheNodeAsItsHost(t *testing.T) {
	srv, f := linkedServer(t)
	require.NotNil(t, f.host, "Start was not called")
	assert.True(t, srv.GetDaemon().Registry().Has("fake.ping"))
	// What the host hands out is the node's own.
	assert.NotNil(t, f.host.News())
	assert.NotNil(t, f.host.Logger())
	_, err := f.host.Read("nowhere")
	assert.Error(t, err, "a namespace the node does not hold is refused, not invented")
}

// Its signum is served as the node's own: checked, bound to its path, laid
// into the document and offered as a tool with the rest.
func TestALinkedSubsystemsSignumIsServed(t *testing.T) {
	srv, _ := linkedServer(t)
	var names []string
	for _, s := range srv.checkedSigna() {
		names = append(names, s.GetName())
	}
	assert.Contains(t, names, linkedName)
	answered := srv.answeredFromSigils()
	assert.Contains(t, answered, "/api/fake/ping")
}

// What its messages say of themselves is read where QNTX's own are read, so a
// sigil's fields are described and the signum is served; what QNTX's own say
// is not overridden by a subsystem saying it again.
func TestALinkedSubsystemsMessagesSayWhatTheyDeclare(t *testing.T) {
	linkedServer(t)
	said, err := parity.OursSay("protocol.Empty")
	require.NoError(t, err)
	assert.Equal(t, "nothing, for a test", said)
	own, err := parity.OursSay("protocol.Sigil.does")
	require.NoError(t, err)
	assert.Equal(t, "What it is for, in words, for somebody who has never seen the code.", own)
}

// Its standing rows stand with QNTX's own: every namespace watches them.
func TestALinkedSubsystemsRowsAreStanding(t *testing.T) {
	linkedServer(t)
	assert.True(t, watcher.IsStanding("fake-pinged"))
	var found bool
	for _, w := range watcher.Standing() {
		if w.ID == "fake-pinged" && strings.Contains(w.ActionData, "fake.ping") {
			found = true
		}
	}
	assert.True(t, found, "the row is not among what Standing hands out")
}

// A delivery is handed to each linked subsystem that reads its event, and
// what each answered is in the webhook's response under the subsystem's name.
func TestALinkedSubsystemReadsTheDeliveriesItNames(t *testing.T) {
	srv, f := linkedServer(t)
	said := srv.deliveries(context.Background(), "push", []byte(`{"ref":"refs/heads/main"}`))
	assert.Equal(t, map[string]any{linkedName: []string{"refs/heads/main"}}, said)
	assert.Equal(t, []string{"push"}, f.delivered)

	said = srv.deliveries(context.Background(), "pull_request", []byte(`{}`))
	assert.Empty(t, said, "an event the subsystem does not name is not handed to it")
	assert.Equal(t, []string{"push"}, f.delivered)
}

// A listener that fails is said in the response under its name, as the
// fixtures scanner is, not as a failed delivery GitHub would hand in again.
func TestALinkedSubsystemThatFailsADeliveryIsSaidByName(t *testing.T) {
	srv, _ := linkedServer(t)
	said := srv.deliveries(context.Background(), "push", []byte(`not json`))
	got, _ := said[linkedName].(string)
	assert.True(t, strings.HasPrefix(got, "error: "), "said %v", said)
}
