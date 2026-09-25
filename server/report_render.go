package server

import (
	"bytes"
	"fmt"
	"html"
	"image"
	"image/color"
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

	start, end := r.Window.Start.UTC().Format("2006-01-02"), r.Window.End.UTC().Format("2006-01-02")
	return services.NodeMail{
		Name:    reportHandlerName,
		Subject: "QNTX week " + start + " to " + end,
		HTML:    renderReportHTML(r, drawn),
		Text:    renderReportText(r),
		Inline:  images,
	}, nil
}

// ---------------------------------------------------------------------------
// html
// ---------------------------------------------------------------------------

type htmlDoc struct{ b strings.Builder }

func (d *htmlDoc) raw(s string) { d.b.WriteString(s) }

func (d *htmlDoc) text(s string) { d.b.WriteString(html.EscapeString(s)) }

func (d *htmlDoc) heading(s string) {
	d.raw(`<h2 style="font-size:15px;margin:24px 0 8px">`)
	d.text(s)
	d.raw(`</h2>`)
}

func (d *htmlDoc) said(s string) {
	d.raw(`<p style="margin:0 0 8px;color:#8a1c1c">`)
	d.text(s)
	d.raw(`</p>`)
}

func (d *htmlDoc) line(s string) {
	d.raw(`<p style="margin:0 0 8px">`)
	d.text(s)
	d.raw(`</p>`)
}

func (d *htmlDoc) table(head []string, rows [][]string) {
	d.raw(`<table style="border-collapse:collapse;font-size:13px">`)
	d.raw(`<tr>`)
	for _, h := range head {
		d.raw(`<th style="text-align:left;padding:2px 12px 2px 0;border-bottom:1px solid #ccc">`)
		d.text(h)
		d.raw(`</th>`)
	}
	d.raw(`</tr>`)
	for _, row := range rows {
		d.raw(`<tr>`)
		for _, cell := range row {
			d.raw(`<td style="padding:2px 12px 2px 0;vertical-align:top">`)
			d.text(cell)
			d.raw(`</td>`)
		}
		d.raw(`</tr>`)
	}
	d.raw(`</table>`)
}

func renderReportHTML(r Report, drawn map[string]bool) string {
	var d htmlDoc
	d.raw(`<!DOCTYPE html><html><body style="margin:0;padding:24px;background:#ffffff;color:#1a1a1a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;font-size:14px;line-height:1.5"><div style="max-width:640px;margin:0 auto">`)
	d.raw(`<h1 style="font-size:18px;margin:0 0 4px">`)
	d.text("QNTX week " + r.Window.Start.UTC().Format("2006-01-02 15:04") + " to " + r.Window.End.UTC().Format("2006-01-02 15:04") + " UTC")
	d.raw(`</h1>`)
	d.line(r.Node)

	d.heading("Attestations created per namespace")
	if r.AttestationsErr != "" {
		d.said(r.AttestationsErr)
	} else {
		d.table([]string{"Namespace", "Created"}, namespaceRows(r.Attestations))
	}

	d.heading("Users registered per namespace")
	if r.RegistrationsErr != "" {
		d.said(r.RegistrationsErr)
	} else if len(r.Registrations) == 0 {
		d.line("None.")
	} else {
		d.table([]string{"Namespace", "Registered"}, namespaceRows(r.Registrations))
	}

	d.heading("Node restarts")
	if r.RestartsErr != "" {
		d.said(r.RestartsErr)
	} else {
		d.line(fmt.Sprintf("%d", r.Restarts))
	}

	// What Sentry holds is said once when Sentry was not asked, not under every
	// section it would have filled.
	if r.SentryErr != "" {
		d.heading("From Sentry: downtime, CPU, memory, swap, network, boot, queries, 4xx and 5xx")
		d.said(r.SentryErr)
	} else {
		renderSentryHTML(&d, r, drawn)
	}

	d.heading("Top 3 handler failures")
	switch {
	case r.FailuresErr != "":
		d.said(r.FailuresErr)
	case len(r.Failures) == 0:
		d.line("None.")
	default:
		rows := make([][]string, 0, len(r.Failures))
		for _, f := range r.Failures {
			rows = append(rows, []string{f.Handler, fmt.Sprintf("%d", f.Failures), f.LastError})
		}
		d.table([]string{"Handler", "Failures", "Last error"}, rows)
	}

	d.raw(`</div></body></html>`)
	return d.b.String()
}

// renderSentryHTML is the sections Sentry filled.
func renderSentryHTML(d *htmlDoc, r Report, drawn map[string]bool) {
	d.heading("Downtime")
	if r.Downtime.Err != "" {
		d.said(r.Downtime.Err)
	} else {
		d.line(downtimeLine(r.Downtime))
	}

	for _, g := range r.graphs() {
		d.heading(g.title + " over 7 days")
		switch {
		case g.s.Err != "":
			d.said(g.s.Err)
		case !drawn[g.id]:
			d.line("No samples.")
		default:
			d.raw(`<img src="cid:` + g.id + `" width="` + fmt.Sprint(graphWidth) + `" height="` + fmt.Sprint(graphHeight) + `" alt="` + g.title + ` over 7 days" style="display:block;max-width:100%;border:1px solid #ddd">`)
			d.line(seriesLine(g.s.Points))
		}
	}

	d.heading("Network over 7 days")
	d.table([]string{"", "Total"}, [][]string{
		{"host.net.in", totalCell(r.NetIn)},
		{"host.net.out", totalCell(r.NetOut)},
	})

	d.heading("boot.subsystem.took over 7 days")
	if r.BootErr != "" {
		d.said(r.BootErr)
	} else {
		d.table([]string{"Subsystem", "Mean", "Max", "Samples"}, bootRows(r.Boot))
	}

	d.heading("Query took over 7 days")
	if r.QueryErr != "" {
		d.said(r.QueryErr)
	} else {
		d.line(queryLine(r.Query))
		d.line("Which queries were slowest is not known: no query's text is recorded.")
	}

	for _, ranked := range []struct {
		title string
		ranks statusRanks
	}{{"Top 3 4xx", r.Status4xx}, {"Top 3 5xx", r.Status5xx}} {
		d.heading(ranked.title)
		switch {
		case ranked.ranks.Err != "":
			d.said(ranked.ranks.Err)
		case len(ranked.ranks.Ranks) == 0:
			d.line("None.")
		default:
			d.table([]string{"Path", "Status", "Count"}, statusRows(ranked.ranks.Ranks))
		}
	}
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
		for _, row := range bootRows(r.Boot) {
			w("  %s: mean %s, max %s, %s samples", row[0], row[1], row[2], row[3])
		}
	})
	sentry("Query took over 7 days", r.QueryErr, func() {
		w("  %s", queryLine(r.Query))
		w("  Which queries were slowest is not known: no query's text is recorded.")
	})
	sentry("Top 3 4xx", r.Status4xx.Err, func() {
		for _, row := range statusRows(r.Status4xx.Ranks) {
			w("  %s %s: %s", row[1], row[0], row[2])
		}
	})
	sentry("Top 3 5xx", r.Status5xx.Err, func() {
		for _, row := range statusRows(r.Status5xx.Ranks) {
			w("  %s %s: %s", row[1], row[0], row[2])
		}
	})
	w("\nTop 3 handler failures")
	orSaid(r.FailuresErr, func() {
		if len(r.Failures) == 0 {
			w("  None.")
		}
		for _, f := range r.Failures {
			w("  %s: %d, last: %s", f.Handler, f.Failures, f.LastError)
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

func bootRows(steps []bootStep) [][]string {
	rows := make([][]string, 0, len(steps))
	for _, s := range steps {
		rows = append(rows, []string{s.Subsystem, fmt.Sprintf("%.0f ms", s.AvgMs), fmt.Sprintf("%.0f ms", s.MaxMs), fmt.Sprintf("%d", s.Samples)})
	}
	return rows
}

func statusRows(ranks []statusRank) [][]string {
	rows := make([][]string, 0, len(ranks))
	for _, r := range ranks {
		rows = append(rows, []string{r.Path, fmt.Sprintf("%d", r.Status), fmt.Sprintf("%d", r.Count)})
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

// seriesLine is what a graph shows, in words: the hours that had samples.
func seriesLine(points []sentryread.Point) string {
	lo, hi, sum, n := math.Inf(1), math.Inf(-1), 0.0, 0
	for _, p := range points {
		if p.Value <= 0 {
			continue
		}
		lo, hi = math.Min(lo, p.Value), math.Max(hi, p.Value)
		sum += p.Value
		n++
	}
	if n == 0 {
		return "No samples."
	}
	return fmt.Sprintf("mean %.1f%%, lowest hour %.1f%%, highest hour %.1f%%", sum/float64(n), lo, hi)
}

// ---------------------------------------------------------------------------
// graphs
// ---------------------------------------------------------------------------

var (
	graphBackground = color.RGBA{0xff, 0xff, 0xff, 0xff}
	graphGrid       = color.RGBA{0xee, 0xee, 0xee, 0xff}
	graphDay        = color.RGBA{0xdd, 0xdd, 0xdd, 0xff}
	graphFill       = color.RGBA{0xc8, 0xdc, 0xf0, 0xff}
	graphLine       = color.RGBA{0x1f, 0x5f, 0x9f, 0xff}
)

// drawPercentGraph draws hourly percentages on 0 to 100: a line with the area
// under it filled, a faint rule at every quarter, and one at every UTC
// midnight. An hour with no sample is a gap, not a fall to zero.
func drawPercentGraph(points []sentryread.Point) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, graphWidth, graphHeight))
	for y := 0; y < graphHeight; y++ {
		for x := 0; x < graphWidth; x++ {
			img.Set(x, y, graphBackground)
		}
	}
	for _, q := range []float64{25, 50, 75} {
		y := yOf(q)
		for x := 0; x < graphWidth; x++ {
			img.Set(x, y, graphGrid)
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
			for y := 0; y < graphHeight; y++ {
				img.Set(x, y, graphDay)
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
			top := yOf(v)
			for y := top + 1; y < graphHeight; y++ {
				img.Set(x, y, graphFill)
			}
		}
		drawLine(img, x0, yOf(a.Value), x1, yOf(b.Value), graphLine)
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
