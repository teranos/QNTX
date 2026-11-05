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
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] onePoint is one data point of a metric: the metric as OTLP carries it, holding this point」
// 「[Opus 5.5] alone, what kind of number it is, and when it was read.」
type onePoint struct {
	metric *metricspb.Metric
	kind   string
	at     uint64
	point  proto.Message
}

// 「[Opus 5.5] pointsOf is every data point of m, each as m holding that point alone.」
func pointsOf(m *metricspb.Metric) []onePoint {
	var points []onePoint
	alone := func(kind string, at uint64, point proto.Message, data func(*metricspb.Metric)) {
		held := &metricspb.Metric{Name: m.GetName(), Description: m.GetDescription(), Unit: m.GetUnit(), Metadata: m.GetMetadata()}
		data(held)
		points = append(points, onePoint{metric: held, kind: kind, at: at, point: point})
	}
	switch data := m.GetData().(type) {
	case *metricspb.Metric_Gauge:
		for _, p := range data.Gauge.GetDataPoints() {
			alone("gauge", p.GetTimeUnixNano(), p, func(held *metricspb.Metric) {
				held.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{p}}}
			})
		}
	case *metricspb.Metric_Sum:
		for _, p := range data.Sum.GetDataPoints() {
			alone("sum", p.GetTimeUnixNano(), p, func(held *metricspb.Metric) {
				held.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{p},
					AggregationTemporality: data.Sum.GetAggregationTemporality(), IsMonotonic: data.Sum.GetIsMonotonic()}}
			})
		}
	case *metricspb.Metric_Histogram:
		for _, p := range data.Histogram.GetDataPoints() {
			alone("histogram", p.GetTimeUnixNano(), p, func(held *metricspb.Metric) {
				held.Data = &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{DataPoints: []*metricspb.HistogramDataPoint{p},
					AggregationTemporality: data.Histogram.GetAggregationTemporality()}}
			})
		}
	case *metricspb.Metric_ExponentialHistogram:
		for _, p := range data.ExponentialHistogram.GetDataPoints() {
			alone("exponential_histogram", p.GetTimeUnixNano(), p, func(held *metricspb.Metric) {
				held.Data = &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
					DataPoints: []*metricspb.ExponentialHistogramDataPoint{p}, AggregationTemporality: data.ExponentialHistogram.GetAggregationTemporality()}}
			})
		}
	case *metricspb.Metric_Summary:
		for _, p := range data.Summary.GetDataPoints() {
			alone("summary", p.GetTimeUnixNano(), p, func(held *metricspb.Metric) {
				held.Data = &metricspb.Metric_Summary{Summary: &metricspb.Summary{DataPoints: []*metricspb.SummaryDataPoint{p}}}
			})
		}
	}
	return points
}

// 「[Opus 5.5] keepNumbers keeps each data point of an export as one attestation, and counts what it rejects and why.」
// 「[Opus 5.5] An error is the store failing, which the sender is told to retry.」
func (s *QNTXServer) keepNumbers(admitted auth.Admission, store ats.AttestationStore, export *colmetricspb.ExportMetricsServiceRequest) (int64, string, error) {
	var rejected int64
	var why []string
	reject := func(n int, reason string) {
		rejected += int64(n)
		if len(why) < 10 {
			why = append(why, reason)
		}
	}
	actors := s.otlpActors(admitted)
	for _, resourceMetrics := range export.GetResourceMetrics() {
		service := otlpServiceOf(resourceMetrics.GetResource().GetAttributes())
		resource, err := otlpJSONOf(resourceMetrics.GetResource())
		if err != nil {
			return rejected, "", errors.Wrap(err, "a resource did not write as OTLP/JSON")
		}
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			scope, err := otlpJSONOf(scopeMetrics.GetScope())
			if err != nil {
				return rejected, "", errors.Wrap(err, "a scope did not write as OTLP/JSON")
			}
			for _, metric := range scopeMetrics.GetMetrics() {
				points := pointsOf(metric)
				switch {
				case metric.GetName() == "":
					reject(len(points), "a metric names nothing it measures")
					continue
				case !admitted.MayWrite(metric.GetName()):
					reject(len(points), fmt.Sprintf("%s holding %v may not write %q", admitted.LevelName(), admitted.Roles(), metric.GetName()))
					continue
				}
				for _, one := range points {
					// 「[Opus 5.5] A number is its service, its scope, its metric and the point itself, so the same point sent again is kept once.」
					said, err := proto.MarshalOptions{Deterministic: true}.Marshal(one.point)
					if err != nil {
						return rejected, "", errors.Wrapf(err, "a data point of %s did not marshal", metric.GetName())
					}
					sum := sha256.Sum256([]byte(strings.Join([]string{service, scopeMetrics.GetScope().GetName(), metric.GetName(), one.kind, string(said)}, "\x00")))
					id := "AS-otel-metric-" + hex.EncodeToString(sum[:16])
					if store.AttestationExists(id) {
						continue
					}
					held, err := otlpJSONOf(one.metric)
					if err != nil {
						return rejected, "", errors.Wrapf(err, "a data point of %s did not write as OTLP/JSON", metric.GetName())
					}
					attributes := map[string]any{"metric": held, "resource": resource, "scope": scope}
					if url := resourceMetrics.GetSchemaUrl(); url != "" {
						attributes["resource_schema_url"] = url
					}
					if url := scopeMetrics.GetSchemaUrl(); url != "" {
						attributes["scope_schema_url"] = url
					}
					now := time.Now()
					at := now
					if one.at != 0 {
						at = time.Unix(0, int64(one.at)).UTC()
					}
					err = store.CreateAttestation(&types.As{
						ID:         id,
						Subjects:   []string{service},
						Predicates: []string{metric.GetName()},
						Contexts:   []string{"metric:" + one.kind},
						Actors:     actors,
						Timestamp:  at,
						CreatedAt:  now,
						Source:     otlpSource,
						Attributes: attributes,
					})
					switch {
					case err == nil, ats.AlreadyExists(err):
					case ats.Unwritable(err):
						reject(1, err.Error())
					default:
						return rejected, "", errors.Wrapf(err, "a data point of %s was not kept", metric.GetName())
					}
				}
			}
		}
	}
	return rejected, strings.Join(why, "; "), nil
}
