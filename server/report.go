package server

import (
	"context"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/sentryread"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// The weekly report (ADR-042): what the node was over the last week, mailed to
// the ROOT User.
//
// "for a user that is one of the root identities, i want to receive a weekly report."
//
// Every section carries its own error. A source that did not answer is said in
// the section it would have filled, and the rest of the report still goes out.

const (
	reportWeek       = 7 * 24 * time.Hour
	reportThreeWeeks = 21 * 24 * time.Hour
	// The ranked sections: "top 3 4xx, top 3 5xx", "top 3 handler failures".
	reportTop = 3
)

// sentryAsker is what the report asks of Sentry.
type sentryAsker interface {
	Table(ctx context.Context, q sentryread.TableQuery) ([]map[string]any, error)
	Series(ctx context.Context, q sentryread.SeriesQuery) ([]sentryread.Point, error)
}

// Report is one week of the node, as the mail says it.
type Report struct {
	Node   string
	Window sentryread.Window

	Attestations    []namespaceCount
	AttestationsErr string
	Registrations    []namespaceCount
	RegistrationsErr string
	Restarts    int
	RestartsErr string
	Failures    []async.HandlerFailures
	FailuresErr string

	// Why Sentry was not asked at all, when it was not.
	SentryErr string

	CPU, Memory, Swap series

	NetIn, NetOut total

	Boot    []bootStep
	BootErr string

	Query    queryTimes
	QueryErr string

	Status4xx, Status5xx statusRanks

	Downtime downtime
}

type namespaceCount struct {
	Namespace string
	Count     int
	Err       string
}

type series struct {
	Points []sentryread.Point
	Err    string
}

type total struct {
	Bytes float64
	Err   string
}

type bootStep struct {
	Subsystem string
	AvgMs     float64
	MaxMs     float64
	Samples   int
}

type queryTimes struct {
	P50Ms, P95Ms, MaxMs float64
	Samples             int
}

type statusRank struct {
	Path   string
	Status int
	Count  int
}

type statusRanks struct {
	Ranks []statusRank
	Err   string
}

// downtime is minutes without a CPU sample: the node's own view of being down,
// sampled once a minute. Counted from Since, the first hour Sentry holds a
// sample for, when that is inside the window.
type downtime struct {
	WeekMinutes       int
	ThreeWeeksMinutes int
	Since             time.Time
	Err               string
}

// gatherReport asks every source for the week ending at end.
func (s *QNTXServer) gatherReport(ctx context.Context, end time.Time, sentry sentryAsker, sentryErr error) Report {
	r := Report{
		Node:   s.nodeDIDOrUnknown(),
		Window: sentryread.Window{Start: end.Add(-reportWeek), End: end},
	}

	r.Attestations, r.AttestationsErr = s.attestationsPerNamespace(r.Window)
	r.Registrations, r.RegistrationsErr = s.registrationsPerNamespace(r.Window)
	r.Restarts, r.RestartsErr = s.restartsIn(r.Window)
	r.Failures, r.FailuresErr = s.failedHandlers(r.Window)

	if sentryErr != nil {
		r.SentryErr = sentryErr.Error()
		return r
	}
	env := s.sentryEnvironment
	r.CPU = gaugeSeries(ctx, sentry, "qntx.host.cpu", env, r.Window)
	r.Memory = gaugeSeries(ctx, sentry, "qntx.host.memory", env, r.Window)
	r.Swap = gaugeSeries(ctx, sentry, "qntx.host.swap", env, r.Window)
	r.NetIn = netTotal(ctx, sentry, "qntx.host.net.in", env, r.Window)
	r.NetOut = netTotal(ctx, sentry, "qntx.host.net.out", env, r.Window)
	r.Boot, r.BootErr = bootSteps(ctx, sentry, env, r.Window)
	r.Query, r.QueryErr = queryTook(ctx, sentry, env, r.Window)
	r.Status4xx = statusesIn(ctx, sentry, env, r.Window, 400, 500)
	r.Status5xx = statusesIn(ctx, sentry, env, r.Window, 500, 600)
	r.Downtime = downtimeUpTo(ctx, sentry, env, end)
	return r
}

// ---------------------------------------------------------------------------
// What the node keeps itself
// ---------------------------------------------------------------------------

// "y attestations got created per namespace"
func (s *QNTXServer) attestationsPerNamespace(w sentryread.Window) ([]namespaceCount, string) {
	if s.held == nil {
		return nil, "this node holds no stores"
	}
	names := []string{auth.NamespaceDefault}
	if known := s.held.Known(); known != nil {
		listed, err := known.List()
		if err != nil {
			return nil, "the namespaces could not be listed: " + err.Error()
		}
		names = names[:0]
		for _, ns := range listed {
			// A switched-off namespace is not opened to be counted.
			if ns.Definition != nil && !ns.Definition.Enabled {
				continue
			}
			names = append(names, ns.Name)
		}
	}
	counts := make([]namespaceCount, 0, len(names))
	for _, name := range names {
		c := namespaceCount{Namespace: name}
		store, err := s.held.Read(name)
		if err != nil {
			c.Err = err.Error()
		} else if n, err := attestationsMadeIn(store, w.Start, w.End, storage.MaxAttestationLimit); err != nil {
			c.Err = err.Error()
		} else {
			c.Count = n
		}
		counts = append(counts, c)
	}
	slices.SortFunc(counts, func(a, b namespaceCount) int { return b.Count - a.Count })
	return counts, ""
}

// attestationsMadeIn counts the attestations made in [start, end]. No store
// counts by time, and no read pages past limit, so a window that fills a read
// is split in two and each half counted.
func attestationsMadeIn(store namespaces.Reading, start, end time.Time, limit int) (int, error) {
	found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: &start, TimeEnd: &end, Limit: limit})
	if err != nil {
		return 0, errors.Wrapf(err, "reading %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	// The window is checked here as well: not every backend honours it.
	n := 0
	for _, as := range found {
		if !as.Timestamp.Before(start) && !as.Timestamp.After(end) {
			n++
		}
	}
	if len(found) < limit {
		return n, nil
	}
	// A full read is only split when the store kept to the window. One that
	// handed back rows from outside it does not read by time, and halving the
	// window would never shrink what it hands back.
	if n < len(found) {
		return 0, errors.Newf("this store does not read by time, so the attestations made from %s to %s cannot be counted",
			start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	if end.Sub(start) <= time.Millisecond {
		return n, nil
	}
	mid := start.Add(end.Sub(start) / 2)
	first, err := attestationsMadeIn(store, start, mid.Add(-time.Millisecond), limit)
	if err != nil {
		return 0, err
	}
	second, err := attestationsMadeIn(store, mid, end, limit)
	if err != nil {
		return 0, err
	}
	return first + second, nil
}

// "x users registered per namespace"
//
// A User that registered is one that arrived at a door: its Namespace is the
// door, and CreatedAt is when (ADR-031).
func (s *QNTXServer) registrationsPerNamespace(w sentryread.Window) ([]namespaceCount, string) {
	users, err := s.authHandler.Users()
	if err != nil {
		return nil, err.Error()
	}
	return registrationsIn(users, w), ""
}

func registrationsIn(users []auth.User, w sentryread.Window) []namespaceCount {
	byDoor := map[string]int{}
	for _, u := range users {
		if u.Level != auth.LevelPublicRegistration {
			continue
		}
		at := time.UnixMilli(u.CreatedAt)
		if at.Before(w.Start) || at.After(w.End) {
			continue
		}
		byDoor[u.Namespace]++
	}
	counts := make([]namespaceCount, 0, len(byDoor))
	for door, n := range byDoor {
		counts = append(counts, namespaceCount{Namespace: door, Count: n})
	}
	slices.SortFunc(counts, func(a, b namespaceCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.Namespace < b.Namespace {
			return -1
		}
		return 1
	})
	return counts
}

// "z node restarts"
//
// Every start writes node:started as its second step, and a start that cannot
// write it does not start.
func (s *QNTXServer) restartsIn(w sentryread.Window) (int, string) {
	if s.held == nil {
		return 0, "this node holds no stores"
	}
	var store namespaces.Reading = s.held.Served()
	if s.held.KeepsSystem() {
		system, err := s.held.Read(auth.NamespaceSystem)
		if err != nil {
			return 0, err.Error()
		}
		store = system
	}
	return startsOf(store, s.nodeDIDOrUnknown(), w)
}

func startsOf(store namespaces.Reading, node string, w sentryread.Window) (int, string) {
	found, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{PredicateStarted},
		Subjects:   []string{node},
		TimeStart:  &w.Start,
		TimeEnd:    &w.End,
		Limit:      storage.MaxAttestationLimit,
	})
	if err != nil {
		return 0, "the starts could not be read: " + err.Error()
	}
	n := 0
	for _, as := range found {
		if slices.Contains(as.Predicates, PredicateStarted) && slices.Contains(as.Subjects, node) &&
			!as.Timestamp.Before(w.Start) && !as.Timestamp.After(w.End) {
			n++
		}
	}
	return n, ""
}

// "top 3 handler failures"
func (s *QNTXServer) failedHandlers(w sentryread.Window) ([]async.HandlerFailures, string) {
	if s.daemon == nil || s.daemon.GetQueue() == nil {
		return nil, "this node runs no Pulse queue"
	}
	failed, err := s.daemon.GetQueue().FailedHandlersSince(w.Start, reportTop)
	if err != nil {
		return nil, err.Error()
	}
	return failed, ""
}

// ---------------------------------------------------------------------------
// What the node sent Sentry
// ---------------------------------------------------------------------------

// metricQuery narrows a question to one metric of this node's environment.
func metricQuery(name, kind, env string) string {
	q := "metric.name:" + name + " metric.type:" + kind
	if env != "" {
		q += " environment:" + env
	}
	return q
}

// "CPU over 7 days graph", "MEM over 7 days graph", "host swap over 7d graph"
func gaugeSeries(ctx context.Context, sentry sentryAsker, name, env string, w sentryread.Window) series {
	points, err := sentry.Series(ctx, sentryread.SeriesQuery{
		Dataset:  sentryread.DatasetMetrics,
		YAxis:    "avg(value," + name + ",gauge,-)",
		Query:    metricQuery(name, "gauge", env),
		Interval: "1h",
		Window:   w,
	})
	if err != nil {
		return series{Err: err.Error()}
	}
	return series{Points: points}
}

// "7d total host.net.in", "7d total host.net.out"
//
// The gauge is bytes per second over the minute before each sample, so the
// week's bytes are every sample times its minute: the mean, times how many,
// times sixty.
func netTotal(ctx context.Context, sentry sentryAsker, name, env string, w sentryread.Window) total {
	avg, count := "avg(value,"+name+",gauge,-)", "count(value,"+name+",gauge,-)"
	rows, err := sentry.Table(ctx, sentryread.TableQuery{
		Dataset: sentryread.DatasetMetrics,
		Fields:  []string{avg, count},
		Query:   metricQuery(name, "gauge", env),
		Window:  w,
	})
	if err != nil {
		return total{Err: err.Error()}
	}
	if len(rows) == 0 {
		return total{}
	}
	return total{Bytes: number(rows[0][avg]) * number(rows[0][count]) * 60}
}

// "boot.subsystem.took over 7 days"
func bootSteps(ctx context.Context, sentry sentryAsker, env string, w sentryread.Window) ([]bootStep, string) {
	const name = "qntx.boot.subsystem.took"
	avg, maxOf, count := "avg(value,"+name+",distribution,-)", "max(value,"+name+",distribution,-)", "count(value,"+name+",distribution,-)"
	rows, err := sentry.Table(ctx, sentryread.TableQuery{
		Dataset: sentryread.DatasetMetrics,
		Fields:  []string{"subsystem", avg, maxOf, count},
		Query:   metricQuery(name, "distribution", env),
		Sort:    "-" + maxOf,
		Limit:   50,
		Window:  w,
	})
	if err != nil {
		return nil, err.Error()
	}
	steps := make([]bootStep, 0, len(rows))
	for _, row := range rows {
		steps = append(steps, bootStep{
			Subsystem: text(row["subsystem"]),
			AvgMs:     number(row[avg]),
			MaxMs:     number(row[maxOf]),
			Samples:   int(number(row[count])),
		})
	}
	return steps, ""
}

// "query took over 7d, top three slowest query"
//
// Only the times: no query's text is recorded, so the slowest are named by how
// long they took and not by what they asked (ADR-042).
func queryTook(ctx context.Context, sentry sentryAsker, env string, w sentryread.Window) (queryTimes, string) {
	const name = "qntx.query.took"
	p50, p95 := "p50(value,"+name+",distribution,millisecond)", "p95(value,"+name+",distribution,millisecond)"
	maxOf, count := "max(value,"+name+",distribution,millisecond)", "count(value,"+name+",distribution,millisecond)"
	rows, err := sentry.Table(ctx, sentryread.TableQuery{
		Dataset: sentryread.DatasetMetrics,
		Fields:  []string{p50, p95, maxOf, count},
		Query:   metricQuery(name, "distribution", env),
		Window:  w,
	})
	if err != nil {
		return queryTimes{}, err.Error()
	}
	if len(rows) == 0 {
		return queryTimes{}, ""
	}
	return queryTimes{
		P50Ms:   number(rows[0][p50]),
		P95Ms:   number(rows[0][p95]),
		MaxMs:   number(rows[0][maxOf]),
		Samples: int(number(rows[0][count])),
	}, ""
}

// "top 3 4xx, top 3 5xx"
//
// Every request answered 400 or above is logged as "http METHOD PATH STATUS"
// with its path and status (accesslog.go).
func statusesIn(ctx context.Context, sentry sentryAsker, env string, w sentryread.Window, from, below int) statusRanks {
	const status = "tags[status,number]"
	query := `message:"http *" AND ` + status + `:>=` + strconv.Itoa(from) + ` AND ` + status + `:<` + strconv.Itoa(below)
	if env != "" {
		query += " AND environment:" + env
	}
	rows, err := sentry.Table(ctx, sentryread.TableQuery{
		Dataset: sentryread.DatasetLogs,
		Fields:  []string{"path", status, "count()"},
		Query:   query,
		Sort:    "-count()",
		Limit:   reportTop,
		Window:  w,
	})
	if err != nil {
		return statusRanks{Err: err.Error()}
	}
	ranks := make([]statusRank, 0, len(rows))
	for _, row := range rows {
		ranks = append(ranks, statusRank{Path: text(row["path"]), Status: int(number(row[status])), Count: int(number(row["count()"]))})
	}
	return statusRanks{Ranks: ranks}
}

// "downtime over last week", "downtime over last 3 weeks"
//
// Minutes without a CPU sample, counted a whole hour at a time up to the start
// of the current hour, which is not over yet.
func downtimeUpTo(ctx context.Context, sentry sentryAsker, env string, now time.Time) downtime {
	end := now.UTC().Truncate(time.Hour)
	w := sentryread.Window{Start: end.Add(-reportThreeWeeks), End: end}
	points, err := sentry.Series(ctx, sentryread.SeriesQuery{
		Dataset:  sentryread.DatasetMetrics,
		YAxis:    "count(value,qntx.host.cpu,gauge,-)",
		Query:    metricQuery("qntx.host.cpu", "gauge", env),
		Interval: "1h",
		Window:   w,
	})
	if err != nil {
		return downtime{Err: err.Error()}
	}
	return downtimeFrom(points, end)
}

// downtimeFrom reads hourly sample counts. An hour holds sixty samples when the
// node was up all of it. Hours before the first sample Sentry holds are not
// counted: no data yet is not down.
func downtimeFrom(points []sentryread.Point, end time.Time) downtime {
	var d downtime
	weekStart := end.Add(-reportWeek)
	started := false
	for _, p := range points {
		if !p.At.Before(end) {
			continue
		}
		if !started {
			if p.Value <= 0 {
				continue
			}
			started = true
			d.Since = p.At
		}
		missing := 60 - int(math.Round(p.Value))
		if missing < 0 {
			missing = 0
		}
		d.ThreeWeeksMinutes += missing
		if !p.At.Before(weekStart) {
			d.WeekMinutes += missing
		}
	}
	if !started {
		d.Err = "Sentry holds no CPU sample in the last three weeks"
	}
	return d
}

// number reads a JSON number Sentry answered, or zero.
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

// text reads a JSON string Sentry answered, or empty.
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
