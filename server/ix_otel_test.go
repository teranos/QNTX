package server

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] The trace and span a sender names, as the W3C ids OTLP carries.」
const (
	sentTrace = "5b8efff798038103d269b633813fc60c"
	sentSpan  = "eee19b7ec3c1b174"
)

// 「[Opus 5.5] A token acting in pond, whose role writes the words given.」
func pondToken(writes ...string) auth.Admission {
	admitted := auth.Admitted(auth.LevelToken, "pond")
	admitted.Grant = &auth.Grant{DID: tokenDID, Level: auth.LevelToken}
	return auth.Saying(admitted, auth.Words{Write: writes, Read: writes})
}

// 「[Opus 5.5] oneSpan is an export of one span from claude-code, named as Claude Code names its tool span.」
func oneSpan(t *testing.T) *coltracepb.ExportTraceServiceRequest {
	t.Helper()
	trace, err := hex.DecodeString(sentTrace)
	if err != nil {
		t.Fatal(err)
	}
	span, err := hex.DecodeString(sentSpan)
	if err != nil {
		t.Fatal(err)
	}
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
			Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-code"}},
		}}},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "com.anthropic.claude_code.tracing"},
			Spans: []*tracepb.Span{{
				TraceId: trace, SpanId: span, Name: "claude_code.tool",
				Kind:              tracepb.Span_SPAN_KIND_INTERNAL,
				StartTimeUnixNano: uint64(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC).UnixNano()),
				EndTimeUnixNano:   uint64(time.Date(2026, 11, 5, 10, 0, 1, 0, time.UTC).UnixNano()),
				Attributes: []*commonpb.KeyValue{{
					Key: "tool_name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "Bash"}},
				}},
			}},
		}},
	}}}
}

func protobufOf(t *testing.T, m proto.Message) []byte {
	t.Helper()
	body, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// 「[Opus 5.5] exporting posts one body to path as the caller, the way an OTLP/HTTP exporter does.」
func exporting(s *QNTXServer, caller auth.Admission, path, contentType, encoding string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	req = req.WithContext(auth.WithAdmission(req.Context(), caller))
	rec := httptest.NewRecorder()
	s.HandleIxOtel(rec, req)
	return rec
}

func everythingIn(t *testing.T, store ats.AttestationStore) []*types.As {
	t.Helper()
	found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: 100})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	return found
}

// 「[Opus 5.5] answered is the ExportTraceServiceResponse a 200 carries, read as protobuf.」
func answered(t *testing.T, rec *httptest.ResponseRecorder) *coltracepb.ExportTraceServiceResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-protobuf" {
		t.Fatalf("Content-Type = %q, want the request's own, application/x-protobuf", got)
	}
	said := &coltracepb.ExportTraceServiceResponse{}
	if err := proto.Unmarshal(rec.Body.Bytes(), said); err != nil {
		t.Fatalf("the answer is no ExportTraceServiceResponse: %v", err)
	}
	return said
}

// 「[Opus 5.5] refusedWith is the google.rpc.Status a 4xx or 5xx carries, read as protobuf.」
func refusedWith(t *testing.T, rec *httptest.ResponseRecorder, code int) *status.Status {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status = %d, want %d: %q", rec.Code, code, rec.Body.String())
	}
	said := &status.Status{}
	if err := proto.Unmarshal(rec.Body.Bytes(), said); err != nil {
		t.Fatalf("the refusal is no google.rpc.Status: %v", err)
	}
	return said
}

// 「[Opus 5.5] A sender names its token in the OTLP headers, and what it sends lands in the namespace that token acts in.」
func TestASpanLandsInTheNamespaceItsTokenActsIn(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	rec := exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "", protobufOf(t, oneSpan(t)))

	if said := answered(t, rec); said.GetPartialSuccess() != nil {
		t.Fatalf("a span fully taken was answered with partial_success %v", said.GetPartialSuccess())
	}
	kept := everythingIn(t, markets["pond"])
	if len(kept) != 1 {
		t.Fatalf("pond holds %d attestations, want the one span", len(kept))
	}
	span := kept[0]
	if !slices.Equal(span.Subjects, []string{"claude-code"}) || !slices.Equal(span.Predicates, []string{"claude_code.tool"}) {
		t.Fatalf("kept as %v is %v, want claude-code is claude_code.tool", span.Subjects, span.Predicates)
	}
	if !slices.Equal(span.Contexts, []string{"trace:" + sentTrace, "span:" + sentSpan}) {
		t.Fatalf("contexts = %v, want the trace and the span by their hex ids", span.Contexts)
	}
	if !slices.Equal(span.Actors, []string{tokenDID}) {
		t.Fatalf("actors = %v, want the token that sent it", span.Actors)
	}
	if !span.Timestamp.Equal(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("timestamp = %s, want the span's start", span.Timestamp)
	}
	held, isMap := span.Attributes["span"].(map[string]any)
	if !isMap || held["traceId"] != sentTrace || held["spanId"] != sentSpan || held["endTimeUnixNano"] == nil {
		t.Fatalf("the span is not kept whole as OTLP/JSON: %v", span.Attributes["span"])
	}
}

// 「[Opus 5.5] JSON is the other body OTLP/HTTP carries, with its ids in hex rather than base64.」
func TestASpanSentAsJSONIsReadWithHexIds(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	body := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-platform"}}]},` +
		`"scopeSpans":[{"spans":[{"traceId":"` + strings.ToUpper(sentTrace) + `","spanId":"` + sentSpan + `",` +
		`"name":"anthropic.session.turn","kind":2,"startTimeUnixNano":"1793872800000000000","unknownField":true}]}]}]}`
	rec := exporting(s, pondToken("*"), ixOtelTraces, "application/json", "", []byte(body))

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, Content-Type %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	kept := everythingIn(t, markets["pond"])
	if len(kept) != 1 || !slices.Equal(kept[0].Contexts, []string{"trace:" + sentTrace, "span:" + sentSpan}) {
		t.Fatalf("kept %v, want the one span under its trace and span", kept)
	}
}

// 「[Opus 5.5] A sender retries, and the Claude API delivers a span at least once: the same span is kept once.」
func TestASpanSentTwiceIsKeptOnce(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	body := protobufOf(t, oneSpan(t))
	for range 2 {
		if said := answered(t, exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "", body)); said.GetPartialSuccess() != nil {
			t.Fatalf("a span sent again was answered with partial_success %v", said.GetPartialSuccess())
		}
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 1 {
		t.Fatalf("pond holds %d attestations after the same span came twice, want 1", len(kept))
	}
}

// 「[Opus 5.5] A token writes what its role's WRITE lines say, here as over /api/attestations.」
func TestASpanItsTokenMayNotWriteIsRejectedAndTheExportIsTaken(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	said := answered(t, exporting(s, pondToken("noted"), ixOtelTraces, "application/x-protobuf", "", protobufOf(t, oneSpan(t))))

	if said.GetPartialSuccess().GetRejectedSpans() != 1 || !strings.Contains(said.GetPartialSuccess().GetErrorMessage(), "claude_code.tool") {
		t.Fatalf("partial_success = %v, want one rejected span naming its predicate", said.GetPartialSuccess())
	}
	if kept := everythingIn(t, markets["pond"]); len(kept) != 0 {
		t.Fatalf("pond holds %v, a span its token may not write", kept)
	}
}

// 「[Opus 5.5] The Claude API sends its spans gzipped.」
func TestAGzippedExportIsRead(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	rec := exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "gzip", gzipped(t, protobufOf(t, oneSpan(t))))

	answered(t, rec)
	if kept := everythingIn(t, markets["pond"]); len(kept) != 1 {
		t.Fatalf("pond holds %d attestations, want the gzipped span", len(kept))
	}
}

func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return packed.Bytes()
}

// 「[Opus 5.5] 413 past ix/otel's bound, and nothing of the body is kept.」
func TestABodyPastTheBoundIs413(t *testing.T) {
	s, _, markets := standServer(t, "pond")
	refusedWith(t, exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "", make([]byte, ixOtelBodyMost+1)), http.StatusRequestEntityTooLarge)
	if kept := everythingIn(t, markets["pond"]); len(kept) != 0 {
		t.Fatalf("pond holds %v after a refused body", kept)
	}
}

// 「[Opus 5.5] OTLP bounds the body after decompression too: a small gzip that unpacks past the bound is 413.」
func TestAGzipThatUnpacksPastTheBoundIs413(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	refusedWith(t, exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "gzip", gzipped(t, make([]byte, ixOtelBodyMost+1))), http.StatusRequestEntityTooLarge)
}

// 「[Opus 5.5] The Claude API retries no 413, and a span it sends with full content holds up to 8 MiB of it:」
// 「[Opus 5.5] a token acting in a namespace is not bound at a namespace's MCP size, which turned such a span away for good.」
func TestAFullContentSpanFromANamespaceTokenIsNot413(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	rec := exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "", make([]byte, 8<<20))
	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("an 8 MiB export from a namespace's token was refused 413: %s", rec.Body.String())
	}
}

func TestABodyThatIsNoExportIs400(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	refusedWith(t, exporting(s, pondToken("*"), ixOtelTraces, "application/x-protobuf", "", []byte{0xff, 0xff, 0xff}), http.StatusBadRequest)
}

func TestAContentTypeOTLPDoesNotSpeakIs415(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	refusedWith(t, exporting(s, pondToken("*"), ixOtelTraces, "text/plain", "", []byte("hello")), http.StatusUnsupportedMediaType)
}

func TestOnlyPostIsTaken(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	req := httptest.NewRequest(http.MethodGet, ixOtelTraces, nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), pondToken("*")))
	rec := httptest.NewRecorder()
	s.HandleIxOtel(rec, req)
	refusedWith(t, rec, http.StatusMethodNotAllowed)
}

// 「[Opus 5.5] Only the signals ix/otel takes are answered under it.」
func TestASignalIxOtelDoesNotTakeIs404(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	refusedWith(t, exporting(s, pondToken("*"), ixOtelPrefix+"v1development/profiles", "application/x-protobuf", "", nil), http.StatusNotFound)
}

// 「[Opus 5.5] A caller in no namespace the node serves reaches no store, and is told so.」
func TestATokenInNoServedNamespaceIsRefused(t *testing.T) {
	s, _, _ := standServer(t, "pond")
	elsewhere := auth.Admitted(auth.LevelToken, "lake")
	elsewhere.Grant = &auth.Grant{DID: tokenDID, Level: auth.LevelToken}
	refusedWith(t, exporting(s, elsewhere, ixOtelTraces, "application/x-protobuf", "", protobufOf(t, oneSpan(t))), http.StatusForbidden)
}

// 「[Opus 5.5] Who may send is the tablet's line, the levels that write attestations, and never ANYONE: no second scheme.」
func TestIxOtelIsReachedAsAWriteOfAttestations(t *testing.T) {
	reached, err := reach.Reached()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reached[ixOtelPrefix], reached["/api/attestations"]) {
		t.Fatalf("%s is reached by %v, and /api/attestations by %v", ixOtelPrefix, reached[ixOtelPrefix], reached["/api/attestations"])
	}
}

// "system (Recommended)"
//
// 「[Opus 5.5] ROOT's agents keep their sessions in system, so what one sends through its door lands there,」
// 「[Opus 5.5] not in the namespace ROOT stands in.」
func TestARootsAgentsSpansLandInSystem(t *testing.T) {
	for _, label := range []string{auth.NodeAgentLabel, auth.ElementAgentLabel + ":canvas"} {
		s, sys, markets := standServer(t, "pond")
		req := httptest.NewRequest(http.MethodPost, ixOtelTraces, bytes.NewReader(protobufOf(t, oneSpan(t))))
		req.Header.Set("Content-Type", "application/x-protobuf")
		ctx := auth.ThroughDoor(req.Context(), auth.Grant{DID: "did:key:agent", Label: label, Level: auth.LevelRoot})
		req = req.WithContext(auth.WithAdmission(ctx, auth.Admitted(auth.LevelRoot)))
		rec := httptest.NewRecorder()
		s.HandleIxOtel(rec, req)
		answered(t, rec)

		if kept := everythingIn(t, sys); len(kept) != 1 {
			t.Fatalf("%s: system holds %d attestations, want the one span", label, len(kept))
		}
		if kept := everythingIn(t, markets["pond"]); len(kept) != 0 {
			t.Fatalf("%s: pond holds %v", label, kept)
		}
	}
}
