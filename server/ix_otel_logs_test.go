package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] ixOtelLogsAt is when the records below happened.」
var ixOtelLogsAt = time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)

// 「[Opus 5.5] twoRecords is an export of two of Claude Code's events, one inside a span and one not.」
func twoRecords() *collogspb.ExportLogsServiceRequest {
	at := uint64(ixOtelLogsAt.UnixNano())
	return &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
			Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-code"}},
		}}},
		ScopeLogs: []*logspb.ScopeLogs{{
			Scope: &commonpb.InstrumentationScope{Name: "com.anthropic.claude_code.events"},
			LogRecords: []*logspb.LogRecord{
				{TimeUnixNano: at, EventName: "claude_code.api_request", SeverityText: "INFO", SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
					TraceId: []byte("0123456789abcdef"), SpanId: []byte("01234567"),
					Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude_code.api_request"}}},
				{TimeUnixNano: at, SeverityText: "ERROR", SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
					Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "the tool did not answer"}}},
			},
		}},
	}}}
}

// 「[Opus 5.5] logsAnswered is the ExportLogsServiceResponse a 200 carries, read as protobuf.」
func logsAnswered(t *testing.T, code int, body []byte) *collogspb.ExportLogsServiceResponse {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %q", code, body)
	}
	said := &collogspb.ExportLogsServiceResponse{}
	if err := proto.Unmarshal(body, said); err != nil {
		t.Fatalf("the answer is no ExportLogsServiceResponse: %v", err)
	}
	return said
}

// Each log record lands in the namespace the one the gate admitted acts in, by that one, at the moment it happened.
// 「[Opus 5.5] Its event name is what it says happened; one with none is a log, and one inside a span names its trace and span.」
func TestALogRecordLandsInTheNamespaceItsSenderActsIn(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	rec := exporting(s, pondToken("*"), ixOtelLogs, "application/x-protobuf", "", protobufOf(t, twoRecords()))
	if said := logsAnswered(t, rec.Code, rec.Body.Bytes()); said.GetPartialSuccess() != nil {
		t.Fatalf("records fully taken were answered with partial_success %v", said.GetPartialSuccess())
	}

	kept := everythingIn(t, markets["pond"])
	if len(kept) != 2 {
		t.Fatalf("pond holds %d attestations, want the two records", len(kept))
	}
	byName := map[string]int{}
	for i, record := range kept {
		byName[record.Predicates[0]] = i
		if !slices.Equal(record.Subjects, []string{"claude-code"}) || !slices.Equal(record.Actors, []string{tokenDID}) || !record.Timestamp.Equal(ixOtelLogsAt) {
			t.Fatalf("kept %v %v by %v at %s, want claude-code's record by its sender at the moment it happened", record.Subjects, record.Predicates, record.Actors, record.Timestamp)
		}
	}
	request := kept[byName["claude_code.api_request"]]
	if !slices.Equal(request.Contexts, []string{"log:INFO", "trace:30313233343536373839616263646566", "span:3031323334353637"}) {
		t.Fatalf("contexts = %v, want the severity, the trace and the span", request.Contexts)
	}
	logged, isLog := byName[otlpUnnamedLog]
	if !isLog {
		t.Fatalf("the record with no event name is kept as %v, want %q", kept, otlpUnnamedLog)
	}
	failed := kept[logged]
	if !slices.Equal(failed.Contexts, []string{"log:ERROR"}) || !strings.Contains(asJSON(t, failed.Attributes["log"]), "the tool did not answer") {
		t.Fatalf("the error record is kept as %v in %v", failed.Attributes["log"], failed.Contexts)
	}
}

// 「[Opus 5.5] A sender retries: the same record is kept once.」
func TestALogRecordSentTwiceIsKeptOnce(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	body := protobufOf(t, twoRecords())
	for range 2 {
		rec := exporting(s, pondToken("*"), ixOtelLogs, "application/x-protobuf", "", body)
		logsAnswered(t, rec.Code, rec.Body.Bytes())
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 2 {
		t.Fatalf("pond holds %d attestations after the same two records came twice, want 2", len(kept))
	}
}

// A sender writes the records its role's WRITE lines say, and the rest are counted as rejected records.
func TestALogRecordItsSenderMayNotWriteIsRejected(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	rec := exporting(s, pondToken("claude_code.api_request"), ixOtelLogs, "application/x-protobuf", "", protobufOf(t, twoRecords()))
	said := logsAnswered(t, rec.Code, rec.Body.Bytes())
	if said.GetPartialSuccess().GetRejectedLogRecords() != 1 || !strings.Contains(said.GetPartialSuccess().GetErrorMessage(), otlpUnnamedLog) {
		t.Fatalf("partial_success = %v, want one rejected record naming what it is", said.GetPartialSuccess())
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 1 || kept[0].Predicates[0] != "claude_code.api_request" {
		t.Fatalf("pond holds %v, want the api request alone", kept)
	}
}

// An all-zero trace or span id is OTLP's none: it ties the record to nothing,
// and no trace: or span: context is kept for it.
func TestALogRecordWithZeroIdsNamesNoTrace(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	export := twoRecords()
	for _, record := range export.ResourceLogs[0].ScopeLogs[0].LogRecords {
		record.TraceId, record.SpanId = make([]byte, 16), make([]byte, 8)
	}
	rec := exporting(s, pondToken("*"), ixOtelLogs, "application/x-protobuf", "", protobufOf(t, export))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, kept := range everythingIn(t, markets["pond"]) {
		for _, context := range kept.Contexts {
			assert.False(t, strings.HasPrefix(context, "trace:") || strings.HasPrefix(context, "span:"), "a zero id was kept as %s", context)
		}
	}
}
