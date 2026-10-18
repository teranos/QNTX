package statusline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors "github.com/teranos/sacred-error"

	"github.com/teranos/QNTX/server/auth"
)

// A peer's row as the peer answers a primary: its items, no caller.
func peerRowBody(t *testing.T, items ...StatusItem) []byte {
	t.Helper()
	body, err := json.Marshal(StatusLineResponse{Items: items})
	require.NoError(t, err)
	return body
}

func superRoot() auth.Admission {
	admitted := auth.Admitted(auth.LevelSuper)
	admitted.Identity = admittedRoute
	admitted.DisplayName = auth.RootName
	return admitted
}

func drawRow(t *testing.T, h *StatusLineHandler, query string) StatusLineResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/am/statusline?"+query, nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), superRoot()))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var row StatusLineResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))
	return row
}

// aNodeWithPeers is a handler with an empty registry and no plugins probed, so
// the row runs its whole way through, with the peers answering as given.
func aNodeWithPeers(peers PeerRows) *StatusLineHandler {
	h := aRow(quietNode{})
	h.Peers = peers
	return h
}

// rowWithPeers draws the row for a SUPER caller with the peers answering as given.
func rowWithPeers(t *testing.T, answers []auth.PeerAnswer) (StatusLineResponse, []string) {
	t.Helper()
	var asked []string
	h := aNodeWithPeers(func(_ context.Context, _ auth.Admission, path string) []auth.PeerAnswer {
		asked = append(asked, path)
		return answers
	})
	return drawRow(t, h, "format=json"), asked
}

// "the statusline of the primary, it needs to be a combination of all the
// peer's statuslines combined"
func TestTheRowCarriesEveryPeersRowAfterItsOwn(t *testing.T) {
	news := StatusItem{ID: "AS-CI-WATCH-1", Name: "ci.watch", Note: "success", Symbol: SymbolWell}
	row, asked := rowWithPeers(t, []auth.PeerAnswer{
		{Name: "k", Status: http.StatusOK, Body: peerRowBody(t, news, StatusItem{Name: "attestations", Note: "272086", Symbol: SymbolWell})},
	})
	assert.Equal(t, []string{"/am/statusline?format=json&for=primary"}, asked, "the peer is asked for a primary's row")

	require.GreaterOrEqual(t, len(row.Items), 4)
	assert.Equal(t, auth.RootName, row.Items[0].Name, "the primary's caller stays leftmost")
	tail := row.Items[len(row.Items)-3:]
	assert.Equal(t, StatusItem{Name: "k", Symbol: SymbolWell}, tail[0], "the peer is named before its items")
	assert.Equal(t, news, tail[1], "the news keeps its id, so the laptop writes it once and asks by it")
	assert.Equal(t, "attestations", tail[2].Name)
}

// A primary asking draws the person itself, so the peer's answer carries no caller item.
func TestAPrimarysAskIsAnsweredWithoutTheCaller(t *testing.T) {
	h := aNodeWithPeers(NoPeers)
	own := drawRow(t, h, "format=json")
	require.Len(t, own.Items, 2, "the caller, and the node's own frame")
	assert.Equal(t, auth.RootName, own.Items[0].Name)

	forPrimary := drawRow(t, h, "format=json&for=primary")
	require.Len(t, forPrimary.Items, 1, "only the node's own frame")
	assert.NotEqual(t, auth.RootName, forPrimary.Items[0].Name, "a primary's ask drew the person again")
}

// A peer with an unwell item is marked unwell at its name, so the row says
// where to look before it says what.
func TestAPeerWithAnUnwellItemIsUnwell(t *testing.T) {
	row, _ := rowWithPeers(t, []auth.PeerAnswer{
		{Name: "k", Status: http.StatusOK, Body: peerRowBody(t, StatusItem{Name: "gaze", Symbol: SymbolUnwell})},
	})
	marker := row.Items[len(row.Items)-2]
	assert.Equal(t, StatusItem{Name: "k", Symbol: SymbolUnwell}, marker)
}

// A peer that did not answer, or refused, is one unwell item saying so.
func TestAPeerThatDidNotAnswerIsSaid(t *testing.T) {
	row, _ := rowWithPeers(t, []auth.PeerAnswer{
		{Name: "k", Err: errors.New("k did not answer: dial tcp: i/o timeout")},
		{Name: "elsewhere", Status: http.StatusUnauthorized, Body: []byte(`{"error":"no session"}`)},
	})
	tail := row.Items[len(row.Items)-2:]
	assert.Equal(t, StatusItem{Name: "k", Note: "k did not answer: dial tcp: i/o timeout", Symbol: SymbolUnwell}, tail[0])
	assert.Equal(t, StatusItem{Name: "elsewhere", Note: "answered 401", Symbol: SymbolUnwell}, tail[1])
}

// A node with no peers draws the row it always drew.
func TestANodeWithNoPeersDrawsItsOwnRow(t *testing.T) {
	row, _ := rowWithPeers(t, nil)
	assert.Len(t, row.Items, 2, "the caller, and the node's own frame")
	assert.Equal(t, auth.RootName, row.Items[0].Name)
}

// An item on a peer's row is asked of the peer by its name, and answered as
// the peer spelled it, naming the peer.
func TestAnItemOnAPeersRowIsAnsweredThroughThePrimary(t *testing.T) {
	var asked []string
	h := aNodeWithPeers(func(_ context.Context, _ auth.Admission, path string) []auth.PeerAnswer {
		asked = append(asked, path)
		k := auth.PeerAnswer{Name: "k", Status: http.StatusNotFound, Body: []byte(`{"error":"no such item"}`)}
		if path == "/am/statusline/AS-CI-WATCH-1" {
			k = auth.PeerAnswer{Name: "k", Status: http.StatusOK, Body: []byte(`{"id":"AS-CI-WATCH-1","name":"ci.watch","url":"https://github.com/teranos/ground/actions/runs/9"}`)}
		}
		return []auth.PeerAnswer{
			{Name: "elsewhere", Status: http.StatusNotFound, Body: []byte(`{"error":"no such item"}`)},
			k,
		}
	})
	ctx := auth.WithAdmission(context.Background(), superRoot())

	detail, refusal := h.Item(ctx, "AS-CI-WATCH-1")
	require.Nil(t, refusal)
	assert.Equal(t, []string{"/am/statusline/AS-CI-WATCH-1"}, asked)
	got, isMap := detail.(map[string]any)
	require.True(t, isMap)
	assert.Equal(t, "ci.watch", got["name"])
	assert.Equal(t, "k", got["peer"])

	_, refusal = h.Item(ctx, "nowhere")
	require.NotNil(t, refusal)
}
