package server

import (
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Staands is the first signum (ADR-039): what the node does with a stand, a
// sigil each. Its paths, its tools and its operations in the served document
// are all read from here (signa.go); the functions that answer are in staand.go.

// What every read of one stand takes.
func staandMarketParam() *protocol.Param {
	return &protocol.Param{Name: "market", Required: true,
		Says: "The market the stand is in. Never system or default."}
}

func staandSlugParam() *protocol.Param {
	return &protocol.Param{Name: "slug", Required: true, Says: "The stand, by its slug."}
}

func staandSinceParam() *protocol.Param {
	return &protocol.Param{Name: "since",
		Says: "Arrivals at or after this, in AX's words: yesterday, last monday, or an ISO stamp. Naming none is the stand's whole life."}
}

func staandUntilParam() *protocol.Param {
	return &protocol.Param{Name: "until", Says: "Arrivals at or before this, said the way since is."}
}

// staandEchoed is what every read of one stand gives back with its answer.
func staandEchoed(fields ...*protocol.Field) []*protocol.Field {
	return append(fields,
		&protocol.Field{Name: "market", Says: "The market that was asked about."},
		&protocol.Field{Name: "slug", Says: "The stand that was asked about."},
	)
}

func (s *QNTXServer) staandsSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "staands",
			Sigils: []*protocol.Sigil{
				{
					Name:  "list",
					Does:  "List every stand across all markets, with what arrived at each.",
					Takes: []*protocol.Param{staandSinceParam(), staandUntilParam()},
					Gives: []*protocol.Field{{Name: "staands", Says: "One row per stand that has not been taken down."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands"},
				},
				{
					Name:  "create",
					Does:  "Create a stand in a market. Its pixel starts recording at /s/{market}/{slug}.",
					Takes: []*protocol.Param{staandMarketParam(), {Name: "slug", Required: true, Says: "The new stand's slug. No slash."}},
					Gives: []*protocol.Field{
						{Name: "slug", Says: "The stand that was created."},
						{Name: "url", Says: "The path its pixel is fired at."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/staands"},
				},
				{
					Name:  "take-down",
					Does:  "Take a stand down, so its pixel stops recording. What it recorded stays.",
					Takes: []*protocol.Param{staandMarketParam(), staandSlugParam()},
					Gives: []*protocol.Field{
						{Name: "slug", Says: "The stand that was taken down."},
						{Name: "status", Says: "removed."},
					},
					Http: &protocol.Endpoint{Method: http.MethodDelete, Path: "/api/staands"},
				},
				{
					Name: "metrics",
					Does: "One stand's arrivals grouped by one dimension, most first.",
					Takes: []*protocol.Param{staandMarketParam(), staandSlugParam(),
						{Name: "type", Required: true, OneOf: staandDimensions(),
							Says: "What to group the arrivals by."},
						staandSinceParam(), staandUntilParam(),
						{Name: "limit", Kind: sigil.Count, Says: "How many rows at most. Naming none is one hundred."},
					},
					Gives: staandEchoed(
						&protocol.Field{Name: "type", Says: "What the arrivals were grouped by."},
						&protocol.Field{Name: "counts", Says: "One row per value, most first: its name and its count."},
					),
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands/metrics"},
				},
				{
					Name: "activity",
					Does: "What happened at one stand, newest first, as rows. Naming a visit is one sitting, naming a visitor is that person, naming neither is the stand.",
					Takes: []*protocol.Param{staandMarketParam(), staandSlugParam(),
						{Name: "visit", Says: "One sitting, by its id."},
						{Name: "visitor", Says: "One person, by their id."},
						staandSinceParam(), staandUntilParam(),
					},
					Gives: staandEchoed(
						&protocol.Field{Name: "activity", Says: "One row per arrival, five hundred at most: when, the visit, the visitor, the page, where they came from, and the event."},
					),
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands/activity"},
				},
				{
					Name:  "visits",
					Does:  "One stand's sittings, derived from its arrivals every time and stored nowhere.",
					Takes: []*protocol.Param{staandMarketParam(), staandSlugParam(), staandSinceParam(), staandUntilParam()},
					Gives: staandEchoed(
						&protocol.Field{Name: "visits", Says: "One row per sitting: who, when it started and ended, how long, the first and last page, how many views and events, and whether it was a bounce."},
					),
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands/visits"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list":      s.staandsList,
			"create":    s.staandsCreate,
			"take-down": s.staandsTakeDown,
			"metrics":   s.staandsMetrics,
			"activity":  s.staandsActivity,
			"visits":    s.staandsVisits,
		},
	}
}
