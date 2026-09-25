package sentryread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var week = Window{
	Start: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	End:   time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
}

func sentryAnswering(t *testing.T, body string, asked *http.Request) Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*asked = *r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return Client{API: srv.URL, Organization: "abcd", Project: "1234", Token: "sntryu_read"}
}

func TestATableIsAskedOfTheOrganizationAndReadAsRows(t *testing.T) {
	var asked http.Request
	c := sentryAnswering(t, `{"data":[{"path":"/","tags[status,number]":404,"count()":706}],"meta":{}}`, &asked)

	rows, err := c.Table(context.Background(), TableQuery{
		Dataset: DatasetLogs,
		Fields:  []string{"path", "tags[status,number]", "count()"},
		Query:   `message:"http *" tags[status,number]:>=400 tags[status,number]:<500`,
		Sort:    "-count()",
		Limit:   3,
		Window:  week,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "/", rows[0]["path"])
	assert.InDelta(t, 706.0, rows[0]["count()"], 0)

	assert.Equal(t, "/api/0/organizations/abcd/events/", asked.URL.Path)
	assert.Equal(t, "Bearer sntryu_read", asked.Header.Get("Authorization"))
	q := asked.URL.Query()
	assert.Equal(t, DatasetLogs, q.Get("dataset"))
	assert.Equal(t, []string{"path", "tags[status,number]", "count()"}, q["field"])
	assert.Equal(t, "1234", q.Get("project"))
	assert.Equal(t, "2026-09-18T00:00:00Z", q.Get("start"))
	assert.Equal(t, "2026-09-25T00:00:00Z", q.Get("end"))
	assert.Equal(t, "3", q.Get("per_page"))
}

func TestASeriesIsReadAsOneValuePerBucket(t *testing.T) {
	var asked http.Request
	c := sentryAnswering(t, `{"data":[[1790208000,[{"count":13.668}]],[1790211600,[{"count":null}]],[1790215200,[]]]}`, &asked)

	points, err := c.Series(context.Background(), SeriesQuery{
		Dataset:  DatasetMetrics,
		YAxis:    "avg(value,qntx.host.cpu,gauge,-)",
		Query:    "metric.name:qntx.host.cpu metric.type:gauge",
		Interval: "1h",
		Window:   week,
	})
	require.NoError(t, err)
	require.Len(t, points, 3)
	assert.Equal(t, time.Unix(1790208000, 0).UTC(), points[0].At)
	assert.InDelta(t, 13.668, points[0].Value, 1e-9)
	assert.Zero(t, points[1].Value, "a bucket Sentry holds nothing for is zero")
	assert.Zero(t, points[2].Value)

	assert.Equal(t, "/api/0/organizations/abcd/events-stats/", asked.URL.Path)
	assert.Equal(t, "avg(value,qntx.host.cpu,gauge,-)", asked.URL.Query().Get("yAxis"))
	assert.Equal(t, "1h", asked.URL.Query().Get("interval"))
}

// A refusal carries what Sentry said: it is the only place the reason is.
func TestSentryRefusingIsSaidWithItsOwnWords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"You do not have permission to perform this action."}`))
	}))
	t.Cleanup(srv.Close)
	c := Client{API: srv.URL, Organization: "abcd", Project: "1", Token: "wrong"}

	_, err := c.Table(context.Background(), TableQuery{Dataset: DatasetSpans, Fields: []string{"count()"}, Window: week})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "You do not have permission")
}

// Nothing is asked without the four things a question needs, and the refusal
// names which are missing.
func TestSentryIsNotAskedWithoutAToken(t *testing.T) {
	c := Client{API: "https://de.sentry.io", Organization: "abcd", Project: "1"}
	_, err := c.Series(context.Background(), SeriesQuery{Dataset: DatasetMetrics, YAxis: "count()", Interval: "1h", Window: week})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sentry.read_token")
}
