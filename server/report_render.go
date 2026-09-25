package server

import (
	"bytes"
	"fmt"
	"html"
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

// "what is the bg color of the canvas, what pattern does it use, what are the colors of the ax element, and the type element, and the sigma element, and the attestation element, and the triplet."
//
// The report is drawn the way QNTX is: on its canvas, each part a window in
// the ink of the element closest to what it holds.

// win is one window's body, written in its ink.
type win struct {
	b       strings.Builder
	ink     services.Ink
	written bool
}

func (w *win) raw(s string) { w.b.WriteString(s); w.written = true }

func (w *win) text(s string) { w.raw(html.EscapeString(s)) }

// heading is a part within a window, in the ink's keyword colour.
func (w *win) heading(s string) {
	top := "0"
	if w.written {
		top = "16px"
	}
	w.raw(`<div style="color:` + w.ink.Keyword + `;font-size:11px;letter-spacing:0.08em;text-transform:uppercase;margin:` + top + ` 0 6px">`)
	w.text(s)
	w.raw(`</div>`)
}

func (w *win) said(s string) {
	w.raw(`<p style="margin:0 0 8px;color:` + services.Canvas.Alert.Value + `">`)
	w.text(s)
	w.raw(`</p>`)
}

func (w *win) line(s string) {
	w.raw(`<p style="margin:0 0 8px">`)
	w.text(s)
	w.raw(`</p>`)
}

func (w *win) quiet(s string) {
	w.raw(`<p style="margin:0 0 8px;color:` + w.ink.Keyword + `">`)
	w.text(s)
	w.raw(`</p>`)
}

// table is labelled columns: the labels in the ink's keyword colour, the cells
// in colour, or in the ink's value colour when colour is empty.
func (w *win) table(head []string, rows [][]string, colour string) {
	if colour == "" {
		colour = w.ink.Value
	}
	w.raw(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;font-size:13px">`)
	w.raw(`<tr>`)
	for _, h := range head {
		w.raw(`<td style="color:` + w.ink.Keyword + `;padding:2px 16px 4px 0;border-bottom:1px solid ` + services.Canvas.Border + `">`)
		w.text(h)
		w.raw(`</td>`)
	}
	w.raw(`</tr>`)
	for _, row := range rows {
		w.raw(`<tr>`)
		for _, cell := range row {
			w.raw(`<td style="color:` + colour + `;padding:3px 16px 0 0;vertical-align:top">`)
			w.text(cell)
			w.raw(`</td>`)
		}
		w.raw(`</tr>`)
	}
	w.raw(`</table>`)
}

// canvas is the windows standing on the report's canvas, in order.
type canvas struct{ b strings.Builder }

// window stands one window on the canvas. symbol is one of web/ts/sym.ts.
func (c *canvas) window(symbol, title string, ink services.Ink, titleBar string, fill func(w *win)) {
	w := &win{ink: ink}
	fill(w)
	c.b.WriteString(services.CanvasWindow(symbol, html.EscapeString(title), ink, titleBar, w.b.String()))
}

func renderReportHTML(r Report, drawn map[string]bool) string {
	ink, bar := services.Canvas, services.Canvas.TitleBar
	var c canvas

	c.window("≡", "QNTX week "+r.Window.Start.UTC().Format("2006-01-02 15:04")+" to "+r.Window.End.UTC().Format("2006-01-02 15:04")+" UTC", ink.Ax, bar, func(w *win) {
		w.quiet(r.Node)
		w.heading("Node restarts")
		if r.RestartsErr != "" {
			w.said(r.RestartsErr)
		} else {
			w.line(fmt.Sprintf("%d", r.Restarts))
		}
	})

	c.window("⎔", "Namespaces", ink.Attestation, bar, func(w *win) {
		w.heading("Attestations created per namespace")
		if r.AttestationsErr != "" {
			w.said(r.AttestationsErr)
		} else {
			w.table([]string{"Namespace", "Created"}, namespaceRows(r.Attestations), "")
		}
		w.heading("Users registered per namespace")
		switch {
		case r.RegistrationsErr != "":
			w.said(r.RegistrationsErr)
		case len(r.Registrations) == 0:
			w.line("None.")
		default:
			w.table([]string{"Namespace", "Registered"}, namespaceRows(r.Registrations), "")
		}
	})

	// What Sentry holds is said once when Sentry was not asked, not under every
	// section it would have filled.
	if r.SentryErr != "" {
		c.window("Σ", "From Sentry: downtime, CPU, memory, swap, network, boot, queries, 4xx and 5xx", ink.Sigma, bar, func(w *win) {
			w.said(r.SentryErr)
		})
	} else {
		renderSentryHTML(&c, r, drawn)
	}

	// A failing handler takes the alert's window; a week without one does not.
	failures, failuresBar := ink.Ax, bar
	if len(r.Failures) > 0 || r.FailuresErr != "" {
		failures, failuresBar = ink.Alert, services.AlertTitleBar
	}
	c.window("꩜", "Top 3 handler failures", failures, failuresBar, func(w *win) {
		switch {
		case r.FailuresErr != "":
			w.said(r.FailuresErr)
		case len(r.Failures) == 0:
			w.line("None.")
		default:
			rows := make([][]string, 0, len(r.Failures))
			for _, f := range r.Failures {
				rows = append(rows, []string{f.Handler, fmt.Sprintf("%d", f.Failures), f.LastError})
			}
			w.table([]string{"Handler", "Failures", "Last error"}, rows, "")
		}
	})

	return services.CanvasPage(c.b.String())
}

// renderSentryHTML is the windows Sentry filled.
func renderSentryHTML(c *canvas, r Report, drawn map[string]bool) {
	ink, bar := services.Canvas, services.Canvas.TitleBar

	// The sigma element is the sum of many observations; so is every number
	// Sentry answers with.
	c.window("Σ", "The host over 7 days", ink.Sigma, bar, func(w *win) {
		w.heading("Downtime")
		if r.Downtime.Err != "" {
			w.said(r.Downtime.Err)
		} else {
			w.line(downtimeLine(r.Downtime))
		}
		for _, g := range r.graphs() {
			w.heading(g.title + " over 7 days")
			switch {
			case g.s.Err != "":
				w.said(g.s.Err)
			case !drawn[g.id]:
				w.line("No samples.")
			default:
				w.raw(`<img src="cid:` + g.id + `" width="` + fmt.Sprint(graphWidth) + `" height="` + fmt.Sprint(graphHeight) + `" alt="` + g.title + ` over 7 days" style="display:block;max-width:100%;height:auto;border:1px solid ` + ink.Border + `;border-radius:3px;margin:0 0 6px">`)
				w.line(seriesLine(g.s.Points))
			}
		}
		w.heading("Network over 7 days")
		w.table([]string{"", "Total"}, [][]string{
			{"host.net.in", totalCell(r.NetIn)},
			{"host.net.out", totalCell(r.NetOut)},
		}, "")
	})

	c.window("✿", "boot.subsystem.took over 7 days", ink.Type, bar, func(w *win) {
		if r.BootErr != "" {
			w.said(r.BootErr)
			return
		}
		w.table([]string{"Subsystem", "Mean", "Max", "Samples"}, bootRows(r.Boot), "")
	})

	c.window("⋈", "Query took over 7 days", ink.Ax, bar, func(w *win) {
		if r.QueryErr != "" {
			w.said(r.QueryErr)
			return
		}
		w.line(queryLine(r.Query))
		w.quiet("Which queries were slowest is not known: no query's text is recorded.")
	})

	// Responses grouped by path and status, the way the triplet element groups
	// attestations. A 5xx is written in the alert's colour.
	c.window("⫶", "Top 3 4xx and 5xx", ink.Triplet, bar, func(w *win) {
		for _, ranked := range []struct {
			title  string
			ranks  statusRanks
			colour string
		}{{"Top 3 4xx", r.Status4xx, ""}, {"Top 3 5xx", r.Status5xx, ink.Alert.Value}} {
			w.heading(ranked.title)
			switch {
			case ranked.ranks.Err != "":
				w.said(ranked.ranks.Err)
			case len(ranked.ranks.Ranks) == 0:
				w.line("None.")
			default:
				w.table([]string{"Path", "Status", "Count"}, statusRows(ranked.ranks.Ranks), ranked.colour)
			}
		}
	})
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

// graphInk is what a graph is drawn in: the canvas and its 24px grid, rules in
// the windows' border, and the sigma element's bar over its shade.
type graphInk struct {
	background, grid, rule, shade, line color.NRGBA
}

func graphInkOf() (graphInk, error) {
	var g graphInk
	for _, c := range []struct {
		into *color.NRGBA
		css  string
	}{
		{&g.background, services.Canvas.Background},
		{&g.grid, services.Canvas.Grid},
		{&g.rule, services.Canvas.Border},
		{&g.shade, services.Canvas.SigmaBarShade},
		{&g.line, services.Canvas.SigmaBar},
	} {
		v, err := services.ColorOf(c.css)
		if err != nil {
			return graphInk{}, errors.Wrap(err, "the graph cannot be drawn in the palette")
		}
		*c.into = v
	}
	return g, nil
}

// canvasCell is canvas.css's grid: a line on the last pixel of every 24.
const canvasCell = 24

// drawPercentGraph draws hourly percentages on 0 to 100 over QNTX's canvas: a
// line with the area under it shaded, a rule at every quarter, and one at every
// UTC midnight. An hour with no sample is a gap, not a fall to zero.
func drawPercentGraph(points []sentryread.Point) ([]byte, error) {
	ink, err := graphInkOf()
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, graphWidth, graphHeight))
	for y := 0; y < graphHeight; y++ {
		for x := 0; x < graphWidth; x++ {
			c := ink.background
			if x%canvasCell == canvasCell-1 || y%canvasCell == canvasCell-1 {
				c = ink.grid
			}
			img.Set(x, y, c)
		}
	}
	for _, q := range []float64{25, 50, 75} {
		y := yOf(q)
		for x := 0; x < graphWidth; x++ {
			img.Set(x, y, ink.rule)
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
				img.Set(x, y, ink.rule)
			}
		}
	}

	// The shade is translucent, as the sigma element's is: the grid shows
	// through it. A column two hours share is shaded once, or it would be
	// darker than its neighbours.
	shade := &image.Uniform{C: ink.shade}
	shaded := -1
	for i := 0; i+1 < n; i++ {
		a, b := points[i], points[i+1]
		if a.Value <= 0 || b.Value <= 0 {
			continue
		}
		x0, x1 := xOf(i), xOf(i+1)
		for x := max(x0, shaded+1); x <= x1; x++ {
			t := 0.0
			if x1 > x0 {
				t = float64(x-x0) / float64(x1-x0)
			}
			v := a.Value + (b.Value-a.Value)*t
			draw.Draw(img, image.Rect(x, yOf(v)+1, x+1, graphHeight), shade, image.Point{}, draw.Over)
			shaded = x
		}
		drawLine(img, x0, yOf(a.Value), x1, yOf(b.Value), ink.line)
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
