package server

import (
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
