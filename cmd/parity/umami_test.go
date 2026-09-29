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
	probes, err := server.StaandProbes()
	if err != nil {
		t.Fatal(err)
	}
	clades, err := UmamiReport(pinnedSchema(t), recorded, probes)
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

// TestUmamiReport_ScoresWhatIsRecorded pins what a stand records and how much of
// it takes Umami's shape today.
func TestUmamiReport_ScoresWhatIsRecorded(t *testing.T) {
	recorded, err := server.StaandRecorded()
	if err != nil {
		t.Fatal(err)
	}
	probes, err := server.StaandProbes()
	if err != nil {
		t.Fatal(err)
	}
	clades, err := UmamiReport(pinnedSchema(t), recorded, probes)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Item{}
	for _, c := range clades {
		for _, i := range c.Items {
			got[i.Name] = i
		}
	}
	for name, want := range map[string][3]int{
		"WebsiteEvent": {13, 9, 31},
		"Session":      {2, 0, 12},
		"EventData":    {2, 2, 9},
		"User":         {0, 0, 10},
	} {
		i := got[name]
		if i.Present != want[0] || i.Conform != want[1] || i.Total != want[2] {
			t.Errorf("%s is present %d, conforming %d, of %d; want %d, %d, of %d",
				name, i.Present, i.Conform, i.Total, want[0], want[1], want[2])
		}
	}
}

// TestUmamiReport_NamesWhatDeparts holds the findings to the columns where a stand
// and Umami differ, and to why.
func TestUmamiReport_NamesWhatDeparts(t *testing.T) {
	recorded, err := server.StaandRecorded()
	if err != nil {
		t.Fatal(err)
	}
	probes, err := server.StaandProbes()
	if err != nil {
		t.Fatal(err)
	}
	clades, err := UmamiReport(pinnedSchema(t), recorded, probes)
	if err != nil {
		t.Fatal(err)
	}
	var event Item
	for _, c := range clades {
		for _, i := range c.Items {
			if i.Name == "WebsiteEvent" {
				event = i
			}
		}
	}
	departs := map[string]string{}
	for _, f := range event.Findings {
		departs[f.Column] += f.Reason + "; "
	}
	for column, reason := range map[string]string{
		"visitId":   "required in Umami",
		"sessionId": "a UUID in Umami",
		"websiteId": "a UUID in Umami",
		"eventName": "at most 50 in Umami",
	} {
		if !strings.Contains(departs[column], reason) {
			t.Errorf("%s departs by %q, want it to say %q", column, departs[column], reason)
		}
	}
	for _, column := range []string{"urlPath", "createdAt", "utmSource", "referrerDomain"} {
		if departs[column] != "" {
			t.Errorf("%s departs by %q, and should not", column, departs[column])
		}
	}
}

// TestShapeOf_EachCheck holds each check to a column made to fail it and to one
// made to pass.
func TestShapeOf_EachCheck(t *testing.T) {
	kept := server.StaandProbe{Required: true, MaxLength: 10, Timestamp: true}
	cases := []struct {
		name   string
		column Column
		probe  server.StaandProbe
		every  bool
		want   string
	}{
		{"an int is not text", Column{Type: "Int"}, kept, true, "Int in Umami"},
		{"required and absent", Column{Type: "String"}, server.StaandProbe{}, true, "required in Umami"},
		{"required of a row that may not exist", Column{Type: "String"}, server.StaandProbe{}, false, ""},
		{"optional and absent", Column{Type: "String", Optional: true}, server.StaandProbe{}, true, ""},
		{"a UUID that keeps any string", Column{Type: "String", Native: "Uuid"}, server.StaandProbe{Required: true, KeepsNonUUID: true}, true, "a UUID in Umami"},
		{"a UUID that keeps only UUIDs", Column{Type: "String", Native: "Uuid"}, kept, true, ""},
		{"longer than the column", Column{Type: "String", Native: "VarChar", MaxLen: 5}, kept, true, "at most 5 in Umami"},
		{"as long as the column", Column{Type: "String", Native: "VarChar", MaxLen: 10}, kept, true, ""},
		{"no end to it", Column{Type: "String", Native: "VarChar", MaxLen: 500}, server.StaandProbe{Required: true, MaxLength: server.StaandProbeCap}, true, "1024 or more"},
		{"a timestamp that does not read", Column{Type: "DateTime"}, server.StaandProbe{Required: true}, true, "a timestamp in Umami"},
		{"a timestamp that reads", Column{Type: "DateTime"}, kept, true, ""},
	}
	for _, c := range cases {
		got := strings.Join(shapeOf(c.column, "field", c.probe, c.every), "; ")
		if c.want == "" && got != "" {
			t.Errorf("%s: departed by %q, want nothing", c.name, got)
		}
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want it to say %q", c.name, got, c.want)
		}
	}
}

// TestParsePrisma_ReadsTypes is the natives the checks depend on, on the pinned file.
func TestParsePrisma_ReadsTypes(t *testing.T) {
	models := pinnedSchema(t)
	var event Model
	for _, m := range models {
		if m.Name == "WebsiteEvent" {
			event = m
		}
	}
	if c := event.Columns["visitId"]; c.Native != "Uuid" || c.Optional {
		t.Errorf("visitId is %+v, want a required Uuid", c)
	}
	if c := event.Columns["urlPath"]; c.Native != "VarChar" || c.MaxLen != 500 || c.Optional {
		t.Errorf("urlPath is %+v, want a required VarChar(500)", c)
	}
	if c := event.Columns["eventName"]; c.MaxLen != 50 || !c.Optional {
		t.Errorf("eventName is %+v, want an optional VarChar(50)", c)
	}
	if c := event.Columns["createdAt"]; c.Type != "DateTime" || !c.Optional {
		t.Errorf("createdAt is %+v, want an optional DateTime", c)
	}
}

// TestStaandProbes_WhatTheHandlerDoes pins the probe answers the report rests on.
func TestStaandProbes_WhatTheHandlerDoes(t *testing.T) {
	probes, err := server.StaandProbes()
	if err != nil {
		t.Fatal(err)
	}
	if p := probes["visit"]; p.Required || !p.KeepsNonUUID {
		t.Errorf("visit is %+v: a hit can arrive without one, and any string of 8 or more is kept", p)
	}
	if p := probes["path"]; !p.Required || p.MaxLength != 128 {
		t.Errorf("path is %+v: a bare hit fills it, and it is cut at 128", p)
	}
	if p := probes["event"]; p.MaxLength != 128 {
		t.Errorf("event keeps up to %d, want 128", p.MaxLength)
	}
	if p := probes["utm_source"]; p.MaxLength != 128 {
		t.Errorf("utm_source keeps up to %d, want 128", p.MaxLength)
	}
	if p := probes["at"]; !p.Required || !p.Timestamp {
		t.Errorf("at is %+v: a bare hit fills it with a timestamp", p)
	}
	if p := probes["slug"]; !p.KeepsNonUUID || p.MaxLength != server.StaandProbeCap {
		t.Errorf("slug is %+v: any string, without end", p)
	}
}

// TestUmamiReport_UnmappedColumnIsAnError is a mapping that names a column the
// schema does not have.
func TestUmamiReport_UnmappedColumnIsAnError(t *testing.T) {
	_, err := UmamiReport([]Model{{Name: "WebsiteEvent", Fields: []string{"id"}}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not in the Umami schema") {
		t.Errorf("got %v, want an error naming a column the schema lacks", err)
	}
}

// TestItemScore_HundredOnlyWhenComplete is the floor: thirty of thirty-one is
// not a hundred.
func TestItemScore_HundredOnlyWhenComplete(t *testing.T) {
	if s := (Item{Conform: 30, Total: 31}).Score(); s != 96 {
		t.Errorf("30 of 31 scored %d, want 96", s)
	}
	if s := (Item{Conform: 31, Total: 31}).Score(); s != 100 {
		t.Errorf("31 of 31 scored %d, want 100", s)
	}
	if s := (Item{Conform: 0, Total: 31}).Score(); s != 0 {
		t.Errorf("0 of 31 scored %d, want 0", s)
	}
}

var shown = []Clade{
	{Name: "nothing", Items: []Item{{Name: "A", Total: 4}, {Name: "B", Total: 5}}},
	{Name: "all", Items: []Item{{Name: "C", Present: 3, Conform: 3, Total: 3}, {Name: "D", Present: 2, Conform: 2, Total: 2}}},
	{Name: "some", Items: []Item{{Name: "E", Total: 4}, {Name: "F", Present: 3, Conform: 3, Total: 3}, {Name: "G", Present: 3, Conform: 2, Total: 4,
		Findings: []Finding{{Column: "g1", Reason: "a UUID in Umami"}}}}},
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
	if !strings.Contains(out, "g1: a UUID in Umami") {
		t.Errorf("the finding under G is not listed:\n%s", out)
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

// TestFindOutOfSpec_SortsWhatHasNoColumn: a filled field with no column, a
// declared field nothing fills, and a mapped field that is neither.
func TestFindOutOfSpec_SortsWhatHasNoColumn(t *testing.T) {
	got := FindOutOfSpec(
		[]string{"path", "extra_filled", "extra_declared"},
		[]string{"path", "extra_filled"},
		[]string{"staand"},
	)
	if !slices.Equal(got.Recorded, []string{"extra_filled"}) {
		t.Errorf("recorded is %v, want [extra_filled]", got.Recorded)
	}
	if !slices.Equal(got.DeclaredOnly, []string{"extra_declared"}) {
		t.Errorf("declared only is %v, want [extra_declared]", got.DeclaredOnly)
	}
	if !slices.Equal(got.Attributes, []string{"staand"}) {
		t.Errorf("attributes is %v, want [staand]", got.Attributes)
	}
}

// TestOutOfSpec_WhatAStandCarriesToday pins it: every field a stand fills has an
// Umami column, the slug rides as an attribute, and three declared fields match
// none.
func TestOutOfSpec_WhatAStandCarriesToday(t *testing.T) {
	recorded, err := server.StaandRecorded()
	if err != nil {
		t.Fatal(err)
	}
	got := FindOutOfSpec(arrivalFields(), recorded, server.StaandExtraAttributes())
	if len(got.Recorded) != 0 {
		t.Errorf("a stand fills %v, which no Umami column matches", got.Recorded)
	}
	if !slices.Equal(got.Attributes, []string{"staand"}) {
		t.Errorf("attributes is %v, want [staand]", got.Attributes)
	}
	if !slices.Equal(got.DeclaredOnly, []string{"bot", "browser_version", "operating_system_version"}) {
		t.Errorf("declared only is %v", got.DeclaredOnly)
	}
}

// TestRenderOutOfSpec_NoneIsOneLine and a kind with names is one line each.
func TestRenderOutOfSpec_NoneIsOneLine(t *testing.T) {
	if out := RenderOutOfSpec(OutOfSpec{}); !strings.Contains(out, "out of spec: none") {
		t.Errorf("nothing out of spec did not say so:\n%s", out)
	}
	out := RenderOutOfSpec(OutOfSpec{Attributes: []string{"staand"}, DeclaredOnly: []string{"bot", "x"}})
	if !strings.Contains(out, "attributes") || !strings.Contains(out, "bot, x") || strings.Contains(out, "recorded") {
		t.Errorf("out of spec rendered wrongly:\n%s", out)
	}
}
