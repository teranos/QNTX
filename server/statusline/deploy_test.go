package statusline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/server/auth"
)

// A pushed deploy building, with two of its three checks passed.
func buildingDeploy(now time.Time) DeployData {
	return DeployData{
		Plugin: "datapunt", Rev: "f7cf853732c0aee6", Repo: "teranos/datapunt", Pushed: true,
		Stage:  "building",
		Began:  map[string]time.Time{"checks": now.Add(-3 * time.Minute), "queued": now.Add(-2 * time.Minute), "building": now.Add(-80 * time.Second)},
		Checks: map[string]string{"test": "done", "lint": "done", "e2e": "running"},
	}
}

func drawRowWith(t *testing.T, deploys ...DeployData) StatusLineResponse {
	t.Helper()
	h := aNodeWithPeers(NoPeers)
	h.node = fullNode{}
	h.Deploys = func() ([]DeployData, bool) { return deploys, inFlightOf(deploys) }
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
	assert.Equal(t, "building", d.Stage)
	assert.True(t, d.Pushed)
	assert.Equal(t, "teranos/datapunt", d.Repo)
	assert.Equal(t, "f7cf853732c0aee6", d.Rev)
	assert.Equal(t, map[string]string{"test": "done", "lint": "done", "e2e": "running"}, d.Checks)
	assert.Empty(t, d.Version, "a version said before any build said it")
	assert.Contains(t, d.Began, "checks")
	assert.Contains(t, d.Began, "building")
}

// An ended deploy is still sent; it holds the carousel no longer.
func TestAnEndedDeployIsSentAndTheCarouselIsBack(t *testing.T) {
	live := buildingDeploy(time.Now())
	live.Stage = "live"
	body := drawRowWith(t, live)
	require.Len(t, body.Items, 2, "the caller and the carousel: %+v", body.Items)
	require.Len(t, body.Deploys, 1)
	assert.Equal(t, "live", body.Deploys[0].Stage)
}

// The tmux line carries no deploy: it is data, on the JSON row.
func TestTheTmuxLineCarriesNoDeploy(t *testing.T) {
	h := aNodeWithPeers(NoPeers)
	h.node = fullNode{}
	h.Deploys = func() ([]DeployData, bool) { return []DeployData{buildingDeploy(time.Now())}, true }
	req := httptest.NewRequest(http.MethodGet, "/statusline?format=tmux", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), auth.Admitted(auth.LevelSuper)))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	assert.NotContains(t, rec.Body.String(), "datapunt")
}

// inFlightOf is whether any deploy has not ended, which is what holds the carousel.
func inFlightOf(deploys []DeployData) bool {
	for _, d := range deploys {
		switch d.Stage {
		case "live", "unchanged", "disabled", "failed", "interrupted":
		default:
			return true
		}
	}
	return false
}
