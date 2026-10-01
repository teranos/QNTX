package server

import (
	"context"
	"os"
	"path/filepath"
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
