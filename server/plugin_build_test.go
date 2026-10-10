package server

import (
	"context"
	"errors"
	"github.com/teranos/QNTX/plugin"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"go.uber.org/zap"
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

// On start the node asks after each enabled plugin it builds, and no other.
func TestEnabledBuildsAreEachEnabledPluginQNTXBuilds(t *testing.T) {
	built := map[string]string{
		buildCore:    "teranos/QNTX@real-inboxes",
		buildCommand: "go build -o bin/qntx-inbox-plugin ./cmd/qntx-inbox-plugin",
		buildOutput:  "bin/qntx-inbox-plugin",
	}
	records := []grpcplugin.PluginRecord{
		{Name: "inbox", Enabled: true, Config: built},
		{Name: "off", Enabled: false, Config: built},
		{Name: "cleanAPI", Enabled: true, Config: map[string]string{}},
	}

	builds, refused := enabledBuilds(records)
	if len(refused) != 0 {
		t.Fatalf("refused: %v", refused)
	}
	if len(builds) != 1 || builds[0].name != "inbox" {
		t.Fatalf("builds: %+v", builds)
	}
}

// A build that failed is not built and mailed again at every start; a push,
// or a change to its record, is a different build and is tried.
func TestAFailedBuildIsTriedOncePerRevsAndRecipe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := pluginBuild{name: "cleanAPI", command: "cargo build", packages: []string{"cargo", "rustc"}, output: "target/release/cleanAPI"}
	key := b.failedKey([]string{"c0ffee"})

	if failedBefore("cleanAPI", key) {
		t.Fatal("nothing failed yet, and a build counts as failed")
	}
	if err := keepFailed("cleanAPI", key); err != nil {
		t.Fatalf("keep: %v", err)
	}
	if !failedBefore("cleanAPI", key) {
		t.Fatal("the same build at the same revs is not known to have failed")
	}
	if failedBefore("cleanAPI", b.failedKey([]string{"beef"})) {
		t.Fatal("a push to a new rev is taken as already failed")
	}
	fixed := b
	fixed.packages = []string{"cargo", "rustc", "protobuf"}
	if failedBefore("cleanAPI", fixed.failedKey([]string{"c0ffee"})) {
		t.Fatal("a record changed at the same revs is taken as already failed")
	}
	if err := keepFailed("cleanAPI", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if failedBefore("cleanAPI", key) {
		t.Fatal("a built plugin still counts its earlier failure")
	}
}

// A build killed by a restart, or a push while the node was down, leaves a
// binary built from older sources; what it was built from is kept beside it,
// so the next start sees the sources moved.
func TestABuildKeepsWhatItWasBuiltFrom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := grpcplugin.PluginInstallPath("inbox")
	if err != nil {
		t.Fatalf("install path: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if revs := installedRevs("inbox"); revs != nil {
		t.Fatalf("no binary, and revs %v", revs)
	}
	if err := os.WriteFile(filepath.Join(dir, grpcplugin.PluginBinaryName("inbox")), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if revs := installedRevs("inbox"); revs != nil {
		t.Fatalf("a binary QNTX did not build, and revs %v", revs)
	}
	if err := keepBuiltRevs("inbox", []string{"c0ffee", "beef"}); err != nil {
		t.Fatalf("keep: %v", err)
	}
	if revs := installedRevs("inbox"); strings.Join(revs, " ") != "c0ffee beef" {
		t.Fatalf("revs %v", revs)
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

// "needs to be kinder with the system, we can tolerate slower less resource intensive builds"
//
// "18 minutes is also too slow"
func TestABuildRunsAtTheLowestPriority(t *testing.T) {
	env := buildEnv("/w", "", nil)
	for _, held := range []string{"CARGO_BUILD_JOBS=1", "GOFLAGS=-p=1", "MAKEFLAGS=-j1"} {
		if slices.Contains(env, held) {
			t.Fatalf("%s holds the build to one job: %v", held, env)
		}
	}
	name, args := gentle("/nix/bin/nix", []string{"shell"})
	if name != "nice" || !slices.Equal(args, []string{"-n", "19", "/nix/bin/nix", "shell"}) {
		t.Fatalf("the build runs as %s %v", name, args)
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

// A deploy stops the node, and a build it cut off is not a build that failed:
// it is not kept as one, so the next start builds it.
func TestABuildStoppedWithTheNodeIsNotAFailure(t *testing.T) {
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.NewNop().Sugar()}
	running, stop := context.WithCancel(context.Background())
	if s.stoppedWithTheNode(running) {
		t.Fatal("a build in a running node reads as stopped with it")
	}
	stop()
	if !s.stoppedWithTheNode(running) {
		t.Fatal("a build in a node that stopped does not read as stopped with it")
	}
}

// openrouter-qntx's build was terminated by a deploy's stop while its node
// drained with its context not yet ended, and was kept as failed.
func TestABuildCutOffWhileTheNodeDrainsIsNotAFailure(t *testing.T) {
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.NewNop().Sugar()}
	s.setState(ServerStateDraining)
	if !s.stoppedWithTheNode(context.Background()) {
		t.Fatal("a build in a draining node does not read as stopped with it")
	}
}
