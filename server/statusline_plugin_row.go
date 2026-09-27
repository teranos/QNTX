package server

import (
	"strconv"
	"time"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

// A plugin's own row: what the node's row becomes when that plugin's name is
// clicked, and what a surface asks for when it draws one plugin. The node
// proxies every call a plugin is sent, so the node is what can say how often
// each route was called and how it answered; the plugin is asked nothing.

// trafficked is a plugin the node carries HTTP to.
type trafficked interface {
	Traffic() *grpcplugin.Traffic
}

// pluginRow is one plugin's row, or false when the node holds no such plugin.
func (h *StatusLineHandler) pluginRow(name string) ([]StatusItem, bool) {
	if h == nil || h.registry == nil {
		return nil, false
	}
	p, ok := h.registry.Get(name)
	if !ok {
		return nil, false
	}

	healthy := false
	if h.health != nil {
		results, _, _ := h.health()
		healthy = well(results[name].Healthy, string(h.registry.GetAllStates()[name]))
	}
	meta := p.Metadata()

	// The name leads, so clicking it again is where the node's row comes back.
	items := []StatusItem{{Name: meta.Name, Note: meta.Version, Symbol: symbolFor(healthy)}}

	var mine []handlerFailureRun
	for _, run := range h.recentHandlerFailures() {
		if pluginOf(run.Handler) == name {
			mine = append(mine, run)
		}
	}
	items = append(items, handlerFailureItemsFor(mine)...)

	carried, ok := p.(trafficked)
	if !ok || carried.Traffic() == nil {
		return items, true
	}
	t := carried.Traffic()
	for _, r := range t.Routes() {
		items = append(items, routeItem(r))
	}
	items = append(items, StatusItem{Name: "counted", Note: shortDuration(time.Since(t.Since())), Symbol: SymbolWell})
	return items, true
}

// One route: how often it was called, and what it answered badly. A 5xx is the
// plugin saying it broke, and is the only thing that makes a route unwell.
func routeItem(r grpcplugin.RouteTraffic) StatusItem {
	note := strconv.FormatInt(r.Calls, 10)
	if r.Calls > 0 {
		note += " " + (r.Took / time.Duration(r.Calls)).Round(time.Millisecond).String()
	}
	if r.Refused > 0 {
		note += ", " + strconv.FormatInt(r.Refused, 10) + " 4xx"
	}
	if r.Broke > 0 {
		note += ", " + strconv.FormatInt(r.Broke, 10) + " 5xx"
		return StatusItem{Name: r.Route, Note: note, Symbol: SymbolUnwell}
	}
	return StatusItem{Name: r.Route, Note: note, Symbol: SymbolWell}
}
