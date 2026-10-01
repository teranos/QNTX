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

// GitHubService held to GitHub's own description: what the github signum
// follows is read from its messages, and gives what is recorded beside the
// pinned description. A model at 100 is not recorded.
func TestParityHoldsGitHubAsRecorded(t *testing.T) {
	s := &QNTXServer{}
	signum := s.paritySignum()
	answer, refused := signum.Answers["hold"](context.Background(), sigil.Sent{"signum": "github"})
	if refused != nil {
		t.Fatalf("hold refused github: %s", refused.GetSays())
	}
	held, ok := answer.(parity.Parity)
	if !ok {
		t.Fatalf("answer is %T, not parity.Parity", answer)
	}
	want, err := os.ReadFile(filepath.Join("parity", "github_2026-03-10_7bdf5f0", "github"))
	if err != nil {
		t.Fatal(err)
	}
	if got := held.Render(false); got != string(want) {
		t.Errorf("github no longer reads as parity/github_2026-03-10_7bdf5f0/github records; got\n%s", got)
	}
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

// The gate of mcp: every sigil held to Tool by its shape, as mcp.go makes one.
// name, description and annotations follow and conform; inputSchema follows
// takes and outputSchema gives, and both depart, since each is a list and the
// schema one object.
func TestParityHoldsEverySigilToMCP(t *testing.T) {
	signum := (&QNTXServer{}).paritySignum()
	for _, name := range []string{"staands", "parity"} {
		answer, refused := signum.Answers["hold"](context.Background(), sigil.Sent{"signum": name, "reference": "mcp"})
		if refused != nil {
			t.Fatalf("hold refused %s against mcp: %s", name, refused.GetSays())
		}
		held := answer.(parity.Parity)
		var tool *parity.Clade
		for i, c := range held.Clades {
			if c.Model == "Tool" {
				tool = &held.Clades[i]
			}
		}
		if tool == nil {
			t.Fatalf("%s: no Tool clade", name)
		}
		if tool.Score() != 37 {
			t.Errorf("%s: Tool reads %d", name, tool.Score())
		}
		for _, item := range tool.Items {
			switch item.Column {
			case "name", "description", "annotations":
				if !item.Conforms() {
					t.Errorf("%s: %s does not conform: %+v", name, item.Column, item)
				}
			case "inputSchema":
				if item.Conforms() || strings.Join(item.Departs, ";") != "one value in the schema, and protocol.Sigil.takes is repeated" {
					t.Errorf("%s: inputSchema is %+v", name, item)
				}
			case "outputSchema":
				if item.Conforms() || strings.Join(item.Departs, ";") != "one value in the schema, and protocol.Sigil.gives is repeated" {
					t.Errorf("%s: outputSchema is %+v", name, item)
				}
			}
		}
		if len(held.Required) != 0 {
			t.Errorf("%s: required and unfollowed is %v", name, held.Required)
		}
		// "seeing prose from the specs themselves where they have it": theirs on
		// the column, ours by field.
		for _, item := range tool.Items {
			if item.Column == "description" && !strings.HasPrefix(item.Says, "A human-readable description of the tool.") {
				t.Errorf("%s: Tool.description says %q", name, item.Says)
			}
			if item.Column == "name" && !item.Required {
				t.Errorf("%s: Tool.name is not required", name)
			}
		}
		if held.Ours["protocol.Sigil.does"] != "What it is for, in words, for somebody who has never seen the code." {
			t.Errorf("%s: Sigil.does says %q", name, held.Ours["protocol.Sigil.does"])
		}
		if len(held.Unfollowed["protocol.Sigil"]) != 0 {
			t.Errorf("%s: Sigil unfollowed is %v", name, held.Unfollowed["protocol.Sigil"])
		}
		holds(t, signum, "hold", answer)
	}
}

// The gate of 4a: the node held to AgentCard, by what am declares of itself.
// name, description, version and skills follow; what an A2A binding would
// fill does not, and is said to be required.
func TestParityHoldsTheNodeToAgentCard(t *testing.T) {
	signum := (&QNTXServer{}).paritySignum()
	answer, refused := signum.Answers["hold"](context.Background(), sigil.Sent{"signum": "am"})
	if refused != nil {
		t.Fatalf("hold refused am: %s", refused.GetSays())
	}
	held := answer.(parity.Parity)
	if held.Reference != "a2a" {
		t.Fatalf("am was held to %s", held.Reference)
	}
	scores := map[string]int{}
	for _, c := range held.Clades {
		scores[c.Model] = c.Score()
	}
	if scores["AgentCard"] != 28 || scores["AgentSkill"] != 12 {
		t.Errorf("AgentCard reads %d and AgentSkill %d", scores["AgentCard"], scores["AgentSkill"])
	}
	want := []string{
		"AgentCard.supported_interfaces", "AgentCard.capabilities",
		"AgentCard.default_input_modes", "AgentCard.default_output_modes",
		"AgentSkill.id", "AgentSkill.description", "AgentSkill.tags",
	}
	if strings.Join(held.Required, " ") != strings.Join(want, " ") {
		t.Errorf("required and unfollowed is %v", held.Required)
	}
	holds(t, signum, "hold", answer)
}

// follows is what the parity window offers to hold: every signum, what it
// declares it follows, and what every signum follows by its shape.
func TestParityFollowsIsEverySignumAndItsReferences(t *testing.T) {
	signum := (&QNTXServer{}).paritySignum()
	answer, refused := signum.Answers["follows"](context.Background(), nil)
	if refused != nil {
		t.Fatalf("follows refused: %s", refused.GetSays())
	}
	rows := answer.([]map[string]any)
	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[row["signum"].(string)] = row
	}
	staands, ok := byName["staands"]
	if !ok {
		t.Fatalf("follows has no staands: %v", rows)
	}
	if strings.Join(staands["declares"].([]string), " ") != "umami" {
		t.Errorf("staands declares %v", staands["declares"])
	}
	if strings.Join(staands["by_shape"].([]string), " ") != "a2a mcp" {
		t.Errorf("by its shape staands follows %v", staands["by_shape"])
	}
	if len(byName["parity"]["declares"].([]string)) != 0 {
		t.Errorf("parity declares %v", byName["parity"]["declares"])
	}
	holds(t, signum, "follows", answer)
}
