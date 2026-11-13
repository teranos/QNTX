package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sentryread"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
)

// "ADR-025 is DEAD ok?"
//
// "And the node gives Agents access to the MCP of Sentry"

// askedSentry is the Sentry MCP server as the report asks it: every call kept, each answered by answer.
type askedSentry struct {
	calls  []map[string]any
	tools  []string
	answer func(tool string, arguments map[string]any) string
}

func (a *askedSentry) call(_ context.Context, tool string, arguments map[string]any) (json.RawMessage, error) {
	a.tools = append(a.tools, tool)
	a.calls = append(a.calls, arguments)
	return json.RawMessage(a.answer(tool, arguments)), nil
}

func sentryAt(t *testing.T, asked *askedSentry) sentryAsker {
	t.Helper()
	sentry, err := sentryOver(appcfg.SentryConfig{API: "https://de.sentry.io", Organization: "sbvh", Project: "qntx-go"}, asked.call)
	require.NoError(t, err)
	return sentry
}

// A table is asked with its fields named exactly, of the node's organization, region and project, over the
// window back from now; the rows are what Sentry's search_metrics answered (as it did on 2026-11-13).
func TestATableIsAskedOfTheNodesSentryMCPWithItsFieldsExactly(t *testing.T) {
	asked := &askedSentry{answer: func(string, map[string]any) string {
		return `{"resultType":"aggregates","results":[{"values":{"avg(value,qntx.host.net.in,gauge,-)":122886.49952453635,"count(value,qntx.host.net.in,gauge,-)":9913},"links":{"traceUrl":null}}]}`
	}}
	avg, count := "avg(value,qntx.host.net.in,gauge,-)", "count(value,qntx.host.net.in,gauge,-)"
	rows, err := sentryAt(t, asked).Table(context.Background(), sentryread.TableQuery{
		Dataset: sentryread.DatasetMetrics, Fields: []string{avg, count}, Query: "metric.name:qntx.host.net.in",
		Window: sentryread.Window{Start: reportEnd.Add(-reportWeek), End: reportEnd},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 9913.0, rows[0][count])

	require.Equal(t, []string{"search_metrics"}, asked.tools)
	assert.Equal(t, map[string]any{
		"organizationSlug": "sbvh", "regionUrl": "https://de.sentry.io", "projectSlug": "qntx-go",
		"query": "metric.name:qntx.host.net.in", "fields": []string{avg, count}, "period": "168h",
	}, asked.calls[0])
}

// Logs are asked of search_logs, ranked and cut as the report asks.
func TestLogsAreAskedOfSearchLogs(t *testing.T) {
	asked := &askedSentry{answer: func(string, map[string]any) string { return `{"results":[]}` }}
	_, err := sentryAt(t, asked).Table(context.Background(), sentryread.TableQuery{
		Dataset: sentryread.DatasetLogs, Fields: []string{"message", "count()"}, Query: "severity:error", Sort: "-count()", Limit: 3,
		Window: sentryread.Window{Start: reportEnd.Add(-reportWeek), End: reportEnd},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"search_logs"}, asked.tools)
	assert.Equal(t, "-count()", asked.calls[0]["sort"])
	assert.Equal(t, 3, asked.calls[0]["limit"])
}

// Sentry's MCP takes a period back from now and no bucket: a day is what the last day holds, the day before what the
// last two days hold beyond it, and an average is told apart by its count.
func TestASeriesIsEachPeriodLessTheOneBeforeIt(t *testing.T) {
	// Day 1 back: 1440 samples averaging 40. Day 2 back: 1380 more, averaging 50. Day 3 back: nothing.
	held := map[string][2]float64{"24h": {40, 1440}, "48h": {(40*1440 + 50*1380) / 2820.0, 2820}, "72h": {(40*1440 + 50*1380) / 2820.0, 2820}}
	asked := &askedSentry{answer: func(_ string, arguments map[string]any) string {
		h := held[arguments["period"].(string)]
		return fmt.Sprintf(`{"results":[{"values":{"avg(value,qntx.host.cpu,gauge,-)":%v,"count(value,qntx.host.cpu,gauge,-)":%v}}]}`, h[0], h[1])
	}}
	points, err := sentryAt(t, asked).Series(context.Background(), sentryread.SeriesQuery{
		Dataset: sentryread.DatasetMetrics, YAxis: "avg(value,qntx.host.cpu,gauge,-)", Query: "metric.name:qntx.host.cpu",
		Bucket: reportBucket, Window: sentryread.Window{Start: reportEnd.Add(-3 * reportBucket), End: reportEnd},
	})
	require.NoError(t, err)
	require.Len(t, points, 3)
	assert.Equal(t, reportEnd.Add(-3*reportBucket), points[0].At, "oldest first")
	assert.Equal(t, 0.0, points[0].Value, "a day that holds nothing is a gap")
	assert.InDelta(t, 50, points[1].Value, 1e-9)
	assert.InDelta(t, 40, points[2].Value, 1e-9)
	for _, c := range asked.calls {
		assert.Equal(t, []string{"avg(value,qntx.host.cpu,gauge,-)", "count(value,qntx.host.cpu,gauge,-)"}, c["fields"])
	}
}

// Downtime is the minutes a day held no CPU sample, from the counts the series answers.
func TestDowntimeIsCountedADayAtATimeThroughTheMCP(t *testing.T) {
	// Up all of the last 20 days but 5 minutes yesterday and 30 ten days back; nothing before.
	asked := &askedSentry{answer: func(_ string, arguments map[string]any) string {
		var hours int
		_, err := fmt.Sscanf(arguments["period"].(string), "%dh", &hours)
		require.NoError(t, err)
		days := hours / 24
		count := 0
		for d := 1; d <= days && d <= 20; d++ {
			count += 1440
			switch d {
			case 2:
				count -= 5
			case 10:
				count -= 30
			}
		}
		return fmt.Sprintf(`{"results":[{"values":{"count(value,qntx.host.cpu,gauge,-)":%d}}]}`, count)
	}}
	d := downtimeUpTo(context.Background(), sentryAt(t, asked), "", reportEnd)
	assert.Empty(t, d.Err)
	assert.Equal(t, 5, d.WeekMinutes)
	assert.Equal(t, 35, d.ThreeWeeksMinutes)
	assert.Equal(t, reportEnd.Add(-20*reportBucket), d.Since)
	assert.Len(t, asked.calls, 21)
}

// What Sentry took and dropped of the week's error events is summed per outcome and reason, as find_dropped_events
// answered it on 2026-11-13.
func TestOutcomesAreWhatFindDroppedEventsSaid(t *testing.T) {
	asked := &askedSentry{answer: func(string, map[string]any) string {
		return `{"dataset":"errors","droppedEvents":[
			{"outcome":"rate_limited","reason":"error_usage_exceeded","count":12},
			{"outcome":"client_discard","reason":"ratelimit_backoff","count":225},
			{"outcome":"rate_limited","reason":"error_usage_exceeded","count":121}],
			"acceptedEvents":[{"outcome":"accepted","reason":"accepted","count":3474},{"outcome":"accepted","reason":"accepted","count":331}]}`
	}}
	w := sentryread.Window{Start: reportEnd.Add(-reportWeek), End: reportEnd}
	outcomes, err := sentryAt(t, asked).Outcomes(context.Background(), sentryread.CategoryError, w)
	require.NoError(t, err)
	assert.Equal(t, []sentryread.Outcome{
		{Outcome: "accepted", Count: 3805},
		{Outcome: "rate_limited", Reason: "error_usage_exceeded", Count: 133},
		{Outcome: "client_discard", Reason: "ratelimit_backoff", Count: 225},
	}, outcomes)
	assert.Equal(t, []string{"find_dropped_events"}, asked.tools)
	assert.Equal(t, "errors", asked.calls[0]["dataset"])
	assert.Equal(t, "2026-09-18T10:00:00Z", asked.calls[0]["start"])
	assert.Equal(t, "2026-09-25T10:00:00Z", asked.calls[0]["end"])
}

// A tool that failed is said in its own words, once, where its section would have been.
func TestASentryToolThatFailedIsSaidInItsOwnWords(t *testing.T) {
	_, err := sentryAnswer("search_metrics", &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Organization sbvh not found"}}})
	require.Error(t, err)
	assert.Equal(t, "Sentry's search_metrics answered: Organization sbvh not found", err.Error())

	said, err := sentryAnswer("search_metrics", &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"results":[]}`}}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"results":[]}`, string(said))
}

// "make the report also get delivered to the Node Room"
//
// The report is said in the Node Room by the node, under what the mail is called, with the report whole as text and
// its graphs beside it.
func TestTheReportIsSaidInTheNodeRoom(t *testing.T) {
	rooms := &commentsRoom{t: t, list: []*protocol.Room{{Id: "telegram-chat", Name: "Elsewhere"}, {Id: "3479956d", Name: nodeRoomName}}}
	s := rootKnowingServer(t)
	mail, err := renderReport(s.gatherReport(context.Background(), reportEnd, answeringSentry{}, nil))
	require.NoError(t, err)

	require.NoError(t, sayReportIn(context.Background(), rooms, "switchai-py", "did:key:z6Mkgardennode", mail))
	require.Len(t, rooms.said, 1)
	said := rooms.said[0]
	assert.Equal(t, "3479956d", said.GetRoom())
	assert.Equal(t, "switchai-py", said.GetProvider())
	assert.Equal(t, "did:key:z6Mkgardennode", said.GetBy().GetDid())
	assert.Equal(t, mail.Subject, said.GetBody())

	require.Len(t, said.GetFiles(), 1+len(mail.Inline))
	text := said.GetFiles()[0]
	assert.Equal(t, strings.ReplaceAll(mail.Subject, " ", "-")+".txt", text.GetName())
	assert.Equal(t, nodeBody(t, mail, services.MailText), string(text.GetData()))
	for i, img := range mail.Inline {
		assert.Equal(t, img.FileName, said.GetFiles()[1+i].GetName())
		assert.Equal(t, img.Data, said.GetFiles()[1+i].GetData())
	}
}

// A provider that holds no Node Room is the report's failure, said, and nothing is posted elsewhere.
func TestNoNodeRoomIsSaid(t *testing.T) {
	rooms := &commentsRoom{t: t, list: []*protocol.Room{{Id: "telegram-chat", Name: "Elsewhere"}}}
	err := sayReportIn(context.Background(), rooms, "switchai-py", "did:key:z6Mkgardennode", services.NodeMail{Subject: "QNTX week"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the provider holds no Node Room")
	assert.Empty(t, rooms.said)
}
