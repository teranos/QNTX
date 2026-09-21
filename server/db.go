package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// What answers the db signum. What it is and what it takes is db_sigils.go.

// sentryIsGiven bounds the wait on a service that is not this one. A caller
// told nothing beats a handler holding the connection until a browser gives up.
const sentryIsGiven = 20 * time.Second

// topLines is how many values a split question comes back in. Sentry wants the
// number, and a chart with more lines than this says less than one with fewer.
const topLines = "5"

// A Reading is one bucket of one line: when, what the numbers in it came to,
// and how many there were. The count answers on its own — for a merge it is
// how many ran, where the value is how many files they took.
type Reading struct {
	At      int64   `json:"at"`
	Value   float64 `json:"value"`
	Samples int64   `json:"samples"`
}

// A Line is one series: what it is of, and its readings in time order. By is
// empty when the question split on nothing.
type Line struct {
	By       map[string]string `json:"by"`
	Readings []Reading         `json:"readings"`
}

// An Answer carries the question with it, so a screen draws what was asked
// rather than what it assumed it asked.
type Answer struct {
	Metric string   `json:"metric"`
	Since  string   `json:"since"`
	Every  string   `json:"every"`
	By     []string `json:"by"`
	Lines  []Line   `json:"lines"`
}

// dbSeries answers with one of the node's numbers over time.
//
// That the metric is one the node measures is the sigil's to refuse (OneOf),
// so what arrives here is a name this may ask about.
func (s *QNTXServer) dbSeries(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	token := config.GetString("sentry.read_token")
	org := config.GetString("sentry.org")
	region := config.GetString("sentry.region_url")
	if missing := unset(token, org, region); missing != "" {
		// Neither the caller's fault nor a failure: the node was never given
		// what this needs. Said with the key named, because a chart with no
		// line on it and a node that measures nothing look the same.
		return nil, &protocol.Refusal{Why: sigil.Failed,
			Says: missing + " unset, so the node cannot read its numbers back. " +
				"sentry.dsn writes and cannot read; this is a second credential and it wants org:read."}
	}

	metric := sent["metric"]
	since := orElse(sent["since"], "24h")
	every := orElse(sent["every"], "1h")
	by := sent["by"]

	question := url.Values{}
	question.Set("dataset", "tracemetrics")
	question.Set("yAxis", "sum(value)")
	question.Set("query", "metric.name:"+metric)
	question.Set("statsPeriod", since)
	question.Set("interval", every)
	if by != "" {
		question.Set("groupBy", by)
		// What makes Sentry answer with a line per value rather than one line
		// for all of them. It will not take the grouping without both.
		question.Set("topEvents", topLines)
		question.Set("sort", "-sum(value)")
	}

	asking := fmt.Sprintf("%s/api/0/organizations/%s/events-timeseries/?%s",
		strings.TrimSuffix(region, "/"), url.PathEscape(org), question.Encode())

	lines, err := linesFrom(ctx, asking, token)
	if err != nil {
		s.logger.Errorw("the node could not read its numbers back",
			"metric", metric, "since", since, "every", every, "region", region, "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed,
			Says: fmt.Sprintf("%s was asked about %s and the answer did not come back", region, metric)}
	}

	split := []string{}
	if by != "" {
		split = append(split, by)
	}
	return Answer{Metric: metric, Since: since, Every: every, By: split, Lines: lines}, nil
}

// linesFrom asks and reads the answer into the shape a chart takes.
func linesFrom(ctx context.Context, asking, token string) ([]Line, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asking, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "the question could not be built for %s", asking)
	}
	request.Header.Set("Authorization", "Bearer "+token)

	said, err := (&http.Client{Timeout: sentryIsGiven}).Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "the question went out and nothing came back")
	}
	defer func() { _ = said.Body.Close() }()

	body, err := io.ReadAll(said.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "%s answered and the answer did not all arrive", said.Status)
	}

	if said.StatusCode != http.StatusOK {
		// Their words, not a summary of them: a 403 here is a credential
		// without org:read, and "failed" would send a reader to the wrong
		// thing entirely. It reaches the log, which is where this is read.
		return nil, errors.Newf("answered %d: %s", said.StatusCode, strings.TrimSpace(string(body)))
	}

	var answered struct {
		TimeSeries []struct {
			GroupBy []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"groupBy"`
			Values []struct {
				Timestamp   int64    `json:"timestamp"`
				Value       *float64 `json:"value"`
				SampleCount float64  `json:"sampleCount"`
			} `json:"values"`
		} `json:"timeSeries"`
	}
	if err := json.Unmarshal(body, &answered); err != nil {
		return nil, errors.Wrapf(err, "the answer is %d bytes and none of it is the shape this reads", len(body))
	}

	lines := make([]Line, 0, len(answered.TimeSeries))
	for _, one := range answered.TimeSeries {
		of := make(map[string]string, len(one.GroupBy))
		for _, pair := range one.GroupBy {
			of[pair.Key] = pair.Value
		}

		readings := make([]Reading, 0, len(one.Values))
		for _, at := range one.Values {
			// A bucket they hold nothing for carries null and not nought, and
			// the two do not draw the same: one is a gap, one is a floor.
			if at.Value == nil {
				continue
			}
			readings = append(readings, Reading{
				At:      at.Timestamp,
				Value:   *at.Value,
				Samples: int64(at.SampleCount),
			})
		}
		lines = append(lines, Line{By: of, Readings: readings})
	}
	return lines, nil
}

func orElse(said, otherwise string) string {
	if said == "" {
		return otherwise
	}
	return said
}

// unset names the keys that are empty, so a reader is told which one to set
// rather than all three of them.
func unset(token, org, region string) string {
	var missing []string
	if token == "" {
		missing = append(missing, "sentry.read_token")
	}
	if org == "" {
		missing = append(missing, "sentry.org")
	}
	if region == "" {
		missing = append(missing, "sentry.region_url")
	}
	return strings.Join(missing, ", ")
}
