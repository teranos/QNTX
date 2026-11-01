package subsystem

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// a is a subsystem for a test: one signum, one standing row, one built-in.
type a struct {
	name    string
	started bool
}

func (s *a) Declare() *protocol.Subsystem {
	return &protocol.Subsystem{
		Name: s.name,
		Signa: []*protocol.Signum{{
			Name:        s.name,
			Description: "a subsystem for a test",
			Sigils: []*protocol.Sigil{{
				Name:   "ping",
				Does:   "Answers nothing, to show the sigil is served.",
				Answer: "protocol.Empty",
				Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/" + s.name + "/ping"},
			}},
		}},
		Standing: []*protocol.WatcherRegistration{{
			Id: s.name + "-row", HandlerName: s.name + ".ping", Predicates: []string{s.name + ":pinged"}, MaxFiresPerSecond: 10,
		}},
		HandlerNames: []string{s.name + ".ping"},
		Says:         map[string]string{"protocol.Empty": "nothing"},
		GithubEvents: []string{"push"},
	}
}

func (s *a) Answers() map[string]map[string]sigil.Answer {
	return map[string]map[string]sigil.Answer{s.name: {"ping": func(context.Context, sigil.Sent) (any, *protocol.Refusal) {
		return &protocol.Empty{}, nil
	}}}
}

func (s *a) Start(Host) error { s.started = true; return nil }

// A subsystem registers once, by name, and is found among the registered.
func TestARegisteredSubsystemIsAmongTheRegistered(t *testing.T) {
	s := &a{name: "fake"}
	if err := Register(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Withdraw("fake") })
	held := Registered()
	if len(held) != 1 || held[0].Declare().GetName() != "fake" {
		t.Fatalf("Registered = %v", held)
	}
}

// Two subsystems of one name are one too many: the second is refused, and
// says which name, so the build that linked both learns it at start.
func TestASecondSubsystemOfTheSameNameIsRefused(t *testing.T) {
	if err := Register(&a{name: "twice"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Withdraw("twice") })
	err := Register(&a{name: "twice"})
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a second registration of twice: %v", err)
	}
}

// A subsystem with no name is nothing to register.
func TestASubsystemWithNoNameIsRefused(t *testing.T) {
	if err := Register(&a{name: ""}); err == nil {
		t.Fatal("a nameless subsystem was registered")
	}
}

// Signa pairs each declared signum with the functions that answer it: the
// shape from Declare, the functions from Answers, one sigil.Signum each.
func TestSignaPairsEachSignumWithItsAnswers(t *testing.T) {
	signa := Signa(&a{name: "fake"})
	if len(signa) != 1 {
		t.Fatalf("Signa = %d signa, want 1", len(signa))
	}
	if signa[0].GetName() != "fake" || signa[0].Answers["ping"] == nil {
		t.Fatalf("the signum fake came without its answer for ping: %+v", signa[0])
	}
	if err := signa[0].Check(); err != nil {
		t.Fatalf("the paired signum does not check: %v", err)
	}
}

// A standing row declared is a row of the standing table: a built-in by the
// handler's name, the structural filter as declared, enabled, at its rate.
func TestADeclaredStandingRowIsABuiltinRow(t *testing.T) {
	rows, err := Standing((&a{name: "fake"}).Declare())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.ID != "fake-row" || row.ActionType != storage.ActionTypeBuiltinExecute || !row.Enabled || row.MaxFiresPerSecond != 10 {
		t.Errorf("row %+v", row)
	}
	if !strings.Contains(row.ActionData, `"handler_name":"fake.ping"`) {
		t.Errorf("action data %q names no handler", row.ActionData)
	}
	if len(row.Filter.Predicates) != 1 || row.Filter.Predicates[0] != "fake:pinged" {
		t.Errorf("filter %+v", row.Filter)
	}
	for _, end := range []struct {
		name  string
		bound types.TimeBound
	}{{"start", row.Filter.TimeStart}, {"end", row.Filter.TimeEnd}} {
		_, bounded, err := end.bound.At(end.name)
		if err != nil || bounded {
			t.Errorf("a standing row watches every time, and this one's %s is %v %v", end.name, bounded, err)
		}
	}
}

// A row naming no filter would match every attestation; a row naming a
// handler the subsystem does not declare would reach nothing. Both refused.
func TestAStandingRowThatWatchesNothingOrReachesNothingIsRefused(t *testing.T) {
	decl := (&a{name: "fake"}).Declare()
	decl.Standing[0].Predicates = nil
	if _, err := Standing(decl); err == nil || !strings.Contains(err.Error(), "fake-row") {
		t.Fatalf("a row with no filter: %v", err)
	}
	decl = (&a{name: "fake"}).Declare()
	decl.Standing[0].HandlerName = "nobody.home"
	if _, err := Standing(decl); err == nil || !strings.Contains(err.Error(), "nobody.home") {
		t.Fatalf("a row reaching an undeclared handler: %v", err)
	}
}
