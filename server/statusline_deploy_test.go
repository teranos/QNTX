package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		began:  map[string]time.Time{deployChecking: now.Add(-3 * time.Minute), deployQueued: now.Add(-2 * time.Minute), deployBuilding: now.Add(-80 * time.Second)},
		checks: map[string]string{"test": checkDone, "lint": checkDone, "e2e": checkRunning},
	}
}

func drawRowWith(t *testing.T, deploys ...pluginDeploy) StatusLineResponse {
	t.Helper()
	h := aNodeWithPeers(noPeers)
	h.node = fullNode{}
	h.deploys = func() []pluginDeploy { return deploys }
	req := httptest.NewRequest(http.MethodGet, "/statusline?format=json", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), auth.Admitted(auth.LevelSuper)))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	var body StatusLineResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// The row carries each deploy as the node holds it, for the surface to draw,
// and the carousel waits while one is in flight.
func TestTheRowCarriesTheDeploysAsData(t *testing.T) {
	now := time.Now()
	body := drawRowWith(t, buildingDeploy(now))
	require.Len(t, body.Items, 1, "the carousel drew while a deploy is in flight: %+v", body.Items)
	require.Len(t, body.Deploys, 1)

	d := body.Deploys[0]
	assert.Equal(t, "datapunt", d.Plugin)
	assert.Equal(t, deployBuilding, d.Stage)
	assert.True(t, d.Pushed)
	assert.Equal(t, "teranos/datapunt", d.Repo)
	assert.Equal(t, "f7cf853732c0aee6", d.Rev)
	assert.Equal(t, map[string]string{"test": checkDone, "lint": checkDone, "e2e": checkRunning}, d.Checks)
	assert.Empty(t, d.Version, "a version said before any build said it")
	assert.Contains(t, d.Began, deployChecking)
	assert.Contains(t, d.Began, deployBuilding)
}

// Once the build said its version, the deploy carries it.
func TestADeployCarriesTheVersionItsBuildSaid(t *testing.T) {
	built := buildingDeploy(time.Now())
	built.version, built.versioned = "0.3.11-8ed95ff65b864797", true
	data, inFlight := deployData([]pluginDeploy{built})
	assert.True(t, inFlight)
	assert.Equal(t, "0.3.11-8ed95ff65b864797", data[0].Version)
}

// An ended deploy is still sent; it holds the carousel no longer.
func TestAnEndedDeployIsSentAndTheCarouselIsBack(t *testing.T) {
	live := buildingDeploy(time.Now())
	live.stage = deployLive
	body := drawRowWith(t, live)
	require.Len(t, body.Items, 2, "the caller and the carousel: %+v", body.Items)
	require.Len(t, body.Deploys, 1)
	assert.Equal(t, deployLive, body.Deploys[0].Stage)
}

// The tmux line carries no deploy: it is data, on the JSON row.
func TestTheTmuxLineCarriesNoDeploy(t *testing.T) {
	h := aNodeWithPeers(noPeers)
	h.node = fullNode{}
	h.deploys = func() []pluginDeploy { return []pluginDeploy{buildingDeploy(time.Now())} }
	req := httptest.NewRequest(http.MethodGet, "/statusline?format=tmux", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), auth.Admitted(auth.LevelSuper)))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	assert.NotContains(t, rec.Body.String(), "datapunt")
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
	s.deploys.incoming("datapunt", "0.3.11-8ed95ff65b864797")
	held = s.deploys.held()
	assert.Equal(t, map[string]string{"test": checkDone}, held[0].checks)
	assert.True(t, held[0].pushed)
	assert.Equal(t, "0.3.11-8ed95ff65b864797", held[0].version)
}

// A check that failed ends the deploy at its checks.
func TestAFailedCheckEndsTheDeployAtItsChecks(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", repo: "teranos/datapunt", after: "c0ffee", pushed: true, stage: deployChecking})
	s.deploys.checksAre("datapunt", "c0ffee", map[string]string{"test": checkFailed})
	s.deploys.fail("datapunt", errors.New("checks on teranos/datapunt@c0ffee did not pass: test"))

	data, inFlight := deployData(s.deploys.held())
	assert.False(t, inFlight)
	assert.Equal(t, deployFailed, data[0].Stage)
	assert.Equal(t, deployChecking, data[0].FailedAt)
	assert.Equal(t, map[string]string{"test": checkFailed}, data[0].Checks)
}
