package otlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/internal/measure"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] receiving is an OTLP/HTTP metrics receiver standing in for a node's ix/otel: what it was sent, by metric name.」
type receiving struct {
	mu      sync.Mutex
	metrics map[string]*metricspb.Metric
	paths   []string
}

func (got *receiving) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	export := &colmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(body, export); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	got.paths = append(got.paths, r.URL.Path)
	for _, resource := range export.GetResourceMetrics() {
		for _, scope := range resource.GetScopeMetrics() {
			for _, metric := range scope.GetMetrics() {
				got.metrics[metric.GetName()] = metric
			}
		}
	}
	answer, _ := proto.Marshal(&colmetricspb.ExportMetricsServiceResponse{})
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(answer)
}

// 「[Opus 5.5] The numbers the node measures leave under the names they have when the environment names where.」
func TestTheNodesNumbersLeaveAsOTLPMetrics(t *testing.T) {
	got := &receiving{metrics: map[string]*metricspb.Metric{}}
	receiver := httptest.NewServer(got)
	defer receiver.Close()
	t.Setenv(Endpoint, receiver.URL+"/ix/otel")
	t.Setenv(TracesEndpoint, "")
	t.Setenv(MetricsEndpoint, "")

	if err := StartMetrics(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if !MetricsRunning() {
		t.Fatal("an endpoint was named and no metrics are leaving")
	}
	measure.Count(measure.AttestationsWritten, 2, measure.String(measure.AttrOutcome, "taken"))
	measure.Gauge(measure.QueueDepth, 3681)
	measure.Took(measure.QueryTook, 15*time.Millisecond)
	measure.Took(measure.QueryTook, 250*time.Microsecond)
	if err := Flush(); err != nil {
		t.Fatal(err)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if len(got.paths) == 0 || got.paths[0] != "/ix/otel/v1/metrics" {
		t.Fatalf("exported to %v, want the base endpoint's v1/metrics", got.paths)
	}
	written := got.metrics[measure.AttestationsWritten].GetSum().GetDataPoints()
	if got.metrics[measure.AttestationsWritten].GetSum().GetAggregationTemporality() != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
		t.Fatalf("%s is sent cumulative, so a total that does not move is sent again every interval", measure.AttestationsWritten)
	}
	if len(written) != 1 || written[0].GetAsInt() != 2 || written[0].GetAttributes()[0].GetValue().GetStringValue() != "taken" {
		t.Fatalf("%s arrived as %v", measure.AttestationsWritten, got.metrics[measure.AttestationsWritten])
	}
	depth := got.metrics[measure.QueueDepth].GetGauge().GetDataPoints()
	if len(depth) != 1 || depth[0].GetAsDouble() != 3681 {
		t.Fatalf("%s arrived as %v", measure.QueueDepth, got.metrics[measure.QueueDepth])
	}
	took := got.metrics[measure.QueryTook]
	// What takes less than a millisecond is kept as the part of one it took.
	if took.GetUnit() != "ms" || len(took.GetHistogram().GetDataPoints()) != 1 || took.GetHistogram().GetDataPoints()[0].GetSum() != 15.25 {
		t.Fatalf("%s arrived as %v", measure.QueryTook, took)
	}
}

// 「[Opus 5.5] No endpoint named starts nothing, and a number measured then goes nowhere.」
func TestNoEndpointSendsNoMetrics(t *testing.T) {
	t.Setenv(Endpoint, "")
	t.Setenv(MetricsEndpoint, "")
	if err := StartMetrics(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if MetricsRunning() {
		t.Fatal("metrics are leaving with no endpoint named")
	}
	measure.Count(measure.AttestationsWritten, 1)
}

// 「[Opus 5.5] OTEL_METRICS_EXPORTER=none starts no metrics, so the one endpoint pointed at Sentry, which takes no OTLP metrics, carries spans alone.」
func TestMetricsExporterNoneSendsNoMetrics(t *testing.T) {
	t.Setenv(Endpoint, "https://o1.ingest.sentry.io/api/2/integration/otlp")
	t.Setenv(MetricsEndpoint, "")
	t.Setenv(MetricsExporter, "none")
	if _, said := MetricsSaid(); said {
		t.Fatal("an endpoint is said for metrics while OTEL_METRICS_EXPORTER is none")
	}
	if err := StartMetrics(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if MetricsRunning() {
		t.Fatal("metrics are leaving while OTEL_METRICS_EXPORTER is none")
	}
}

// An exporter other than OTLP named alone sends nothing over OTLP; OTLP among
// several, or named in another case, does.
func TestOnlyAnOTLPExporterSendsMetrics(t *testing.T) {
	t.Setenv(Endpoint, "http://127.0.0.1:1/ix/otel")
	t.Setenv(MetricsEndpoint, "")
	for named, sends := range map[string]bool{"prometheus": false, "console": false, "None": false, "": true, "OTLP": true, "prometheus, otlp": true} {
		t.Setenv(MetricsExporter, named)
		if _, said := MetricsSaid(); said != sends {
			t.Errorf("OTEL_METRICS_EXPORTER=%q: metrics said %t, want %t", named, said, sends)
		}
	}
}
