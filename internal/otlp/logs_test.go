package otlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	errors "github.com/teranos/sacred-error"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// "I WANT QNTX TO SPEAK OTEL"
//
// "I WANT OTEL TO BE THE THING WE SETTLE ON"
// "FOR OBSERVABILITY"
//
// "THE LANGUAGE OF OBSERVABILITY NEEDS TO BE OTEL"

// 「[Opus 5.5] hearing is an OTLP/HTTP logs receiver: each record it was sent, with the scope it came under.」
type hearing struct {
	mu      sync.Mutex
	records []*logspb.LogRecord
	scopes  []string
	paths   []string
}

func (got *hearing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	export := &collogspb.ExportLogsServiceRequest{}
	if err := proto.Unmarshal(body, export); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	got.paths = append(got.paths, r.URL.Path)
	for _, resource := range export.GetResourceLogs() {
		for _, scope := range resource.GetScopeLogs() {
			for _, record := range scope.GetLogRecords() {
				got.records = append(got.records, record)
				got.scopes = append(got.scopes, scope.GetScope().GetName())
			}
		}
	}
	answer, _ := proto.Marshal(&collogspb.ExportLogsServiceResponse{})
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(answer)
}

func attributeOf(record *logspb.LogRecord, key string) string {
	for _, held := range record.GetAttributes() {
		if held.GetKey() == key {
			return held.GetValue().GetStringValue()
		}
	}
	return ""
}

// 「[Opus 5.5] What the node logs leaves as OTLP logs when the environment names where: its message,」
// 「[Opus 5.5] its level, its fields, the logger it was named for, and an error as the exception it is.」
func TestTheNodesLogsLeaveAsOTLPLogs(t *testing.T) {
	got := &hearing{}
	receiver := httptest.NewServer(got)
	defer receiver.Close()
	t.Setenv(Endpoint, receiver.URL)
	t.Setenv(LogsEndpoint, "")
	t.Setenv(LogsExporter, "")

	core, err := StartLogs(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if core == nil || !LogsRunning() {
		t.Fatal("an endpoint was named and no logs are leaving")
	}
	log := zap.New(core).Named("server.forward").Sugar()
	// A Debug line stays on the box: only Info and above leave.
	log.Debugw("compared one attestation", "id", "AS-1")
	log.Errorw("A delivery did not reach the node it is forwarded to",
		"url", "http://10.88.0.2:8770/github/webhook", "error", errors.New("dial tcp 10.88.0.2:8770: i/o timeout"))
	if err := Flush(); err != nil {
		t.Fatal(err)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if len(got.paths) == 0 || got.paths[0] != "/v1/logs" {
		t.Fatalf("exported to %v, want the base endpoint's v1/logs", got.paths)
	}
	if len(got.records) != 1 {
		t.Fatalf("%d records arrived, want the one Error logged and not the Debug line", len(got.records))
	}
	record := got.records[0]
	if record.GetBody().GetStringValue() != "A delivery did not reach the node it is forwarded to" {
		t.Fatalf("the record's body is %v", record.GetBody())
	}
	if record.GetSeverityNumber() != logspb.SeverityNumber_SEVERITY_NUMBER_ERROR {
		t.Fatalf("the record's severity is %v, want ERROR", record.GetSeverityNumber())
	}
	if got.scopes[0] != "server.forward" {
		t.Fatalf("the record came under scope %q, want the logger's name", got.scopes[0])
	}
	if attributeOf(record, "url") != "http://10.88.0.2:8770/github/webhook" {
		t.Fatalf("the url field did not arrive: %v", record.GetAttributes())
	}
	if attributeOf(record, "exception.message") != "dial tcp 10.88.0.2:8770: i/o timeout" {
		t.Fatalf("the error did not arrive as the exception: %v", record.GetAttributes())
	}
}

// 「[Opus 5.5] No endpoint named starts nothing.」
func TestNoEndpointSendsNoLogs(t *testing.T) {
	t.Setenv(Endpoint, "")
	t.Setenv(LogsEndpoint, "")
	core, err := StartLogs(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if core != nil || LogsRunning() {
		t.Fatal("logs are leaving with no endpoint named")
	}
}

// 「[Opus 5.5] OTEL_LOGS_EXPORTER naming no otlp sends no logs over OTLP, whatever endpoint is named.」
func TestLogsExporterNoneSendsNoLogs(t *testing.T) {
	t.Setenv(Endpoint, "http://127.0.0.1:1")
	t.Setenv(LogsExporter, "none")
	core, err := StartLogs(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if core != nil || LogsRunning() {
		t.Fatal("logs are leaving while OTEL_LOGS_EXPORTER is none")
	}
}
