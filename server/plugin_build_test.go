package server

import (
	"errors"
	"strings"
	"testing"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

func TestBuildOfReadsWhatDatapuntIsBuiltFrom(t *testing.T) {
	record := grpcplugin.PluginRecord{
		Name:    "datapunt",
		Enabled: true,
		Config: map[string]string{
			buildCore:      "teranos/datapunt@main",
			buildInputs:    "abcd-nl/clean@main:competitor.cue abcd-nl/q.abcd.nl@master:company.cue",
			buildInputsEnv: "DATAPUNT_SCHEMAS",
			buildPackages:  "ldc dub",
			buildCommand:   "dub build --config=plugin --compiler=ldc2 --build=release",
			buildOutput:    "bin/qntx-datapunt-plugin",
		},
	}
	b, built, err := buildOf(record)
	if err != nil || !built {
		t.Fatalf("built=%v err=%v", built, err)
	}
	if b.core != (buildSource{Owner: "teranos", Repo: "datapunt", Branch: "main"}) {
		t.Fatalf("core: %+v", b.core)
	}
	want := []buildSource{
		{Owner: "abcd-nl", Repo: "clean", Branch: "main", Path: "competitor.cue"},
		{Owner: "abcd-nl", Repo: "q.abcd.nl", Branch: "master", Path: "company.cue"},
	}
	if len(b.inputs) != len(want) || b.inputs[0] != want[0] || b.inputs[1] != want[1] {
		t.Fatalf("inputs: %+v", b.inputs)
	}
	if len(b.sources()) != 3 {
		t.Fatalf("sources: %+v", b.sources())
	}
}

func TestAFailedBuildsMailNamesEverySourceAndWhy(t *testing.T) {
	b := pluginBuild{
		name:   "datapunt",
		core:   buildSource{Owner: "teranos", Repo: "datapunt", Branch: "main"},
		inputs: []buildSource{{Owner: "abcd-nl", Repo: "clean", Branch: "main", Path: "competitor.cue"}},
	}
	mail := buildFailureMail(b, []string{"c0ffee"}, errors.New("dub build: <exit 1>"))
	if mail.Subject != "datapunt did not build" {
		t.Fatalf("subject: %q", mail.Subject)
	}
	for _, want := range []string{"teranos/datapunt@main at c0ffee", "abcd-nl/clean@main:competitor.cue at not read", "dub build: <exit 1>"} {
		if !strings.Contains(mail.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, mail.Text)
		}
	}
	if !strings.Contains(mail.HTML, "&lt;exit 1&gt;") {
		t.Errorf("html not escaped: %s", mail.HTML)
	}
}

func TestBuildOfLeavesAPluginWithoutABuildAlone(t *testing.T) {
	_, built, err := buildOf(grpcplugin.PluginRecord{Name: "cleanAPI", Config: map[string]string{}})
	if err != nil || built {
		t.Fatalf("built=%v err=%v", built, err)
	}
}

func TestBuildOfRefusesAnIncompleteBuild(t *testing.T) {
	for name, config := range map[string]map[string]string{
		"no branch":  {buildCore: "teranos/datapunt", buildCommand: "x", buildOutput: "y"},
		"no command": {buildCore: "teranos/datapunt@main", buildOutput: "y"},
		"no output":  {buildCore: "teranos/datapunt@main", buildCommand: "x"},
		"input without env": {buildCore: "teranos/datapunt@main", buildCommand: "x", buildOutput: "y",
			buildInputs: "abcd-nl/clean@main:competitor.cue"},
		"input without file": {buildCore: "teranos/datapunt@main", buildCommand: "x", buildOutput: "y",
			buildInputs: "abcd-nl/clean@main", buildInputsEnv: "E"},
	} {
		if _, _, err := buildOf(grpcplugin.PluginRecord{Name: "p", Config: config}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
