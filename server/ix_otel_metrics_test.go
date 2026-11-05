package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] ixOtelMetricsAt is when the numbers below were read.」
var ixOtelMetricsAt = time.Date(2026, 11, 5, 11, 0, 0, 0, time.UTC)

// 「[Opus 5.5] twoNumbers is an export of two of the node's own numbers, as one node sends another: Pulse's queue depth and attestations written.」
func twoNumbers() *colmetricspb.ExportMetricsServiceRequest {
	at := uint64(ixOtelMetricsAt.UnixNano())
	return &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
			Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "qntx"}},
		}}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Scope: &commonpb.InstrumentationScope{Name: "github.com/teranos/QNTX/internal/measure"},
			Metrics: []*metricspb.Metric{
				{Name: "qntx.pulse.queue.depth", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{{
					TimeUnixNano: at, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 3681},
				}}}}},
				{Name: "qntx.attestations.written", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
					AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, IsMonotonic: true,
					DataPoints: []*metricspb.NumberDataPoint{{TimeUnixNano: at, Value: &metricspb.NumberDataPoint_AsInt{AsInt: 12}}},
				}}},
			},
		}},
	}}}
}

// 「[Opus 5.5] metricsAnswered is the ExportMetricsServiceResponse a 200 carries, read as protobuf.」
func metricsAnswered(t *testing.T, code int, body []byte) *colmetricspb.ExportMetricsServiceResponse {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %q", code, body)
	}
	said := &colmetricspb.ExportMetricsServiceResponse{}
	if err := proto.Unmarshal(body, said); err != nil {
		t.Fatalf("the answer is no ExportMetricsServiceResponse: %v", err)
	}
	return said
}

// 「[Opus 5.5] Each number lands in the namespace its sender's token acts in, by that token, at the moment it was read, to be asked for again.」
func TestANumberLandsInTheNamespaceItsTokenActsIn(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	rec := exporting(s, pondToken("*"), ixOtelMetrics, "application/x-protobuf", "", protobufOf(t, twoNumbers()))
	if said := metricsAnswered(t, rec.Code, rec.Body.Bytes()); said.GetPartialSuccess() != nil {
		t.Fatalf("numbers fully taken were answered with partial_success %v", said.GetPartialSuccess())
	}

	kept := everythingIn(t, markets["pond"])
	if len(kept) != 2 {
		t.Fatalf("pond holds %d attestations, want the two numbers", len(kept))
	}
	byName := map[string]int{}
	for i, number := range kept {
		byName[number.Predicates[0]] = i
		if !slices.Equal(number.Subjects, []string{"qntx"}) || !slices.Equal(number.Actors, []string{tokenDID}) || !number.Timestamp.Equal(ixOtelMetricsAt) {
			t.Fatalf("kept %v %v by %v at %s, want qntx's number by the token at the moment it was read", number.Subjects, number.Predicates, number.Actors, number.Timestamp)
		}
	}
	depth := kept[byName["qntx.pulse.queue.depth"]]
	if !slices.Equal(depth.Contexts, []string{"metric:gauge"}) {
		t.Fatalf("contexts = %v, want the kind of number", depth.Contexts)
	}
	gauge, isMap := depth.Attributes["metric"].(map[string]any)
	if !isMap || gauge["name"] != "qntx.pulse.queue.depth" || !strings.Contains(asJSON(t, gauge), "3681") {
		t.Fatalf("the number is not kept whole as OTLP/JSON: %v", depth.Attributes["metric"])
	}
	written := kept[byName["qntx.attestations.written"]]
	if !strings.Contains(asJSON(t, written.Attributes["metric"]), `"asInt":"12"`) || !slices.Equal(written.Contexts, []string{"metric:sum"}) {
		t.Fatalf("attestations written is kept as %v in %v", written.Attributes["metric"], written.Contexts)
	}
}

// 「[Opus 5.5] A sender retries: the same number at the same moment is kept once.」
func TestANumberSentTwiceIsKeptOnce(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	body := protobufOf(t, twoNumbers())
	for range 2 {
		rec := exporting(s, pondToken("*"), ixOtelMetrics, "application/x-protobuf", "", body)
		metricsAnswered(t, rec.Code, rec.Body.Bytes())
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 2 {
		t.Fatalf("pond holds %d attestations after the same two numbers came twice, want 2", len(kept))
	}
}

// 「[Opus 5.5] A token writes the numbers its role's WRITE lines say, and the rest are counted as rejected data points.」
func TestANumberItsTokenMayNotWriteIsRejected(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	rec := exporting(s, pondToken("qntx.pulse.queue.depth"), ixOtelMetrics, "application/x-protobuf", "", protobufOf(t, twoNumbers()))
	said := metricsAnswered(t, rec.Code, rec.Body.Bytes())

	if said.GetPartialSuccess().GetRejectedDataPoints() != 1 || !strings.Contains(said.GetPartialSuccess().GetErrorMessage(), "qntx.attestations.written") {
		t.Fatalf("partial_success = %v, want one rejected data point naming its metric", said.GetPartialSuccess())
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 1 || kept[0].Predicates[0] != "qntx.pulse.queue.depth" {
		t.Fatalf("pond holds %v, want the queue depth alone", kept)
	}
}

// 「[Opus 5.5] Numbers are held to the bound spans are held to.」
func TestNumbersPastTheBoundAre413(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	refusedWith(t, exporting(s, pondToken("*"), ixOtelMetrics, "application/x-protobuf", "", make([]byte, tenantBodyMost+1)), http.StatusRequestEntityTooLarge)
}

// 「[Opus 5.5] Numbers sent as OTLP/JSON are read and answered in JSON.」
func TestNumbersSentAsJSONAreRead(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	body := `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"qntx"}}]},` +
		`"scopeMetrics":[{"metrics":[{"name":"qntx.pulse.workers.active","gauge":{"dataPoints":[{"timeUnixNano":"1793876400000000000","asDouble":0.31}]}}]}]}]}`
	rec := exporting(s, pondToken("*"), ixOtelMetrics, "application/json", "", []byte(body))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, Content-Type %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 1 || kept[0].Predicates[0] != "qntx.pulse.workers.active" {
		t.Fatalf("pond holds %v, want workers active", kept)
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	said, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(said)
}
