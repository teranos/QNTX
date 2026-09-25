package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"

	"github.com/teranos/QNTX/internal/sentryread"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/errors"
)

// The weekly report as a mail (ADR-042): html for the reader, text for a client
// that shows no html, and the three graphs as images the html shows inline.
// The graphs carry no text of their own; what they say is written beside them.

const (
	graphWidth  = 600
	graphHeight = 160
)

type graph struct {
	id    string
	title string
	s     series
}

func (r Report) graphs() []graph {
	return []graph{
		{id: "cpu", title: "CPU", s: r.CPU},
		{id: "memory", title: "Memory", s: r.Memory},
		{id: "swap", title: "Swap", s: r.Swap},
	}
}

// renderReport is the mail the report is sent as.
func renderReport(r Report) (services.NodeMail, error) {
	var images []services.InlineImage
	drawn := map[string]bool{}
	for _, g := range r.graphs() {
		if g.s.Err != "" || len(g.s.Points) == 0 {
			continue
		}
		img, err := drawPercentGraph(g.s.Points)
		if err != nil {
			return services.NodeMail{}, err
		}
		images = append(images, services.InlineImage{
			ContentID: g.id, ContentType: "image/png", FileName: g.id + ".png", Data: img,
		})
		drawn[g.id] = true
	}

	html, err := services.DrawMail(reportWindow(r, drawn))
	if err != nil {
		return services.NodeMail{}, err
	}
	start, end := r.Window.Start.UTC().Format("2006-01-02"), r.Window.End.UTC().Format("2006-01-02")
	return services.NodeMail{
		Name:    reportHandlerName,
		Subject: "QNTX week " + start + " to " + end,
		HTML:    html,
		Text:    renderReportText(r),
		Inline:  images,
	}, nil
}

// ---------------------------------------------------------------------------
// html
// ---------------------------------------------------------------------------

// "it just needs to fit with the rest of qntx"
//
// The report is one window, drawn the way ≡ am draws the node: a section per
// part, labelled rows in each.

func said(err string) services.MailRow {
	return services.MailRow{Value: err, State: services.RowUnwell}
}

func none() services.MailRow {
	return services.MailRow{Value: "None.", State: services.RowNote}
}

func reportWindow(r Report, drawn map[string]bool) services.MailWindow {
	node := services.MailSection{Title: "Node", Rows: []services.MailRow{{Label: "DID:", Value: r.Node, Key: true}}}
	if r.RestartsErr != "" {
		node.Rows = append(node.Rows, services.MailRow{Label: "Restarts:", Value: r.RestartsErr, State: services.RowUnwell})
	} else {
		node.Rows = append(node.Rows, services.MailRow{Label: "Restarts:", Value: fmt.Sprintf("%d", r.Restarts)})
	}
	if r.SentryErr == "" {
		if r.Downtime.Err != "" {
			node.Rows = append(node.Rows, services.MailRow{Label: "Downtime:", Value: r.Downtime.Err, State: services.RowUnwell})
		} else {
			node.Rows = append(node.Rows,
				services.MailRow{Label: "Downtime last week:", Value: minutesIn(r.Downtime.WeekMinutes)},
				services.MailRow{Label: "Downtime last 3 weeks:", Value: minutesIn(r.Downtime.ThreeWeeksMinutes)})
			if !r.Downtime.Since.IsZero() {
				node.Rows = append(node.Rows, services.MailRow{Label: "Counted from:", Value: r.Downtime.Since.UTC().Format("2006-01-02 15:04") + " UTC", State: services.RowNote})
			}
		}
	}

	sections := []services.MailSection{
		node,
		countSection("Attestations created per namespace", r.Attestations, r.AttestationsErr),
		countSection("Users registered per namespace", r.Registrations, r.RegistrationsErr),
	}

	// What Sentry holds is said once when Sentry was not asked, not under every
	// section it would have filled.
	if r.SentryErr != "" {
		sections = append(sections, services.MailSection{
			Title: "From Sentry: downtime, CPU, memory, swap, network, boot, queries, 4xx and 5xx",
			Rows:  []services.MailRow{said(r.SentryErr)},
		})
	} else {
		sections = append(sections, sentrySections(r, drawn)...)
	}

	failures := services.MailSection{Title: "Top 3 handler failures"}
	switch {
	case r.FailuresErr != "":
		failures.Rows = []services.MailRow{said(r.FailuresErr)}
	case len(r.Failures) == 0:
		failures.Rows = []services.MailRow{none()}
	default:
		for _, f := range r.Failures {
			failures.Rows = append(failures.Rows, services.MailRow{Label: f.Handler, Value: f.Error, State: services.RowUnwell})
		}
	}
	sections = append(sections, failures)

	return services.MailWindow{
		Symbol:   "≡",
		Title:    "QNTX week " + r.Window.Start.UTC().Format("2006-01-02 15:04") + " to " + r.Window.End.UTC().Format("2006-01-02 15:04") + " UTC",
		Sections: sections,
	}
}

func countSection(title string, counts []namespaceCount, err string) services.MailSection {
	s := services.MailSection{Title: title}
	switch {
	case err != "":
		s.Rows = []services.MailRow{said(err)}
	case len(counts) == 0:
		s.Rows = []services.MailRow{none()}
	default:
		for _, row := range namespaceRows(counts) {
			s.Rows = append(s.Rows, services.MailRow{Label: row[0] + ":", Value: row[1]})
		}
	}
	return s
}

// sentrySections is the sections Sentry filled.
func sentrySections(r Report, drawn map[string]bool) []services.MailSection {
	var sections []services.MailSection
	for _, g := range r.graphs() {
		s := services.MailSection{Title: g.title + " over 7 days"}
		switch {
		case g.s.Err != "":
			s.Rows = []services.MailRow{said(g.s.Err)}
		case !drawn[g.id]:
			s.Rows = []services.MailRow{{Value: "No samples.", State: services.RowNote}}
		default:
			s.HTML = `<img src="cid:` + g.id + `" width="` + fmt.Sprint(graphWidth) + `" height="` + fmt.Sprint(graphHeight) + `" alt="` + g.title + ` over 7 days" style="display:block;width:100%;max-width:` + fmt.Sprint(graphWidth) + `px;height:auto;margin:0 0 4px">`
			mean, lo, hi, n := seriesStats(g.s.Points)
			if n == 0 {
				s.Rows = []services.MailRow{{Value: "No samples.", State: services.RowNote}}
			} else {
				s.Rows = []services.MailRow{
					{Label: "Mean:", Value: fmt.Sprintf("%.1f%%", mean)},
					{Label: "Lowest hour:", Value: fmt.Sprintf("%.1f%%", lo)},
					{Label: "Highest hour:", Value: fmt.Sprintf("%.1f%%", hi)},
				}
			}
		}
		sections = append(sections, s)
	}

	sections = append(sections, services.MailSection{Title: "Network over 7 days", Rows: []services.MailRow{
		{Label: "host.net.in:", Value: totalCell(r.NetIn)},
		{Label: "host.net.out:", Value: totalCell(r.NetOut)},
	}})

	boot := services.MailSection{Title: "boot.subsystem.took over 7 days"}
	if r.BootErr != "" {
		boot.Rows = []services.MailRow{said(r.BootErr)}
	} else {
		for _, b := range r.Boot {
			boot.Rows = append(boot.Rows, services.MailRow{Label: b.Subsystem + ":", Value: fmt.Sprintf("mean %.0f ms, max %.0f ms", b.AvgMs, b.MaxMs)})
		}
	}
	sections = append(sections, boot)

	query := services.MailSection{Title: "Query took over 7 days"}
	if r.QueryErr != "" {
		query.Rows = []services.MailRow{said(r.QueryErr)}
	} else {
		query.Rows = []services.MailRow{
			{Label: "p50:", Value: fmt.Sprintf("%.0f ms", r.Query.P50Ms)},
			{Label: "p95:", Value: fmt.Sprintf("%.0f ms", r.Query.P95Ms)},
			{Label: "Max:", Value: fmt.Sprintf("%.0f ms", r.Query.MaxMs)},
			{Value: "Which queries were slowest is not known: no query's text is recorded.", State: services.RowNote},
		}
	}
	sections = append(sections, query)

	// "I WANT TO SEE WHAT WAS TRIED TO ACCESS INSTEAD"
	for _, ranked := range []struct {
		title string
		ranks statusRanks
		state services.RowState
	}{{"Top 3 4xx", r.Status4xx, services.RowPlain}, {"Top 3 5xx", r.Status5xx, services.RowUnwell}} {
		s := services.MailSection{Title: ranked.title}
		switch {
		case ranked.ranks.Err != "":
			s.Rows = []services.MailRow{said(ranked.ranks.Err)}
		case len(ranked.ranks.Ranks) == 0:
			s.Rows = []services.MailRow{none()}
		default:
			for _, rank := range ranked.ranks.Ranks {
				s.Rows = append(s.Rows, services.MailRow{Label: fmt.Sprintf("%d", rank.Status), Value: rank.Path, State: ranked.state})
			}
		}
		sections = append(sections, s)
	}
	return sections
}

// ---------------------------------------------------------------------------
// text
// ---------------------------------------------------------------------------

func renderReportText(r Report) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	orSaid := func(err string, fn func()) {
		if err != "" {
			w("  %s", err)
			return
		}
		fn()
	}

	w("QNTX week %s to %s UTC", r.Window.Start.UTC().Format("2006-01-02 15:04"), r.Window.End.UTC().Format("2006-01-02 15:04"))
	w("%s", r.Node)

	w("\nAttestations created per namespace")
	orSaid(r.AttestationsErr, func() {
		for _, row := range namespaceRows(r.Attestations) {
			w("  %s: %s", row[0], row[1])
		}
	})
	w("\nUsers registered per namespace")
	orSaid(r.RegistrationsErr, func() {
		if len(r.Registrations) == 0 {
			w("  None.")
		}
		for _, row := range namespaceRows(r.Registrations) {
			w("  %s: %s", row[0], row[1])
		}
	})
	w("\nNode restarts")
	orSaid(r.RestartsErr, func() { w("  %d", r.Restarts) })

	// What Sentry holds is said once when Sentry was not asked.
	if r.SentryErr != "" {
		w("\nFrom Sentry: downtime, CPU, memory, swap, network, boot, queries, 4xx and 5xx")
		w("  %s", r.SentryErr)
	}
	sentry := func(title, err string, fn func()) {
		if r.SentryErr != "" {
			return
		}
		w("\n%s", title)
		orSaid(err, fn)
	}
	sentry("Downtime", r.Downtime.Err, func() { w("  %s", downtimeLine(r.Downtime)) })
	for _, g := range r.graphs() {
		sentry(g.title+" over 7 days", g.s.Err, func() { w("  %s", seriesLine(g.s.Points)) })
	}
	sentry("Network over 7 days", "", func() {
		w("  host.net.in: %s", totalCell(r.NetIn))
		w("  host.net.out: %s", totalCell(r.NetOut))
	})
	sentry("boot.subsystem.took over 7 days", r.BootErr, func() {
		for _, b := range r.Boot {
			w("  %s: mean %.0f ms, max %.0f ms", b.Subsystem, b.AvgMs, b.MaxMs)
		}
	})
	sentry("Query took over 7 days", r.QueryErr, func() {
		w("  %s", queryLine(r.Query))
		w("  Which queries were slowest is not known: no query's text is recorded.")
	})
	sentry("Top 3 4xx", r.Status4xx.Err, func() {
		for _, rank := range r.Status4xx.Ranks {
			w("  %d %s", rank.Status, rank.Path)
		}
	})
	sentry("Top 3 5xx", r.Status5xx.Err, func() {
		for _, rank := range r.Status5xx.Ranks {
			w("  %d %s", rank.Status, rank.Path)
		}
	})
	w("\nTop 3 handler failures")
	orSaid(r.FailuresErr, func() {
		if len(r.Failures) == 0 {
			w("  None.")
		}
		for _, f := range r.Failures {
			w("  %s: %s", f.Handler, f.Error)
		}
	})
	return b.String()
}

// ---------------------------------------------------------------------------
// shared cells
// ---------------------------------------------------------------------------

func namespaceRows(counts []namespaceCount) [][]string {
	rows := make([][]string, 0, len(counts))
	for _, c := range counts {
		name := c.Namespace
		if name == "" {
			name = "(no door)"
		}
		value := fmt.Sprintf("%d", c.Count)
		if c.Err != "" {
			value = "not read: " + c.Err
		}
		rows = append(rows, []string{name, value})
	}
	return rows
}

func totalCell(t total) string {
	if t.Err != "" {
		return "not read: " + t.Err
	}
	return bytesIn(t.Bytes)
}

// bytesIn writes a byte count in SI units.
func bytesIn(n float64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	i := 0
	for n >= 1000 && i < len(units)-1 {
		n /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", n, units[i])
	}
	return fmt.Sprintf("%.2f %s", n, units[i])
}

func queryLine(q queryTimes) string {
	return fmt.Sprintf("p50 %.0f ms, p95 %.0f ms, max %.0f ms, %d queries", q.P50Ms, q.P95Ms, q.MaxMs, q.Samples)
}

func downtimeLine(d downtime) string {
	s := fmt.Sprintf("last week %s, last 3 weeks %s", minutesIn(d.WeekMinutes), minutesIn(d.ThreeWeeksMinutes))
	if !d.Since.IsZero() {
		s += fmt.Sprintf(" (counted from %s UTC, the first hour Sentry holds a sample for)", d.Since.UTC().Format("2006-01-02 15:04"))
	}
	return s
}

func minutesIn(m int) string {
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// seriesStats is the hours that had samples: their mean, lowest and highest.
func seriesStats(points []sentryread.Point) (mean, lo, hi float64, n int) {
	lo, hi = math.Inf(1), math.Inf(-1)
	sum := 0.0
	for _, p := range points {
		if p.Value <= 0 {
			continue
		}
		lo, hi = math.Min(lo, p.Value), math.Max(hi, p.Value)
		sum += p.Value
		n++
	}
	if n == 0 {
		return 0, 0, 0, 0
	}
	return sum / float64(n), lo, hi, n
}

// seriesLine is what a graph shows, in words.
func seriesLine(points []sentryread.Point) string {
	mean, lo, hi, n := seriesStats(points)
	if n == 0 {
		return "No samples."
	}
	return fmt.Sprintf("mean %.1f%%, lowest hour %.1f%%, highest hour %.1f%%", mean, lo, hi)
}

// ---------------------------------------------------------------------------
// graphs
// ---------------------------------------------------------------------------

// drawPercentGraph draws hourly percentages on 0 to 100 in the window the
// report stands in: QNTX's accent over its fill, a rule at every quarter and
// at every UTC midnight. An hour with no sample is a gap, not a fall to zero.
func drawPercentGraph(points []sentryread.Point) ([]byte, error) {
	ink, err := services.MailGraphColours()
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, graphWidth, graphHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: ink.Background}, image.Point{}, draw.Src)
	for _, q := range []float64{25, 50, 75} {
		y := yOf(q)
		for x := range graphWidth {
			img.Set(x, y, ink.Rule)
		}
	}

	n := len(points)
	xOf := func(i int) int {
		if n <= 1 {
			return 0
		}
		return i * (graphWidth - 1) / (n - 1)
	}
	for i, p := range points {
		if p.At.UTC().Hour() == 0 && p.At.Minute() == 0 {
			x := xOf(i)
			for y := range graphHeight {
				img.Set(x, y, ink.Rule)
			}
		}
	}

	for i := 0; i+1 < n; i++ {
		a, b := points[i], points[i+1]
		if a.Value <= 0 || b.Value <= 0 {
			continue
		}
		x0, x1 := xOf(i), xOf(i+1)
		for x := x0; x <= x1; x++ {
			t := 0.0
			if x1 > x0 {
				t = float64(x-x0) / float64(x1-x0)
			}
			v := a.Value + (b.Value-a.Value)*t
			for y := yOf(v) + 1; y < graphHeight; y++ {
				img.Set(x, y, ink.Fill)
			}
		}
		drawLine(img, x0, yOf(a.Value), x1, yOf(b.Value), ink.Line)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, errors.Wrap(err, "the graph could not be encoded")
	}
	return buf.Bytes(), nil
}

func yOf(percent float64) int {
	percent = math.Max(0, math.Min(100, percent))
	return graphHeight - 1 - int(math.Round(percent/100*float64(graphHeight-1)))
}

// drawLine is Bresenham's, two pixels thick.
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		img.Set(x0, y0, c)
		img.Set(x0, y0-1, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
