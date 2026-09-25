// Package sentryread asks Sentry what the node sent it (ADR-042).
//
// "can you do it please to the degree that is feasible given sentry access?"
//
// Two questions and no more: a table of rows (the events endpoint) and a
// series of buckets (the events-stats endpoint). What each asks for is the
// caller's; this package only carries the question and reads the answer.
package sentryread

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/errors"
)

// The datasets a question is asked of, as the Sentry API names them.
const (
	DatasetLogs    = "ourlogs"
	DatasetSpans   = "spans"
	DatasetMetrics = "tracemetrics"
)

// Client asks one Sentry organization about one project.
type Client struct {
	API          string // The API origin the organization lives in, e.g. https://de.sentry.io
	Organization string // The organization's slug.
	Project      string // The project's id.
	Token        string // A token that may read events.
	HTTP         *http.Client
}

// Window is the time a question covers.
type Window struct {
	Start time.Time
	End   time.Time
}

// TableQuery asks for rows: fields, often aggregates, grouped by the plain
// fields among them.
type TableQuery struct {
	Dataset string
	Fields  []string
	Query   string
	Sort    string
	Limit   int
	Window  Window
}

// SeriesQuery asks for one aggregate per interval across a window.
type SeriesQuery struct {
	Dataset  string
	YAxis    string
	Query    string
	Interval string // e.g. "1h"
	Window   Window
}

// Point is one bucket of a series.
type Point struct {
	At    time.Time
	Value float64
}

// Configured reports whether there is enough to ask anything, and names what
// is missing when there is not.
func (c Client) Configured() error {
	var missing []string
	if c.API == "" {
		missing = append(missing, "sentry.api")
	}
	if c.Organization == "" {
		missing = append(missing, "sentry.organization")
	}
	if c.Project == "" {
		missing = append(missing, "sentry.project")
	}
	if c.Token == "" {
		missing = append(missing, "sentry.read_token")
	}
	if len(missing) > 0 {
		return errors.Newf("Sentry is not asked: %s not set in am.toml", strings.Join(missing, ", "))
	}
	return nil
}

// Table asks for rows.
func (c Client) Table(ctx context.Context, q TableQuery) ([]map[string]any, error) {
	params := c.params(q.Dataset, q.Query, q.Window)
	for _, f := range q.Fields {
		params.Add("field", f)
	}
	if q.Sort != "" {
		params.Set("sort", q.Sort)
	}
	if q.Limit > 0 {
		params.Set("per_page", strconv.Itoa(q.Limit))
	}
	var answer struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.get(ctx, "events", params, &answer); err != nil {
		return nil, err
	}
	return answer.Data, nil
}

// Series asks for one aggregate per interval. A bucket Sentry holds nothing for
// is zero, the way it answers one.
func (c Client) Series(ctx context.Context, q SeriesQuery) ([]Point, error) {
	params := c.params(q.Dataset, q.Query, q.Window)
	params.Set("yAxis", q.YAxis)
	params.Set("interval", q.Interval)
	var answer struct {
		Data [][2]json.RawMessage `json:"data"`
	}
	if err := c.get(ctx, "events-stats", params, &answer); err != nil {
		return nil, err
	}
	points := make([]Point, 0, len(answer.Data))
	for _, bucket := range answer.Data {
		var at int64
		if err := json.Unmarshal(bucket[0], &at); err != nil {
			return nil, errors.Wrapf(err, "a %s bucket's time is not a number: %s", q.YAxis, bucket[0])
		}
		var values []struct {
			Count *float64 `json:"count"`
		}
		if err := json.Unmarshal(bucket[1], &values); err != nil {
			return nil, errors.Wrapf(err, "a %s bucket's value does not read: %s", q.YAxis, bucket[1])
		}
		p := Point{At: time.Unix(at, 0).UTC()}
		if len(values) > 0 && values[0].Count != nil {
			p.Value = *values[0].Count
		}
		points = append(points, p)
	}
	return points, nil
}

func (c Client) params(dataset, query string, w Window) url.Values {
	params := url.Values{}
	params.Set("dataset", dataset)
	params.Set("project", c.Project)
	params.Set("start", w.Start.UTC().Format(time.RFC3339))
	params.Set("end", w.End.UTC().Format(time.RFC3339))
	if query != "" {
		params.Set("query", query)
	}
	return params
}

// get asks one endpoint of the organization and decodes the answer. A refusal
// carries what Sentry said, because that is the only place the reason is.
func (c Client) get(ctx context.Context, endpoint string, params url.Values, into any) error {
	if err := c.Configured(); err != nil {
		return err
	}
	u := strings.TrimSuffix(c.API, "/") + "/api/0/organizations/" + url.PathEscape(c.Organization) + "/" + endpoint + "/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return errors.Wrapf(err, "failed to build the Sentry request for %s", endpoint)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "Sentry %s did not answer", endpoint)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return errors.Wrapf(readErr, "failed to read Sentry's %s answer", endpoint)
	}
	if closeErr != nil {
		return errors.Wrapf(closeErr, "failed to close Sentry's %s answer", endpoint)
	}
	if resp.StatusCode != http.StatusOK {
		said := strings.TrimSpace(string(body))
		if len(said) > 500 {
			said = said[:500]
		}
		return errors.Newf("Sentry %s answered %s: %s", endpoint, resp.Status, said)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return errors.Wrapf(err, "Sentry's %s answer does not read", endpoint)
	}
	return nil
}
