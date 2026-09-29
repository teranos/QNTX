package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server"
)

func pinnedSchema(t *testing.T) []Model {
	t.Helper()
	models, err := ParsePrisma(filepath.Join("..", "..", umamiDir, "schema.prisma"))
	if err != nil {
		t.Fatal(err)
	}
	return models
}

func fieldsOf(t *testing.T, models []Model, name string) []string {
	t.Helper()
	for _, m := range models {
		if m.Name == name {
			return m.Fields
		}
	}
	t.Fatalf("model %s is not in the pinned schema", name)
	return nil
}

// TestParsePrisma_PinnedSchema holds the pinned file to what it was read as:
// relations are not fields, so a model's count is its scalars.
func TestParsePrisma_PinnedSchema(t *testing.T) {
	models := pinnedSchema(t)
	if len(models) != 24 {
		t.Errorf("got %d models, want 24", len(models))
	}
	if n := len(fieldsOf(t, models, "WebsiteEvent")); n != 31 {
		t.Errorf("WebsiteEvent has %d fields, want 31", n)
	}
	if n := len(fieldsOf(t, models, "Session")); n != 12 {
		t.Errorf("Session has %d fields, want 12", n)
	}
	if slices.Contains(fieldsOf(t, models, "WebsiteEvent"), "website") {
		t.Errorf("a relation was read as a field")
	}
}

// TestUmamiColumns_ArrivalFieldsExist is what keeps the mapping from naming an
// Arrival field the proto does not have.
func TestUmamiColumns_ArrivalFieldsExist(t *testing.T) {
	declared := map[string]bool{}
	fields := (&protocol.Arrival{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		declared[string(fields.Get(i).Name())] = true
	}
	for _, m := range umamiColumns {
		if !declared[m.Arrival] {
			t.Errorf("%s is mapped to Umami and is not an Arrival field", m.Arrival)
		}
	}
}

// TestStandRecords_WhatTheHandlerFills is the record as staandArrival builds
// it. The fields the proto declares for what the request cannot tell are not
// filled, and staand_test.go holds the handler to that.
func TestStandRecords_WhatTheHandlerFills(t *testing.T) {
	recorded, err := server.StaandRecorded()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"at", "market", "slug", "visitor", "visit", "path", "event", "referrer_domain", "referrer_path", "utm_source", "utm_term", "params"} {
		if !slices.Contains(recorded, want) {
			t.Errorf("%s is not recorded, got %v", want, recorded)
		}
	}
	for _, not := range []string{"browser", "country", "bot", "query"} {
		if slices.Contains(recorded, not) {
			t.Errorf("%s is recorded, and nothing fills it", not)
		}
	}
}

// TestUmamiReport_EveryModelIsInOneClade fails when the schema has a model no
// clade names, or a clade names one the schema lacks.
func TestUmamiReport_EveryModelIsInOneClade(t *testing.T) {
	recorded, err := server.StaandRecorded()
	if err != nil {
		t.Fatal(err)
	}
	clades, err := UmamiReport(pinnedSchema(t), recorded)
	if err != nil {
		t.Fatal(err)
	}
	items := 0
	for _, c := range clades {
		items += len(c.Items)
	}
	if items != 24 {
		t.Errorf("the clades hold %d models, want 24", items)
	}
}

// TestUmamiReport_ScoresWhatIsRecorded pins the numbers the record gives today.
func TestUmamiReport_ScoresWhatIsRecorded(t *testing.T) {
	recorded, err := server.StaandRecorded()
	if err != nil {
		t.Fatal(err)
	}
	clades, err := UmamiReport(pinnedSchema(t), recorded)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Item{}
	for _, c := range clades {
		for _, i := range c.Items {
			got[i.Name] = i
		}
	}
	for name, want := range map[string][2]int{
		"WebsiteEvent": {13, 31},
		"Session":      {2, 12},
		"EventData":    {2, 9},
		"User":         {0, 10},
	} {
		if got[name].Covered != want[0] || got[name].Total != want[1] {
			t.Errorf("%s is %d/%d, want %d/%d", name, got[name].Covered, got[name].Total, want[0], want[1])
		}
	}
}

// TestUmamiReport_UnmappedColumnIsAnError is a mapping that names a column the
// schema does not have.
func TestUmamiReport_UnmappedColumnIsAnError(t *testing.T) {
	_, err := UmamiReport([]Model{{Name: "WebsiteEvent", Fields: []string{"id"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "not in the Umami schema") {
		t.Errorf("got %v, want an error naming a column the schema lacks", err)
	}
}

// TestItemScore_HundredOnlyWhenComplete is the floor: thirty of thirty-one is
// not a hundred.
func TestItemScore_HundredOnlyWhenComplete(t *testing.T) {
	if s := (Item{Covered: 30, Total: 31}).Score(); s != 96 {
		t.Errorf("30 of 31 scored %d, want 96", s)
	}
	if s := (Item{Covered: 31, Total: 31}).Score(); s != 100 {
		t.Errorf("31 of 31 scored %d, want 100", s)
	}
	if s := (Item{Covered: 0, Total: 31}).Score(); s != 0 {
		t.Errorf("0 of 31 scored %d, want 0", s)
	}
}

var shown = []Clade{
	{Name: "nothing", Items: []Item{{"A", 0, 4}, {"B", 0, 5}}},
	{Name: "all", Items: []Item{{"C", 3, 3}, {"D", 2, 2}}},
	{Name: "some", Items: []Item{{"E", 0, 4}, {"F", 3, 3}, {"G", 2, 4}}},
}

// TestRenderUmami_ACladeOfZerosIsOneLine: the members of a clade with nothing in
// it are not listed.
func TestRenderUmami_ACladeOfZerosIsOneLine(t *testing.T) {
	out := RenderUmami(shown, false)
	if !strings.Contains(out, "nothing") || !strings.Contains(out, "(2 models)") {
		t.Errorf("the clade of zeros is not one line:\n%s", out)
	}
	if strings.Contains(out, "    A ") || strings.Contains(out, "    B ") {
		t.Errorf("a member of a clade of zeros was listed:\n%s", out)
	}
}

// TestRenderUmami_AnythingAboveZeroOpensTheWholeClade: the zero and the hundred
// in it are listed too.
func TestRenderUmami_AnythingAboveZeroOpensTheWholeClade(t *testing.T) {
	out := RenderUmami(shown, false)
	for _, member := range []string{"E ", "F ", "G "} {
		if !strings.Contains(out, "    "+member) {
			t.Errorf("member %q of an open clade is not listed:\n%s", member, out)
		}
	}
	if !strings.Contains(out, "50") {
		t.Errorf("G at 2 of 4 does not read 50:\n%s", out)
	}
}

// TestRenderUmami_AHundredIsHiddenUntilAll: a clade at 100 throughout is left
// out by default and shown with all.
func TestRenderUmami_AHundredIsHiddenUntilAll(t *testing.T) {
	if out := RenderUmami(shown, false); strings.Contains(out, "  all ") {
		t.Errorf("a clade at 100 throughout was shown:\n%s", out)
	}
	if out := RenderUmami(shown, true); !strings.Contains(out, "  all ") {
		t.Errorf("all did not show the clade at 100:\n%s", out)
	}
}
