package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/teranos/QNTX/server/auth"
)

// "the statusline of the primary, it needs to be a combination of all the
// peer's statuslines combined"
// ug polls one node. A peer's built-ins leave what they concluded on the
// peer's row, so the primary's row carries every peer's after its own.

// peerRows asks every peer one path as the caller: auth.(*Handler).AskPeers.
type peerRows func(ctx context.Context, a auth.Admission, path string) []auth.PeerAnswer

// noPeers is a node with no peers to ask: the row it draws is its own.
func noPeers(context.Context, auth.Admission, string) []auth.PeerAnswer { return nil }

// forPrimary is the query a primary asks a peer's row with. The peer leaves its
// caller item off: the primary drew the person already, leftmost.
const forPrimary = "primary"

// peerItems is every peer's row after the primary's: one item naming the
// peer, then the peer's items as the peer spelled them.
func (h *StatusLineHandler) peerItems(ctx context.Context, a auth.Admission) []StatusItem {
	var items []StatusItem
	for _, answer := range h.peers(ctx, a, "/am/statusline?format="+FormatJSON+"&for="+forPrimary) {
		items = append(items, peerRow(answer)...)
	}
	return items
}

// peerRow is one peer's answer as items. A peer that did not answer, or
// answered with a refusal, is one unwell item saying so.
func peerRow(answer auth.PeerAnswer) []StatusItem {
	if answer.Err != nil {
		return []StatusItem{{Name: answer.Name, Note: answer.Err.Error(), Symbol: SymbolUnwell}}
	}
	if answer.Status != http.StatusOK {
		return []StatusItem{{Name: answer.Name, Note: "answered " + strconv.Itoa(answer.Status), Symbol: SymbolUnwell}}
	}
	var row StatusLineResponse
	if err := json.Unmarshal(answer.Body, &row); err != nil {
		return []StatusItem{{Name: answer.Name, Note: "answered no row", Symbol: SymbolUnwell}}
	}
	marker := StatusItem{Name: answer.Name, Symbol: SymbolWell}
	for _, it := range row.Items {
		if it.Symbol != SymbolWell {
			marker.Symbol = SymbolUnwell
			break
		}
	}
	return append([]StatusItem{marker}, row.Items...)
}

// peerItemDetail asks each peer for one item by name; the first that holds
// it answers, as the peer spelled it, naming the peer.
func (h *StatusLineHandler) peerItemDetail(ctx context.Context, a auth.Admission, name string) (map[string]any, bool) {
	for _, answer := range h.peers(ctx, a, "/am/statusline/"+name) {
		if answer.Err != nil || answer.Status != http.StatusOK {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(answer.Body, &detail); err != nil {
			continue
		}
		detail["peer"] = answer.Name
		return detail, true
	}
	return nil, false
}
