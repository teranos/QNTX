package server

import (
	"net/http"

	"github.com/teranos/QNTX/server/sigil"
)

// Staands is the first signum (ADR-039): what the node does with a stand, a
// sigil each. Its paths, its tools and its operations in the served document
// are all read from here (signa.go), and the functions that answer are in
// staand.go.

// What every read of one stand takes.
var (
	staandMarketParam = sigil.Param{Name: "market", Required: true,
		Says: "The market the stand is in. Never system or default."}
	staandSlugParam = sigil.Param{Name: "slug", Required: true,
		Says: "The stand, by its slug."}
	staandSinceParam = sigil.Param{Name: "since",
		Says: "Arrivals at or after this, in AX's words: yesterday, last monday, or an ISO stamp. Naming none is the stand's whole life."}
	staandUntilParam = sigil.Param{Name: "until",
		Says: "Arrivals at or before this, said the way since is."}
)

func (s *QNTXServer) staandsSignum() sigil.Signum {
	market, slug := staandMarketParam, staandSlugParam
	since, until := staandSinceParam, staandUntilParam

	echoed := []sigil.Field{
		{Name: "market", Says: "The market that was asked about."},
		{Name: "slug", Says: "The stand that was asked about."},
	}

	return sigil.Signum{
		Name: "staands",
		Sigils: []sigil.Sigil{
			{
				Name:   "list",
				Does:   "List every stand across all markets, with what arrived at each.",
				Takes:  []sigil.Param{since, until},
				Gives:  []sigil.Field{{Name: "staands", Says: "One row per stand that has not been taken down."}},
				Answer: s.staandsList,
				HTTP:   sigil.Endpoint{Method: http.MethodGet, Path: "/api/staands"},
			},
			{
				Name:  "create",
				Does:  "Create a stand in a market. Its pixel starts recording at /s/{market}/{slug}.",
				Takes: []sigil.Param{market, {Name: "slug", Required: true, Says: "The new stand's slug. No slash."}},
				Gives: []sigil.Field{
					{Name: "slug", Says: "The stand that was created."},
					{Name: "url", Says: "The path its pixel is fired at."},
				},
				Answer: s.staandsCreate,
				HTTP:   sigil.Endpoint{Method: http.MethodPost, Path: "/api/staands"},
			},
			{
				Name:  "take-down",
				Does:  "Take a stand down, so its pixel stops recording. What it recorded stays.",
				Takes: []sigil.Param{market, slug},
				Gives: []sigil.Field{
					{Name: "slug", Says: "The stand that was taken down."},
					{Name: "status", Says: "removed."},
				},
				Answer: s.staandsTakeDown,
				HTTP:   sigil.Endpoint{Method: http.MethodDelete, Path: "/api/staands"},
			},
			{
				Name: "metrics",
				Does: "One stand's arrivals grouped by one dimension, most first.",
				Takes: []sigil.Param{market, slug,
					{Name: "type", Required: true, OneOf: staandDimensions(),
						Says: "What to group the arrivals by."},
					since, until,
					{Name: "limit", Kind: sigil.Count, Says: "How many rows at most. Naming none is one hundred."},
				},
				Gives: append([]sigil.Field{
					{Name: "type", Says: "What the arrivals were grouped by."},
					{Name: "counts", Says: "One row per value, most first: its name and its count."},
				}, echoed...),
				Answer: s.staandsMetrics,
				HTTP:   sigil.Endpoint{Method: http.MethodGet, Path: "/api/staands/metrics"},
			},
			{
				Name: "activity",
				Does: "What happened at one stand, newest first, as rows. Naming a visit is one sitting, naming a visitor is that person, naming neither is the stand.",
				Takes: []sigil.Param{market, slug,
					{Name: "visit", Says: "One sitting, by its id."},
					{Name: "visitor", Says: "One person, by their id."},
					since, until,
				},
				Gives: append([]sigil.Field{
					{Name: "activity", Says: "One row per arrival, five hundred at most: when, the visit, the visitor, the page, where they came from, and the event."},
				}, echoed...),
				Answer: s.staandsActivity,
				HTTP:   sigil.Endpoint{Method: http.MethodGet, Path: "/api/staands/activity"},
			},
			{
				Name:  "visits",
				Does:  "One stand's sittings, derived from its arrivals every time and stored nowhere.",
				Takes: []sigil.Param{market, slug, since, until},
				Gives: append([]sigil.Field{
					{Name: "visits", Says: "One row per sitting: who, when it started and ended, how long, the first and last page, how many views and events, and whether it was a bounce."},
				}, echoed...),
				Answer: s.staandsVisits,
				HTTP:   sigil.Endpoint{Method: http.MethodGet, Path: "/api/staands/visits"},
			},
		},
	}
}
