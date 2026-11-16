package server

// "ADR-025 is DEAD ok?"
//
// "And the node gives Agents access to the MCP of Sentry"
//
// The weekly report asks Sentry through the Sentry MCP server the node consumes, on the grant the node keeps for it
// (mcp connect), and holds no Sentry token of its own. Each question is one call of that server's tools, written
// down as every call of a consumed server is.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sentryread"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// reportSentryServer is what the node calls the Sentry MCP server it consumes.
const reportSentryServer = "sentry"

// sentryTool calls one tool of the Sentry MCP server and answers what it said, as JSON.
type sentryTool func(ctx context.Context, tool string, arguments map[string]any) (json.RawMessage, error)

// mcpSentry answers the report's questions through the Sentry MCP server.
type mcpSentry struct {
	call         sentryTool
	region       string
	organization string
	project      string
}

// sentryReader is the Sentry MCP server the node consumes, as am.toml places the node in it, or why the report
// cannot ask it.
func (s *QNTXServer) sentryReader(ctx context.Context) (sentryAsker, error) {
	if _, _, err := s.grantOf(reportSentryServer); err != nil {
		return nil, err
	}
	return sentryOver(s.sentryConfig, s.sentryToolCall)
}

// sentryOver is the report's Sentry over call, where am.toml places the node: its region, organization and project.
func sentryOver(cfg appcfg.SentryConfig, call sentryTool) (sentryAsker, error) {
	var missing []string
	if cfg.API == "" {
		missing = append(missing, "sentry.api")
	}
	if cfg.Organization == "" {
		missing = append(missing, "sentry.organization")
	}
	if cfg.Project == "" {
		missing = append(missing, "sentry.project")
	}
	if len(missing) > 0 {
		return nil, errors.Newf("Sentry is not asked: %s not set in am.toml", strings.Join(missing, ", "))
	}
	return mcpSentry{call: call, region: cfg.API, organization: cfg.Organization, project: cfg.Project}, nil
}

// sentryToolCall calls one tool of the Sentry MCP server as the node itself, at ROOT.
func (s *QNTXServer) sentryToolCall(ctx context.Context, tool string, arguments map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(arguments)
	if err != nil {
		return nil, errors.Wrapf(err, "the arguments of %s did not marshal", tool)
	}
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = s.nodeActor()
	return sentryAnswer(tool, s.consumedCall(ctx, admitted, reportSentryServer, tool, body))
}

// sentryAnswer is what a tool of the Sentry MCP server said, or the tool's own words when it failed.
func sentryAnswer(tool string, result *mcp.CallToolResult) (json.RawMessage, error) {
	var said strings.Builder
	for _, c := range result.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			said.WriteString(text.Text)
		}
	}
	if result.IsError {
		return nil, errors.Newf("Sentry's %s answered: %s", tool, strings.TrimSpace(said.String()))
	}
	if result.StructuredContent != nil {
		if structured, err := json.Marshal(result.StructuredContent); err == nil {
			return structured, nil
		}
	}
	return json.RawMessage(said.String()), nil
}

// toolOf is the Sentry MCP tool that searches a dataset.
func toolOf(dataset string) (string, error) {
	switch dataset {
	case sentryread.DatasetMetrics:
		return "search_metrics", nil
	case sentryread.DatasetLogs:
		return "search_logs", nil
	case sentryread.DatasetSpans:
		return "search_traces", nil
	}
	return "", errors.Newf("no Sentry MCP tool searches the dataset %s", dataset)
}

// periodOf is a window as the Sentry MCP's tools take one: hours back from now, a part of an hour counted whole.
func periodOf(w sentryread.Window) string {
	return fmt.Sprintf("%dh", int(math.Ceil(w.End.Sub(w.Start).Hours())))
}

// Table asks for rows: the fields named exactly, so no field is left to the tool's own translation.
func (m mcpSentry) Table(ctx context.Context, q sentryread.TableQuery) ([]map[string]any, error) {
	tool, err := toolOf(q.Dataset)
	if err != nil {
		return nil, err
	}
	arguments := map[string]any{
		"organizationSlug": m.organization,
		"regionUrl":        m.region,
		"projectSlug":      m.project,
		"query":            q.Query,
		"fields":           q.Fields,
		"period":           periodOf(q.Window),
	}
	if q.Sort != "" {
		arguments["sort"] = q.Sort
	}
	if q.Limit > 0 {
		arguments["limit"] = q.Limit
	}
	said, err := m.call(ctx, tool, arguments)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []struct {
			Values map[string]any `json:"values"`
		} `json:"results"`
	}
	if err := json.Unmarshal(said, &answer); err != nil {
		return nil, errors.Wrapf(err, "Sentry's %s answer does not read: %.300s", tool, said)
	}
	rows := make([]map[string]any, 0, len(answer.Results))
	for _, r := range answer.Results {
		rows = append(rows, r.Values)
	}
	return rows, nil
}

// countOf is the count beside an average, of the same value: avg(value,qntx.host.cpu,gauge,-) is counted by
// count(value,qntx.host.cpu,gauge,-).
func countOf(yAxis string) (string, bool) {
	if !strings.HasPrefix(yAxis, "avg(") {
		return "", false
	}
	return "count(" + strings.TrimPrefix(yAxis, "avg("), true
}

// Series asks for one count or average per bucket. The Sentry MCP's tools take a period back from now and no
// bucket, so each bucket is the difference of two periods: the last day is what the last day holds, the day before
// it what the last two days hold beyond it. An average is told apart by its count: a period's sum is its average
// times its count. A bucket that holds nothing is zero.
func (m mcpSentry) Series(ctx context.Context, q sentryread.SeriesQuery) ([]sentryread.Point, error) {
	if q.Bucket <= 0 {
		return nil, errors.Newf("a series of %s needs its bucket", q.YAxis)
	}
	count, averaged := countOf(q.YAxis)
	if !averaged && !strings.HasPrefix(q.YAxis, "count(") {
		return nil, errors.Newf("a series is a count or an average, and %s is neither", q.YAxis)
	}
	fields := []string{q.YAxis}
	if averaged {
		fields = append(fields, count)
	} else {
		count = q.YAxis
	}
	buckets := int(q.Window.End.Sub(q.Window.Start) / q.Bucket)
	// What each period back from now holds: its count, and its sum when it is an average. Each period is measured
	// back from when Sentry answers it, so they are all asked at once: the further apart they are asked, the more
	// the days they are told apart into slide.
	counts, sums := make([]float64, buckets+1), make([]float64, buckets+1)
	errs := make([]error, buckets+1)
	var asked sync.WaitGroup
	for i := 1; i <= buckets; i++ {
		asked.Add(1)
		go func() {
			defer asked.Done()
			rows, err := m.Table(ctx, sentryread.TableQuery{
				Dataset: q.Dataset,
				Fields:  fields,
				Query:   q.Query,
				Window:  sentryread.Window{Start: q.Window.End.Add(-time.Duration(i) * q.Bucket), End: q.Window.End},
			})
			if err != nil {
				errs[i] = err
				return
			}
			if len(rows) == 0 {
				counts[i] = math.NaN()
				return
			}
			counts[i] = number(rows[0][count])
			if averaged {
				sums[i] = number(rows[0][q.YAxis]) * counts[i]
			}
		}()
	}
	asked.Wait()
	for i := 1; i <= buckets; i++ {
		if errs[i] != nil {
			return nil, errs[i]
		}
		// A period that held nothing holds what the shorter one did.
		if math.IsNaN(counts[i]) {
			counts[i], sums[i] = counts[i-1], sums[i-1]
		}
	}
	points := make([]sentryread.Point, 0, buckets)
	for i := buckets; i >= 1; i-- {
		p := sentryread.Point{At: q.Window.End.Add(-time.Duration(i) * q.Bucket)}
		in := counts[i] - counts[i-1]
		switch {
		case !averaged:
			p.Value = math.Max(in, 0)
		case in > 0:
			p.Value = (sums[i] - sums[i-1]) / in
		}
		points = append(points, p)
	}
	return points, nil
}

// Outcomes asks what Sentry took of the events of one category in the window, and what it dropped and why.
func (m mcpSentry) Outcomes(ctx context.Context, category string, w sentryread.Window) ([]sentryread.Outcome, error) {
	if category != sentryread.CategoryError {
		return nil, errors.Newf("the report asks Sentry what became of %s events, and no Sentry MCP tool says", category)
	}
	said, err := m.call(ctx, "find_dropped_events", map[string]any{
		"organizationSlug": m.organization,
		"regionUrl":        m.region,
		"projectSlug":      m.project,
		"dataset":          "errors",
		"start":            w.Start.UTC().Format(time.RFC3339),
		"end":              w.End.UTC().Format(time.RFC3339),
		"interval":         "1h",
	})
	if err != nil {
		return nil, err
	}
	type bucket struct {
		Outcome string  `json:"outcome"`
		Reason  string  `json:"reason"`
		Count   float64 `json:"count"`
	}
	var answer struct {
		Dropped  []bucket `json:"droppedEvents"`
		Accepted []bucket `json:"acceptedEvents"`
	}
	if err := json.Unmarshal(said, &answer); err != nil {
		return nil, errors.Wrapf(err, "Sentry's find_dropped_events answer does not read: %.300s", said)
	}
	type key struct{ outcome, reason string }
	var order []key
	totals := map[key]float64{}
	for _, b := range append(answer.Accepted, answer.Dropped...) {
		k := key{b.Outcome, b.Reason}
		if k.outcome == outcomeAccepted {
			k.reason = ""
		}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		totals[k] += b.Count
	}
	outcomes := make([]sentryread.Outcome, 0, len(order))
	for _, k := range order {
		outcomes = append(outcomes, sentryread.Outcome{Outcome: k.outcome, Reason: k.reason, Count: totals[k]})
	}
	return outcomes, nil
}
