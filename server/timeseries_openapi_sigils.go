package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Timeseries is what the node spent on models, by day; openapi is the
// document of what it serves (ADR-039).

func (s *QNTXServer) timeseriesSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "timeseries",
			Sigils: []*protocol.Sigil{
				{
					Name: "usage",
					Does: "Model requests and their cost, one point per day, for charting.",
					Takes: []*protocol.Param{{Name: "days", Kind: sigil.Count,
						Says: "How many days back: 7 when not sent, at least 1 and at most 365."}},
					Answer: "protocol.TimeseriesUsage",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/timeseries/usage"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"usage": s.timeseriesUsage},
	}
}

func (s *QNTXServer) timeseriesUsage(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	days := 7
	if said := sent["days"]; said != "" {
		read, err := strconv.Atoi(said)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "days", Says: "days is not a whole number: " + said}
		}
		days = min(max(read, 1), 365)
	}
	points, err := s.usageTracker.GetTimeSeriesData(days)
	if err != nil {
		s.logger.Errorw("failed to fetch time-series data", "days", days, "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	answer := &protocol.TimeseriesUsage{Points: make([]*protocol.UsagePoint, 0, len(points))}
	for _, p := range points {
		answer.Points = append(answer.Points, &protocol.UsagePoint{Date: p.Date, Requests: uint32(p.Requests), Cost: p.Cost})
	}
	return answer, nil
}

func (s *QNTXServer) openapiSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "openapi",
			Sigils: []*protocol.Sigil{
				{
					Name: "document",
					Does: "The OpenAPI document for this build: every path the reach table names, who reaches it, and what each sigil does there.",
					Gives: []*protocol.Field{
						{Name: "openapi", Says: "The OpenAPI version the document is written in."},
						{Name: "info", Says: "This build's title and version tag."},
						{Name: "paths", Says: "Every path, who reaches it, and each sigil's operation on it."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/openapi.json"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"document": s.openapiDocument},
	}
}

func (s *QNTXServer) openapiDocument(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	document, err := s.openapiServed()
	if err != nil {
		s.logger.Errorw("the OpenAPI document is not servable", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return json.RawMessage(document), nil
}
