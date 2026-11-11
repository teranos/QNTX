package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] otlpUnnamedLog is what a log record that names no event is said to be.」
const otlpUnnamedLog = "log"

// 「[Opus 5.5] otlpEventOf is what a log record says happened: its event_name, or the event.name attribute」
// 「[Opus 5.5] senders wrote before the field existed, or otlpUnnamedLog.」
func otlpEventOf(record *logspb.LogRecord) string {
	if name := record.GetEventName(); name != "" {
		return name
	}
	for _, attribute := range record.GetAttributes() {
		if attribute.GetKey() == "event.name" {
			if name := attribute.GetValue().GetStringValue(); name != "" {
				return name
			}
		}
	}
	return otlpUnnamedLog
}

// 「[Opus 5.5] keepRecords keeps each log record of an export as one attestation, and counts what it rejects and why.」
// 「[Opus 5.5] An error is the store failing, which the sender is told to retry.」
func (s *QNTXServer) keepRecords(admitted auth.Admission, store ats.AttestationStore, export *collogspb.ExportLogsServiceRequest) (int64, string, error) {
	var rejected int64
	var why []string
	reject := func(reason string) {
		rejected++
		if len(why) < 10 {
			why = append(why, reason)
		}
	}
	actors := s.otlpActors(admitted)
	for _, resourceLogs := range export.GetResourceLogs() {
		service := otlpServiceOf(resourceLogs.GetResource().GetAttributes())
		resource, err := otlpJSONOf(resourceLogs.GetResource())
		if err != nil {
			return rejected, "", errors.Wrap(err, "a resource did not write as OTLP/JSON")
		}
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			scope, err := otlpJSONOf(scopeLogs.GetScope())
			if err != nil {
				return rejected, "", errors.Wrap(err, "a scope did not write as OTLP/JSON")
			}
			for _, record := range scopeLogs.GetLogRecords() {
				event := otlpEventOf(record)
				if !admitted.MayWrite(event) {
					reject(fmt.Sprintf("%s holding %v may not write %q", admitted.LevelName(), admitted.Roles(), event))
					continue
				}
				// 「[Opus 5.5] A record is its service, its scope and the record itself, so the same record sent again is kept once.」
				said, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
				if err != nil {
					return rejected, "", errors.Wrapf(err, "a log record of %s did not marshal", event)
				}
				sum := sha256.Sum256([]byte(strings.Join([]string{service, scopeLogs.GetScope().GetName(), string(said)}, "\x00")))
				id := "AS-otel-log-" + hex.EncodeToString(sum[:16])
				if store.AttestationExists(id) {
					continue
				}
				held, err := otlpJSONOf(record)
				if err != nil {
					return rejected, "", errors.Wrapf(err, "a log record of %s did not write as OTLP/JSON", event)
				}
				attributes := map[string]any{"log": held, "resource": resource, "scope": scope}
				if url := resourceLogs.GetSchemaUrl(); url != "" {
					attributes["resource_schema_url"] = url
				}
				if url := scopeLogs.GetSchemaUrl(); url != "" {
					attributes["scope_schema_url"] = url
				}
				severity := record.GetSeverityText()
				if severity == "" {
					severity = record.GetSeverityNumber().String()
				}
				contexts := []string{"log:" + severity}
				if len(record.GetTraceId()) == 16 {
					contexts = append(contexts, "trace:"+hex.EncodeToString(record.GetTraceId()))
				}
				if len(record.GetSpanId()) == 8 {
					contexts = append(contexts, "span:"+hex.EncodeToString(record.GetSpanId()))
				}
				now := time.Now()
				at := now
				if happened := record.GetTimeUnixNano(); happened != 0 {
					at = time.Unix(0, int64(happened)).UTC()
				} else if observed := record.GetObservedTimeUnixNano(); observed != 0 {
					at = time.Unix(0, int64(observed)).UTC()
				}
				err = store.CreateAttestation(&types.As{
					ID:         id,
					Subjects:   []string{service},
					Predicates: []string{event},
					Contexts:   contexts,
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
					return rejected, "", errors.Wrapf(err, "a log record of %s was not kept", event)
				}
			}
		}
	}
	return rejected, strings.Join(why, "; "), nil
}
