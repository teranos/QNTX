package server

import (
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Datapunts is the first signum (ADR-039): what the node does with a datapunt, a
// sigil each. Its paths, its tools and its operations in the served document
// are all read from here (signa.go); the functions that answer are in datapunt.go.

// What every read of one datapunt takes.
func datapuntMarketParam() *protocol.Param {
	return &protocol.Param{Name: "market", Required: true,
		Says: "The market the datapunt is in. Never system or default."}
}

func datapuntSlugParam() *protocol.Param {
	return &protocol.Param{Name: "slug", Required: true, Says: "The datapunt, by its slug."}
}

func datapuntSinceParam() *protocol.Param {
	return &protocol.Param{Name: "since",
		Says: "Arrivals at or after this, in AX's words: yesterday, last monday, or an ISO stamp. Naming none is the datapunt's whole life."}
}

func datapuntUntilParam() *protocol.Param {
	return &protocol.Param{Name: "until", Says: "Arrivals at or before this, said the way since is."}
}

func (s *QNTXServer) datapuntsSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "datapunts",
			Description: "Datapunts: a market's public pixel, created and taken down, and what arrived at each, read as metrics, activity and visits.",
			Tags:        []string{"analytics", "pixel", "visits"},
			Oasf:        []string{"data_engineering_analytics/data_analytics/product_analytics"},
			Follows:     []*protocol.Follows{datapuntsFollowUmami()},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "List every datapunt across all markets, with what arrived at each.",
					Takes:  []*protocol.Param{datapuntSinceParam(), datapuntUntilParam()},
					Answer: "protocol.Datapunts",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/datapunts"},
				},
				{
					Name: "create",
					Does: "Create a datapunt in a market, as one pixel its CUE declares. Its pixel starts recording at /d/{market}/{slug}; what the pixel does not declare is refused and written down.",
					Takes: []*protocol.Param{datapuntMarketParam(), {Name: "slug", Required: true, Says: "The new datapunt's slug. No slash."},
						{Name: "pixel", Required: true, Says: "The pixel it is, as the datapunt plugin's CUE declares it: what its snippet may say. Read the datapunt pixel sigil for every pixel."}},
					Answer: "protocol.DatapuntCreated",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/datapunts"},
				},
				{
					Name:   "take-down",
					Does:   "Take a datapunt down, so its pixel stops recording. What it recorded stays.",
					Takes:  []*protocol.Param{datapuntMarketParam(), datapuntSlugParam()},
					Answer: "protocol.DatapuntTakenDown",
					Http:   &protocol.Endpoint{Method: http.MethodDelete, Path: "/api/datapunts"},
				},
				{
					Name: "metrics",
					Does: "One datapunt's arrivals grouped by one dimension, most first.",
					Takes: []*protocol.Param{datapuntMarketParam(), datapuntSlugParam(),
						{Name: "type", Required: true, OneOf: datapuntDimensions(),
							Says: "What to group the arrivals by."},
						datapuntSinceParam(), datapuntUntilParam(),
						{Name: "limit", Kind: sigil.Count, Says: "How many rows at most. Naming none is one hundred."},
					},
					Answer: "protocol.DatapuntMetrics",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/datapunts/metrics"},
				},
				{
					Name: "activity",
					Does: "What happened at one datapunt, newest first, as rows. Naming a visit is one sitting, naming a visitor is that person, naming neither is the datapunt.",
					Takes: []*protocol.Param{datapuntMarketParam(), datapuntSlugParam(),
						{Name: "visit", Says: "One sitting, by its id."},
						{Name: "visitor", Says: "One person, by their id."},
						datapuntSinceParam(), datapuntUntilParam(),
					},
					Answer: "protocol.DatapuntActivity",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/datapunts/activity"},
				},
				{
					Name:   "visits",
					Does:   "One datapunt's sittings, derived from its arrivals every time and stored nowhere.",
					Takes:  []*protocol.Param{datapuntMarketParam(), datapuntSlugParam(), datapuntSinceParam(), datapuntUntilParam()},
					Answer: "protocol.DatapuntVisits",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/datapunts/visits"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list":      s.datapuntsList,
			"create":    s.datapuntsCreate,
			"take-down": s.datapuntsTakeDown,
			"metrics":   s.datapuntsMetrics,
			"activity":  s.datapuntsActivity,
			"visits":    s.datapuntsVisits,
		},
	}
}

// datapuntsFollowUmami is which Umami column each field of a datapunt is. "Datapunts will
// be Umami, one reference and not a blend." An arrival is the record a datapunt
// keeps; a visit is the sitting its visits sigil gives, carrying the same ids.
//
// Only what a datapunt fills is said here. The Arrival fields nothing fills yet —
// query, browser, operating system, device, screen, language, country, region,
// city — stay in the proto and are not followed until something fills them.
func datapuntsFollowUmami() *protocol.Follows {
	pairs := []struct{ field, column string }{
		{"protocol.Arrival.market", "WebsiteEvent.websiteId"},
		{"protocol.Arrival.market", "Session.websiteId"},
		{"protocol.Arrival.slug", "WebsiteEvent.websiteId"},
		{"protocol.Arrival.slug", "Session.websiteId"},
		{"protocol.Arrival.at", "WebsiteEvent.createdAt"},
		{"protocol.Arrival.visitor", "WebsiteEvent.sessionId"},
		{"protocol.Arrival.visitor", "Session.id"},
		{"protocol.Arrival.visit", "WebsiteEvent.visitId"},
		{"protocol.Arrival.path", "WebsiteEvent.urlPath"},
		{"protocol.Arrival.event", "WebsiteEvent.eventName"},
		{"protocol.Arrival.referrer_domain", "WebsiteEvent.referrerDomain"},
		{"protocol.Arrival.referrer_path", "WebsiteEvent.referrerPath"},
		{"protocol.Arrival.utm_source", "WebsiteEvent.utmSource"},
		{"protocol.Arrival.utm_medium", "WebsiteEvent.utmMedium"},
		{"protocol.Arrival.utm_campaign", "WebsiteEvent.utmCampaign"},
		{"protocol.Arrival.utm_content", "WebsiteEvent.utmContent"},
		{"protocol.Arrival.utm_term", "WebsiteEvent.utmTerm"},
		{"protocol.Arrival.params", "EventData.dataKey"},
		{"protocol.Arrival.params", "EventData.stringValue"},
		{"protocol.Visit.market", "WebsiteEvent.websiteId"},
		{"protocol.Visit.slug", "WebsiteEvent.websiteId"},
		{"protocol.Visit.visitor", "WebsiteEvent.sessionId"},
		{"protocol.Visit.visit", "WebsiteEvent.visitId"},
		// What Umami's tracker sends, beside what Umami keeps: one reference,
		// read from its record and its tracker. A datapunt splits the URL and
		// the referrer the tracker sends whole, so two fields follow each.
		{"protocol.Arrival.market", "TrackedProperties.website"},
		{"protocol.Arrival.slug", "TrackedProperties.website"},
		{"protocol.Arrival.path", "TrackedProperties.url"},
		{"protocol.Arrival.referrer_domain", "TrackedProperties.referrer"},
		{"protocol.Arrival.referrer_path", "TrackedProperties.referrer"},
		{"protocol.Arrival.event", "EventProperties.name"},
		{"protocol.Arrival.params", "EventProperties.data"},
	}
	follows := &protocol.Follows{Reference: "umami"}
	for _, p := range pairs {
		follows.Columns = append(follows.Columns, &protocol.Corresponds{Field: p.field, Column: p.column})
	}
	return follows
}
