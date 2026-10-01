package server

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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

// A restart stops a build the node was running, and nothing pushes again, so
// the node starts each build that has no binary to show for it.
func TestUnbuiltIsEachEnabledBuildWithNoBinary(t *testing.T) {
	built := map[string]string{
		buildCore:    "teranos/QNTX@real-inboxes",
		buildCommand: "go build -o bin/qntx-inbox-plugin ./qntx-plugins/inbox/cmd/qntx-inbox-plugin",
		buildOutput:  "bin/qntx-inbox-plugin",
	}
	records := []grpcplugin.PluginRecord{
		{Name: "inbox", Enabled: true, Config: built},
		{Name: "installed", Enabled: true, Config: built},
		{Name: "off", Enabled: false, Config: built},
		{Name: "cleanAPI", Enabled: true, Config: map[string]string{}},
	}
	installed := func(name string) bool { return name == "installed" }

	unbuilt, refused := unbuilt(records, installed)
	if len(refused) != 0 {
		t.Fatalf("refused: %v", refused)
	}
	if len(unbuilt) != 1 || unbuilt[0].name != "inbox" {
		t.Fatalf("unbuilt: %+v", unbuilt)
	}
}

// A box's /tmp can be a tmpfs held in memory, smaller than one build.
func TestABuildWorksOnDiskUnderTheNodesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := buildsDir()
	if err != nil {
		t.Fatalf("buildsDir: %v", err)
	}
	if dir != filepath.Join(home, ".qntx", "builds") {
		t.Fatalf("builds in %s", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("%s is not a directory: %v", dir, err)
	}

	env := buildEnv("/w", "", nil)
	if !slices.Contains(env, "TMPDIR=/w/tmp") {
		t.Fatalf("the build's temp files go elsewhere: %v", env)
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
