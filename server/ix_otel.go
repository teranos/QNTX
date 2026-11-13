package server

// 「[Opus 5.5] ix is ingest: external data taken in as attestations. ix/otel takes in OpenTelemetry, as OTLP/HTTP.」
//
// 「[Opus 5.5] The wire is the pinned one, server/parity/otlp_v1.11.0_790608c: a sender's OTLP」
// 「[Opus 5.5] base endpoint is <node>/ix/otel, and the specification appends the signal's path,」
// 「[Opus 5.5] v1/traces or v1/metrics, to it. Binary protobuf and JSON bodies, gzip or not, answered in the」
// 「[Opus 5.5] Content-Type they came in.」
//
// "Tokens are to be removed from QNTX except for third-party OAuth clients"
//
// 「[Opus 5.5] Who may send is the reach table's line and what the gate admitted, as for」
// 「[Opus 5.5] /api/attestations: an agent through its door, a person's session, or a request」
// 「[Opus 5.5] signed by a key this node holds a grant for. What it sends lands where that one acts.」

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] ixOtelPrefix is the route; each signal is the specification's default path under it.」
const (
	ixOtelPrefix  = "/ix/otel/"
	ixOtelTraces  = ixOtelPrefix + "v1/traces"
	ixOtelMetrics = ixOtelPrefix + "v1/metrics"
)

// 「[Opus 5.5] The two encodings OTLP/HTTP carries, by the Content-Type each MUST be sent with.」
const (
	otlpProtobuf = "application/x-protobuf"
	otlpJSON     = "application/json"
)

// 「[Opus 5.5] otlpSource is what a kept span says it came through.」
const otlpSource = "otel"

// 「[Opus 5.5] otlpUnknownService is what the semantic conventions name a service that names none.」
const otlpUnknownService = "unknown_service"

// 「[Opus 5.5] HandleIxOtel answers one OTLP/HTTP export. A path under ix/otel that is no signal it takes is 404.」
func (s *QNTXServer) HandleIxOtel(w http.ResponseWriter, r *http.Request) {
	encoding := otlpEncodingOf(r)
	switch r.URL.Path {
	case ixOtelTraces:
		s.takeIn(w, r, encoding, &coltracepb.ExportTraceServiceRequest{}, func(admitted auth.Admission, store ats.AttestationStore, export proto.Message) (proto.Message, error) {
			traces, isTraces := export.(*coltracepb.ExportTraceServiceRequest)
			if !isTraces {
				return nil, errors.Newf("ix/otel decoded %T for traces", export)
			}
			rejected, why, err := s.keepSpans(admitted, store, traces)
			if err != nil {
				return nil, err
			}
			answer := &coltracepb.ExportTraceServiceResponse{}
			if rejected > 0 {
				answer.PartialSuccess = &coltracepb.ExportTracePartialSuccess{RejectedSpans: rejected, ErrorMessage: why}
				s.logger.Warnw("ix/otel rejected spans of an export", "rejected", rejected, "why", why)
			}
			return answer, nil
		})
	case ixOtelMetrics:
		s.takeIn(w, r, encoding, &colmetricspb.ExportMetricsServiceRequest{}, func(admitted auth.Admission, store ats.AttestationStore, export proto.Message) (proto.Message, error) {
			metrics, isMetrics := export.(*colmetricspb.ExportMetricsServiceRequest)
			if !isMetrics {
				return nil, errors.Newf("ix/otel decoded %T for metrics", export)
			}
			rejected, why, err := s.keepNumbers(admitted, store, metrics)
			if err != nil {
				return nil, err
			}
			answer := &colmetricspb.ExportMetricsServiceResponse{}
			if rejected > 0 {
				answer.PartialSuccess = &colmetricspb.ExportMetricsPartialSuccess{RejectedDataPoints: rejected, ErrorMessage: why}
				s.logger.Warnw("ix/otel rejected data points of an export", "rejected", rejected, "why", why)
			}
			return answer, nil
		})
	default:
		otlpRefuse(w, s, encoding, http.StatusNotFound, fmt.Sprintf("ix/otel takes %s and %s, and %s is no signal it takes", ixOtelTraces, ixOtelMetrics, r.URL.Path))
	}
}

// 「[Opus 5.5] takeIn is what every signal shares: a POST in an encoding OTLP speaks, bounded, read into export,」
// 「[Opus 5.5] kept by keep in the store the caller acts in, and answered in the encoding it came in.」
func (s *QNTXServer) takeIn(w http.ResponseWriter, r *http.Request, encoding string, export proto.Message,
	keep func(auth.Admission, ats.AttestationStore, proto.Message) (proto.Message, error)) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		otlpRefuse(w, s, encoding, http.StatusMethodNotAllowed, "an OTLP/HTTP export is a POST")
		return
	}
	if encoding == "" {
		otlpRefuse(w, s, otlpProtobuf, http.StatusUnsupportedMediaType,
			fmt.Sprintf("ix/otel reads %s and %s, and this body is %q", otlpProtobuf, otlpJSON, r.Header.Get("Content-Type")))
		return
	}
	body, code, err := otlpBody(r, otlpBodyMost(r))
	if err != nil {
		otlpRefuse(w, s, encoding, code, err.Error())
		return
	}
	if err := otlpDecode(encoding, body, export); err != nil {
		otlpRefuse(w, s, encoding, http.StatusBadRequest, fmt.Sprintf("the body is no %s: %v", export.ProtoReflect().Descriptor().Name(), err))
		return
	}
	store, err := s.storeFor(r)
	if err != nil {
		otlpRefuse(w, s, encoding, http.StatusForbidden, err.Error())
		return
	}
	admitted, _ := auth.AdmissionFrom(r.Context())
	answer, err := keep(admitted, store, export)
	if err != nil {
		// 「[Opus 5.5] A store that failed is retried by the sender, and what was kept before it is kept once by its id.」
		s.logger.Errorw("ix/otel kept part of an export and the store failed", "path", r.URL.Path, "error", err)
		otlpRefuse(w, s, encoding, http.StatusServiceUnavailable, err.Error())
		return
	}
	otlpAnswer(w, s, encoding, http.StatusOK, answer)
}

// 「[Opus 5.5] otlpEncodingOf is the encoding the request's Content-Type names, or empty for one OTLP does not speak.」
func otlpEncodingOf(r *http.Request) string {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return ""
	}
	switch media {
	case otlpProtobuf, otlpJSON:
		return media
	}
	return ""
}

// 「[Opus 5.5] otlpBodyMost is the bound an export is held to: what the MCP endpoint takes from the same caller.」
// 「[Opus 5.5] ROOT and SUPER act for the node; a caller acting in a namespace is bound at a namespace's size.」
func otlpBodyMost(r *http.Request) int {
	if admitted, ok := auth.AdmissionFrom(r.Context()); ok && admitted.ReachesEveryNamespace() {
		return nodeBodyMost
	}
	return tenantBodyMost
}

// 「[Opus 5.5] otlpBody reads the body, unpacked when gzipped, refusing 413 past most before or after unpacking.」
func otlpBody(r *http.Request, most int) ([]byte, int, error) {
	if r.Body == nil {
		return nil, 0, nil
	}
	packed, err := io.ReadAll(io.LimitReader(r.Body, int64(most)+1))
	if err != nil {
		return nil, http.StatusBadRequest, errors.Wrap(err, "the export did not read")
	}
	if len(packed) > most {
		return nil, http.StatusRequestEntityTooLarge, errors.Newf("the export is more than the %d bytes %s takes", most, r.URL.Path)
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return packed, 0, nil
	case "gzip":
	default:
		return nil, http.StatusUnsupportedMediaType, errors.Newf("ix/otel unpacks gzip, and this export is %q", r.Header.Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, http.StatusBadRequest, errors.Wrap(err, "the export says gzip and is not")
	}
	body, err := io.ReadAll(io.LimitReader(zr, int64(most)+1))
	if err != nil {
		return nil, http.StatusBadRequest, errors.Wrap(err, "the gzipped export did not unpack")
	}
	if len(body) > most {
		return nil, http.StatusRequestEntityTooLarge, errors.Newf("the export unpacks to more than the %d bytes %s takes", most, r.URL.Path)
	}
	return body, 0, nil
}

// 「[Opus 5.5] otlpDecode reads body in its encoding. OTLP/JSON is proto3's JSON mapping with trace and span ids」
// 「[Opus 5.5] in hex rather than base64, and fields it does not know are passed over.」
func otlpDecode(encoding string, body []byte, into proto.Message) error {
	if encoding == otlpProtobuf {
		return proto.Unmarshal(body, into)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var said any
	if err := decoder.Decode(&said); err != nil {
		return err
	}
	// 「[Opus 5.5] One export is one JSON value; anything after it is no export.」
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("the body holds more than one JSON value")
	}
	if err := otlpExportIDs(said); err != nil {
		return err
	}
	mapped, err := json.Marshal(said)
	if err != nil {
		return err
	}
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(mapped, into)
}

// 「[Opus 5.5] otlpIDKeys are the byte fields OTLP/JSON writes in hex, in every message that has them.」
var otlpIDKeys = map[string]bool{"traceId": true, "spanId": true, "parentSpanId": true}

// 「[Opus 5.5] otlpIDs turns every id OTLP/JSON writes in hex with turn, wherever it is in the tree.」
func otlpIDs(v any, turn func(string) (string, error)) error {
	switch held := v.(type) {
	case map[string]any:
		for key, value := range held {
			if said, isText := value.(string); isText && otlpIDKeys[key] {
				turned, err := turn(said)
				if err != nil {
					return errors.Wrapf(err, "%s %q", key, said)
				}
				held[key] = turned
				continue
			}
			if err := otlpIDs(value, turn); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range held {
			if err := otlpIDs(value, turn); err != nil {
				return err
			}
		}
	}
	return nil
}

// 「[Opus 5.5] otlpExportIDs turns the hex ids of an ExportTraceServiceRequest's spans and」
// 「[Opus 5.5] their links, where the schema has them: a field of that name the schema does」
// 「[Opus 5.5] not know is left to be discarded, whatever it holds.」
func otlpExportIDs(export any) error {
	for _, resource := range otlpList(export, "resourceSpans") {
		for _, scope := range otlpList(resource, "scopeSpans") {
			for _, span := range otlpList(scope, "spans") {
				if err := otlpIDsOf(span, "traceId", "spanId", "parentSpanId"); err != nil {
					return err
				}
				for _, link := range otlpList(span, "links") {
					if err := otlpIDsOf(link, "traceId", "spanId"); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// 「[Opus 5.5] otlpZero is whether an id is all zero bytes, which OTLP names invalid.」
func otlpZero(id []byte) bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

// 「[Opus 5.5] otlpList is the list under key of an object, or none.」
func otlpList(v any, key string) []any {
	object, _ := v.(map[string]any)
	list, _ := object[key].([]any)
	return list
}

// 「[Opus 5.5] otlpIDsOf turns the hex ids named keys of one object.」
func otlpIDsOf(v any, keys ...string) error {
	object, _ := v.(map[string]any)
	for _, key := range keys {
		said, isText := object[key].(string)
		if !isText {
			continue
		}
		turned, err := hexToBase64(said)
		if err != nil {
			return errors.Wrapf(err, "%s %q", key, said)
		}
		object[key] = turned
	}
	return nil
}

func hexToBase64(said string) (string, error) {
	raw, err := hex.DecodeString(said)
	if err != nil {
		return "", errors.Wrap(err, "is not hex")
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func base64ToHex(said string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(said)
	if err != nil {
		return "", errors.Wrap(err, "is not base64")
	}
	return hex.EncodeToString(raw), nil
}

// 「[Opus 5.5] otlpJSONOf is m as OTLP/JSON: lowerCamelCase keys, enums as numbers, ids in hex.」
func otlpJSONOf(m proto.Message) (map[string]any, error) {
	mapped, err := protojson.MarshalOptions{UseEnumNumbers: true}.Marshal(m)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(mapped))
	decoder.UseNumber()
	held := map[string]any{}
	if err := decoder.Decode(&held); err != nil {
		return nil, err
	}
	return held, otlpIDs(held, base64ToHex)
}

// 「[Opus 5.5] keepSpans keeps each span of an export as one attestation, and counts what it rejects and why.」
// 「[Opus 5.5] An error is the store failing, which the sender is told to retry.」
func (s *QNTXServer) keepSpans(admitted auth.Admission, store ats.AttestationStore, export *coltracepb.ExportTraceServiceRequest) (int64, string, error) {
	var rejected int64
	var why []string
	reject := func(reason string) {
		rejected++
		if len(why) < 10 {
			why = append(why, reason)
		}
	}
	actors := s.otlpActors(admitted)
	for _, resourceSpans := range export.GetResourceSpans() {
		service := otlpServiceOf(resourceSpans.GetResource().GetAttributes())
		resource, err := otlpJSONOf(resourceSpans.GetResource())
		if err != nil {
			return rejected, "", errors.Wrap(err, "a resource did not write as OTLP/JSON")
		}
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			scope, err := otlpJSONOf(scopeSpans.GetScope())
			if err != nil {
				return rejected, "", errors.Wrap(err, "a scope did not write as OTLP/JSON")
			}
			for _, span := range scopeSpans.GetSpans() {
				trace, spanID := hex.EncodeToString(span.GetTraceId()), hex.EncodeToString(span.GetSpanId())
				switch {
				case len(span.GetTraceId()) != 16 || len(span.GetSpanId()) != 8 || otlpZero(span.GetTraceId()) || otlpZero(span.GetSpanId()):
					reject(fmt.Sprintf("span %q names trace %q and span %q, and a span is a 16-byte trace id and an 8-byte span id, neither all zero", span.GetName(), trace, spanID))
					continue
				case span.GetName() == "":
					reject(fmt.Sprintf("span %s of trace %s names nothing it did", spanID, trace))
					continue
				case !admitted.MayWrite(span.GetName()):
					reject(fmt.Sprintf("%s holding %v may not write %q", admitted.LevelName(), admitted.Roles(), span.GetName()))
					continue
				}
				// 「[Opus 5.5] A span is its trace and span id, so the same span sent again is the same attestation, kept once.」
				id := "AS-otel-" + trace + "-" + spanID
				if store.AttestationExists(id) {
					continue
				}
				held, err := otlpJSONOf(span)
				if err != nil {
					return rejected, "", errors.Wrapf(err, "span %s did not write as OTLP/JSON", spanID)
				}
				attributes := map[string]any{"span": held, "resource": resource, "scope": scope}
				if url := resourceSpans.GetSchemaUrl(); url != "" {
					attributes["resource_schema_url"] = url
				}
				if url := scopeSpans.GetSchemaUrl(); url != "" {
					attributes["scope_schema_url"] = url
				}
				now := time.Now()
				at := now
				if started := span.GetStartTimeUnixNano(); started != 0 {
					// 「[Opus 5.5] Unsigned nanoseconds, split so one past int64's range does not wrap.」
					at = time.Unix(int64(started/1e9), int64(started%1e9)).UTC()
				}
				err = store.CreateAttestation(&types.As{
					ID:         id,
					Subjects:   []string{service},
					Predicates: []string{span.GetName()},
					Contexts:   []string{"trace:" + trace, "span:" + spanID},
					Actors:     actors,
					Timestamp:  at,
					CreatedAt:  now,
					Source:     otlpSource,
					Attributes: attributes,
				})
				switch {
				case err == nil, ats.AlreadyExists(err):
				case ats.Unwritable(err):
					reject(err.Error())
				default:
					return rejected, "", errors.Wrapf(err, "span %s of trace %s was not kept", spanID, trace)
				}
			}
		}
	}
	return rejected, strings.Join(why, "; "), nil
}

// 「[Opus 5.5] otlpActors is who a kept span is by: who the gate admitted, as /api/attestations writes it, or the node.」
func (s *QNTXServer) otlpActors(admitted auth.Admission) []string {
	if by := admitted.ActsAs(); by != "" {
		return []string{by}
	}
	if s.nodeDID != nil {
		return []string{s.nodeActor()}
	}
	return nil
}

// 「[Opus 5.5] otlpServiceOf is the resource's service.name, the subject a span is about.」
func otlpServiceOf(attributes []*commonpb.KeyValue) string {
	for _, attribute := range attributes {
		if attribute.GetKey() == "service.name" {
			if name := attribute.GetValue().GetStringValue(); name != "" {
				return name
			}
		}
	}
	return otlpUnknownService
}

// 「[Opus 5.5] otlpRefuse answers a failure with a google.rpc.Status, as OTLP/HTTP requires of every 4xx and 5xx.」
func otlpRefuse(w http.ResponseWriter, s *QNTXServer, encoding string, code int, message string) {
	if encoding == "" {
		encoding = otlpProtobuf
	}
	otlpAnswer(w, s, encoding, code, &status.Status{Message: message})
}

// 「[Opus 5.5] otlpAnswer writes m in the encoding the request came in.」
func otlpAnswer(w http.ResponseWriter, s *QNTXServer, encoding string, code int, m proto.Message) {
	var body []byte
	var err error
	if encoding == otlpJSON {
		body, err = protojson.MarshalOptions{UseEnumNumbers: true}.Marshal(m)
	} else {
		body, err = proto.Marshal(m)
	}
	if err != nil {
		s.logger.Errorw("ix/otel's answer did not marshal", "status", code, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", encoding)
	w.WriteHeader(code)
	if _, err := w.Write(body); err != nil {
		s.logger.Warnw("ix/otel's answer was not delivered", "status", code, "error", err)
	}
}
