package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
)

// The gate of the parity sigil: it gives of staands what make parity prisma
// gave, as recorded beside the pinned Umami schema before the command went.
func TestParityHoldsStaandsAsRecorded(t *testing.T) {
	s := &QNTXServer{}
	signum := s.paritySignum()
	answer, refused := signum.Answers["hold"](context.Background(), sigil.Sent{"signum": "staands"})
	if refused != nil {
		t.Fatalf("hold refused staands: %s", refused.GetSays())
	}
	held, ok := answer.(parity.Parity)
	if !ok {
		t.Fatalf("answer is %T, not parity.Parity", answer)
	}
	want, err := os.ReadFile(filepath.Join("parity", "umami_v3.3.1_ca661c7", "staands"))
	if err != nil {
		t.Fatal(err)
	}
	if got := held.Render(true); got != string(want) {
		t.Errorf("staands no longer reads as parity/umami_v3.3.1_ca661c7/staands records; got\n%s", got)
	}
	holds(t, signum, "hold", answer)
}

// What hold is asked that the node cannot answer is refused by the param.
func TestParityHoldRefuses(t *testing.T) {
	hold := (&QNTXServer{}).paritySignum().Answers["hold"]
	for name, c := range map[string]struct {
		sent  sigil.Sent
		param string
		why   string
	}{
		"no such signum":      {sigil.Sent{"signum": "nosuch"}, "signum", sigil.NotFound},
		"follows nothing":     {sigil.Sent{"signum": "parity"}, "signum", sigil.NotFound},
		"not a reference":     {sigil.Sent{"signum": "staands", "reference": "matomo"}, "reference", sigil.NotFound},
		"no such sigil":       {sigil.Sent{"signum": "staands", "sigil": "nosuch"}, "sigil", sigil.NotFound},
		"a sigil of no shape": {sigil.Sent{"signum": "staands", "sigil": "create"}, "sigil", sigil.Invalid},
	} {
		_, refused := hold(context.Background(), c.sent)
		if refused == nil || refused.GetParam() != c.param || refused.GetWhy() != c.why {
			t.Errorf("%s: refused %v, want %s on %s", name, refused, c.why, c.param)
		}
	}
}

// storage gives what make parity wrote, and says it is of the source.
func TestParityStorageIsWhatMakeParityWrote(t *testing.T) {
	signum := (&QNTXServer{}).paritySignum()
	answer, refused := signum.Answers["storage"](context.Background(), nil)
	if refused != nil {
		t.Fatalf("storage refused: %s", refused.GetSays())
	}
	given := answer.(map[string]any)
	if given["describes"] != "source" {
		t.Errorf("storage describes %v", given["describes"])
	}
	written, err := parity.Storage()
	if err != nil {
		t.Fatal(err)
	}
	things := given["things"].([]parity.Stored)
	if len(things) == 0 || len(things) != len(written) {
		t.Errorf("storage gave %d things, and make parity wrote %d", len(things), len(written))
	}
	holds(t, signum, "storage", answer)
}

// The gate of a2a: any signum held to AgentSkill by its shape. name follows,
// and id, description and tags, which the spec requires, follow nothing.
func TestParityHoldsEverySignumToA2A(t *testing.T) {
	signum := (&QNTXServer{}).paritySignum()
	for _, name := range []string{"staands", "parity"} {
		answer, refused := signum.Answers["hold"](context.Background(), sigil.Sent{"signum": name, "reference": "a2a"})
		if refused != nil {
			t.Fatalf("hold refused %s against a2a: %s", name, refused.GetSays())
		}
		held := answer.(parity.Parity)
		var skill *parity.Clade
		for i, c := range held.Clades {
			if c.Model == "AgentSkill" {
				skill = &held.Clades[i]
			}
		}
		if skill == nil {
			t.Fatalf("%s: no AgentSkill clade", name)
		}
		if skill.Score() != 12 {
			t.Errorf("%s: AgentSkill reads %d", name, skill.Score())
		}
		for _, item := range skill.Items {
			if item.Column == "name" && !item.Conforms() {
				t.Errorf("%s: name does not conform: %+v", name, item)
			}
		}
		want := []string{"AgentSkill.id", "AgentSkill.description", "AgentSkill.tags"}
		if strings.Join(held.Required, " ") != strings.Join(want, " ") {
			t.Errorf("%s: required and unfollowed is %v", name, held.Required)
		}
		if strings.Join(held.Unfollowed["protocol.Signum"], " ") != "follows sigils" {
			t.Errorf("%s: Signum unfollowed is %v", name, held.Unfollowed["protocol.Signum"])
		}
		holds(t, signum, "hold", answer)
	}
}
