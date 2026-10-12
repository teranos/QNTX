package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// A pushed deploy building, with two of its three checks passed.
func buildingDeploy(now time.Time) pluginDeploy {
	return pluginDeploy{
		plugin: "datapunt", rev: "f7cf853732c0aee6", repo: "teranos/datapunt", after: "f7cf853732c0aee6", pushed: true,
		stage:  deployBuilding,
		began:  map[string]time.Time{deployQueued: now.Add(-2 * time.Minute), deployBuilding: now.Add(-80 * time.Second)},
		checks: map[string]string{"test": checkDone, "lint": checkDone, "e2e": checkRunning},
	}
}

func drawDeploys(t *testing.T, format string, deploys ...pluginDeploy) *httptest.ResponseRecorder {
	t.Helper()
	h := aNodeWithPeers(noPeers)
	h.node = fullNode{}
	h.deploys = func() []pluginDeploy { return deploys }
	req := httptest.NewRequest(http.MethodGet, "/statusline?format="+format, nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), auth.Admitted(auth.LevelSuper)))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	return rec
}

// A deploy outranks everything on the row, and the carousel waits for it.
func TestADeployLeadsTheRowAndHoldsTheCarousel(t *testing.T) {
	rec := drawDeploys(t, FormatJSON, buildingDeploy(time.Now()))
	var body StatusLineResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2, "the row is the deploy and the caller, and no carousel: %+v", body.Items)

	deploy := body.Items[0]
	assert.Equal(t, "datapunt", deploy.Name)
	assert.Equal(t, "f7cf853", deploy.Note, "the note is the version coming in")
	require.Len(t, deploy.Bars, 2)
	assert.Equal(t, DeployBar{Kind: barChecks, Done: 2, Of: 3}, deploy.Bars[0])
	assert.Equal(t, DeployBar{Kind: barNode, Done: 1, Of: 3}, deploy.Bars[1])
}

// A live deploy is over: the row is the carousel's again.
func TestALiveDeployLeavesTheRow(t *testing.T) {
	live := buildingDeploy(time.Now())
	live.stage = deployLive
	rec := drawDeploys(t, FormatJSON, live)
	var body StatusLineResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2, "the caller and the carousel: %+v", body.Items)
	assert.Empty(t, body.Items[0].Bars)
	assert.Empty(t, body.Items[1].Bars)
}

// ug draws a deploy on the plugin's own name from the JSON row; the tmux line
// carries no copy of it, and no carousel while it is underway.
func TestTheTmuxLineLeavesTheDeployToUg(t *testing.T) {
	line := drawDeploys(t, FormatTmux, buildingDeploy(time.Now())).Body.String()
	assert.NotContains(t, line, "datapunt")
	assert.NotContains(t, line, "up")
	assert.False(t, strings.Contains(line, "\n"))
}

// A pushed deploy waits on its checks; their passing queues it, once.
func TestAPushedDeployIsQueuedOnceItsChecksPass(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", repo: "teranos/datapunt", after: "c0ffee", pushed: true, stage: deployChecking})
	require.True(t, s.deploys.awaiting("datapunt", "c0ffee"))
	assert.False(t, s.deploys.awaiting("datapunt", "beef"), "a newer push waits on its own commit")

	s.deploys.checksAre("datapunt", "c0ffee", map[string]string{"test": checkDone})
	require.True(t, s.deploys.checksPassed("datapunt", "c0ffee"))
	assert.False(t, s.deploys.checksPassed("datapunt", "c0ffee"), "a deploy was queued twice")

	held := s.deploys.held()
	require.Len(t, held, 1)
	assert.Equal(t, deployQueued, held[0].stage)
	assert.Equal(t, map[string]string{"test": checkDone}, held[0].checks)

	// The build takes up the deploy its checks queued, checks and all.
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", stage: deployBuilding})
	held = s.deploys.held()
	assert.Equal(t, map[string]string{"test": checkDone}, held[0].checks)
	assert.True(t, held[0].pushed)
}

// A check that failed ends the deploy at its checks, red.
func TestAFailedCheckEndsTheDeployAtItsChecks(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", repo: "teranos/datapunt", after: "c0ffee", pushed: true, stage: deployChecking})
	s.deploys.checksAre("datapunt", "c0ffee", map[string]string{"test": checkFailed})
	s.deploys.fail("datapunt", errors.New("checks on teranos/datapunt@c0ffee did not pass: test"))

	row := deployItems(s.deploys.held(), time.Now(), []News{})
	require.Len(t, row.items, 1)
	assert.True(t, row.items[0].Bars[0].Failed, "the checks bar is not red")
	assert.True(t, row.items[0].Bars[1].Failed, "the node's bar is not red")
	assert.Equal(t, 0, row.items[0].Bars[1].Done, "the node built nothing")
}

// A failed stage turns its bar red, and the deploy stays until its hold is
// over or other news is left.
func TestAFailedDeployStaysUntilTimeOrNews(t *testing.T) {
	now := time.Now()
	failed := buildingDeploy(now)
	failed.stage, failed.failedAt, failed.failure = deployFailed, deployBuilding, "dub build: exit 1"
	failed.began[deployFailed] = now.Add(-time.Minute)

	row := deployItems([]pluginDeploy{failed}, now, []News{})
	require.Len(t, row.items, 1)
	assert.False(t, row.underway, "a failed deploy held the carousel")
	assert.Equal(t, DeployBar{Kind: barNode, Done: 1, Of: 3, Failed: true}, row.items[0].Bars[1])

	assert.Empty(t, deployItems([]pluginDeploy{failed}, now.Add(newsHold), []News{}).items, "time did not clear it")
	assert.Empty(t, deployItems([]pluginDeploy{failed}, now, []News{{AtMs: now.UnixMilli()}}).items, "news did not clear it")
}
