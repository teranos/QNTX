package server

import (
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

const (
	pushedRev   = "8ed95ffc2a6b0d4e1f3a5c7b9d0e2f4a6c8b0d1e"
	replacedRev = "beef1234c2a6b0d4e1f3a5c7b9d0e2f4a6c8b0d1"
)

// deployRow is a row whose node holds the deploys given and runs the plugins
// given, and whose carousels are on a clock the test moves.
func deployRow(t *testing.T, deploys *[]deployShown, running ...plugin.DomainPlugin) (*StatusLineHandler, *time.Time) {
	t.Helper()
	registry := plugin.NewRegistry("0.0.0", zap.NewNop().Sugar())
	for _, p := range running {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	h := NewStatusLineHandler(registry, zap.NewNop().Sugar(), func() pluginProbe { return nil },
		func() storage.Watchers { return nil },
		func() *handlerFailureLog { return nil }, fullNode{})
	var clock *time.Time
	h.carousel, clock = heldAt(time.Now())
	h.pluginCarousel.now = h.carousel.now
	h.deploys = func() []deployShown { return *deploys }
	return h, clock
}

func rootCaller() auth.Admission {
	a := auth.Admitted(auth.LevelRoot)
	a.DisplayName, a.Identity = auth.RootName, admittedRoute
	return a
}

// A deploy in flight comes before every other item: the row is the deploy and
// nothing else, the version it replaces, the one coming in, and how far it is.
func TestADeployInFlightIsTheWholeRow(t *testing.T) {
	deploys := []deployShown{{plugin: "datapunt", rev: pushedRev, replacesRevs: []string{replacedRev}, stage: deployBuilding, at: time.Now()}}
	h, _ := deployRow(t, &deploys, &fakePlugin{name: "datapunt"})

	row := rowAs(t, h, rootCaller()).Items
	want := StatusItem{Name: "datapunt", Note: "0.0.0 → 8ed95ff · building", Symbol: SymbolDeploying}
	if len(row) != 1 || row[0] != want {
		t.Fatalf("row is %+v, want only %+v", row, want)
	}

	// A plugin the node does not run yet replaces what it was built from.
	deploys = []deployShown{{plugin: "cleanAPI", rev: pushedRev, replacesRevs: []string{replacedRev}, stage: deployQueued, at: time.Now()}}
	if row := rowAs(t, h, rootCaller()).Items; len(row) != 1 || row[0].Note != "beef123 → 8ed95ff · queued" {
		t.Fatalf("row is %+v", row)
	}
	// One replacing nothing says only what comes in.
	deploys = []deployShown{{plugin: "cleanAPI", rev: pushedRev, stage: deployRestarting, at: time.Now()}}
	if row := rowAs(t, h, rootCaller()).Items; len(row) != 1 || row[0].Note != "→ 8ed95ff · restarting" {
		t.Fatalf("row is %+v", row)
	}
}

// The carousel holds while a deploy is in flight, and goes on once the new
// version is live.
func TestTheCarouselHoldsWhileADeployIsInFlight(t *testing.T) {
	deploys := []deployShown{{plugin: "datapunt", rev: pushedRev, stage: deployBuilding, at: time.Now()}}
	h, clock := deployRow(t, &deploys)

	for range 3 {
		*clock = clock.Add(time.Hour)
		rowAs(t, h, rootCaller())
	}
	if h.carousel.advances != 0 || !h.carousel.movesAt.IsZero() {
		t.Fatalf("the carousel moved under a deploy: %d advances", h.carousel.advances)
	}
	if !h.pluginCarousel.movesAt.IsZero() {
		t.Fatal("the plugins' carousel moved under a deploy")
	}

	deploys = []deployShown{{plugin: "datapunt", rev: pushedRev, stage: deployLive, at: *clock}}
	row := rowAs(t, h, rootCaller()).Items
	if len(row) != 2 || row[0].Name != auth.RootName || row[1].Note == "" {
		t.Fatalf("the row did not come back once the deploy went live: %+v", row)
	}
}

// "Time, or other news"
//
// A failed deploy says where it failed and why until then.
func TestAFailedDeployStaysUntilItIsOldOrSomethingNewerIs(t *testing.T) {
	failedAgo := func(ago time.Duration) deployShown {
		return deployShown{plugin: "datapunt", rev: pushedRev, stage: deployFailed, failedAt: deployBuilding,
			failure: "dub build: <exit 1>\nsource/app.d(12): Error: undefined identifier", at: time.Now().Add(-ago)}
	}
	deploys := []deployShown{failedAgo(time.Minute)}
	h, _ := deployRow(t, &deploys)

	row := rowAs(t, h, rootCaller()).Items
	want := StatusItem{Name: "datapunt", Note: "failed at building: dub build: <exit 1>", Symbol: SymbolUnwell}
	if len(row) != 3 || row[1] != want {
		t.Fatalf("row is %+v, want %+v after the caller", row, want)
	}
	if row[2].Name == "datapunt" {
		t.Fatalf("a failed deploy held the carousel: %+v", row)
	}

	deploys = []deployShown{failedAgo(deployFailedHold + time.Minute)}
	if failedOn(rowAs(t, h, rootCaller()).Items) {
		t.Fatal("a failed deploy past its hold is still on the row")
	}

	// Another deploy that began after it is newer.
	deploys = []deployShown{failedAgo(time.Minute), {plugin: "cleanAPI", rev: pushedRev, stage: deployLive, at: time.Now()}}
	if failedOn(rowAs(t, h, rootCaller()).Items) {
		t.Fatal("a failed deploy outlived a deploy after it")
	}

	// News left for the caller after it is newer.
	deploys = []deployShown{failedAgo(time.Minute)}
	l := newNewsLog()
	l.leave(News{ID: "n-1", For: newsKey(rootCaller()), Item: StatusItem{Name: "ci", Symbol: SymbolWell}, UntilMs: time.Now().Add(time.Minute).UnixMilli()})
	h.news = func() *newsLog { return l }
	if failedOn(rowAs(t, h, rootCaller()).Items) {
		t.Fatal("a failed deploy outlived news after it")
	}
}

// The row reads each deploy the node holds, at the stage it is at and since
// when.
func TestTheRowReadsTheDeploysTheNodeHolds(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: pushedRev, replacesRevs: []string{replacedRev}, stage: deployBuilding})
	s.deploys.fail("datapunt", errors.New("dub build: <exit 1>"))
	s.deploys.begin(pluginDeploy{plugin: "cleanAPI", rev: pushedRev, stage: deployQueued})

	shown := s.deploys.shown()
	if len(shown) != 2 || shown[0].plugin != "cleanAPI" || shown[1].plugin != "datapunt" {
		t.Fatalf("shown %+v", shown)
	}
	failed := shown[1]
	if failed.stage != deployFailed || failed.failedAt != deployBuilding || failed.failure != "dub build: <exit 1>" ||
		failed.replacesRevs[0] != replacedRev || failed.at.IsZero() {
		t.Fatalf("shown %+v", failed)
	}
}

func failedOn(row []StatusItem) bool {
	for _, it := range row {
		if it.Name == "datapunt" && strings.HasPrefix(it.Note, "failed at ") {
			return true
		}
	}
	return false
}

// A deploy in flight is spelled in a colour of its own on every surface.
func TestADeployIsSpelledInItsOwnColour(t *testing.T) {
	deploying := []StatusItem{{Name: "datapunt", Symbol: SymbolDeploying}}
	for name, p := range palettes {
		line := renderLine(deploying, p, false)
		if p.deploying == "" || p.deploying == p.well || p.deploying == p.unwell || !strings.Contains(line, p.deploying+"datapunt") {
			t.Fatalf("%s: a deploy is not in its own colour: %q", name, line)
		}
	}
}
