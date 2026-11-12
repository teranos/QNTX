package otlp

// "I WANT QNTX TO SPEAK OTEL"
//
// "THE LANGUAGE OF OBSERVABILITY NEEDS TO BE OTEL"

import (
	"context"
	"os"
	"slices"
	"strings"

	errors "github.com/teranos/sacred-error"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap/zapcore"
)

const (
	// 「[Opus 5.5] LogsEndpoint names where the node's logs go, over Endpoint.」
	LogsEndpoint = "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"
	// 「[Opus 5.5] LogsExporter is OpenTelemetry's own switch: one naming no otlp sends no logs over OTLP.」
	LogsExporter = "OTEL_LOGS_EXPORTER"
)

// 「[Opus 5.5] logs is the provider the node's logs leave through, nil while none do.」
var logs *sdklog.LoggerProvider

// 「[Opus 5.5] LogsSaid is the endpoint the environment names for the node's logs, and whether it names one.」
func LogsSaid() (string, bool) {
	if named := strings.ToLower(strings.TrimSpace(os.Getenv(LogsExporter))); named != "" &&
		!slices.Contains(strings.Split(strings.ReplaceAll(named, " ", ""), ","), "otlp") {
		return "", false
	}
	for _, name := range []string{LogsEndpoint, Endpoint} {
		if endpoint := os.Getenv(name); endpoint != "" {
			return endpoint, true
		}
	}
	return "", false
}

// 「[Opus 5.5] StartLogs is a zap core that sends what the node logs as OTLP/HTTP logs, protobuf, to the」
// 「[Opus 5.5] endpoint said: the message as the body, the level as the severity, the logger's name as the」
// 「[Opus 5.5] scope, the fields as attributes and an error as exception.message and exception.type. The」
// 「[Opus 5.5] exporter reads the rest of OpenTelemetry's own variables itself. No endpoint said is a nil」
// 「[Opus 5.5] core and nothing started.」
func StartLogs(ctx context.Context, release string) (zapcore.Core, error) {
	endpoint, said := LogsSaid()
	if !said {
		return nil, nil
	}

	mu.Lock()
	defer mu.Unlock()
	if logs != nil {
		return nil, errors.New("otlp.StartLogs called twice: the second provider would replace the first and leave its batch unsent")
	}

	exporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to start the OTLP log exporter (endpoint=%q)", endpoint)
	}
	described, err := describe(ctx, release)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to describe the node as an OpenTelemetry resource (endpoint=%q, release=%q)", endpoint, release)
	}
	logs = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(described),
	)
	// 「[Opus 5.5] Info and above, as the console and the file take: a Debug line stays on the box.」
	core, err := zapcore.NewIncreaseLevelCore(otelzap.NewCore("github.com/teranos/QNTX", otelzap.WithLoggerProvider(logs)), zapcore.InfoLevel)
	if err != nil {
		return nil, errors.Wrap(err, "the OTLP log core did not take the Info level")
	}
	return core, nil
}

// 「[Opus 5.5] LogsRunning reports whether the node's logs are leaving over OTLP.」
func LogsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return logs != nil
}
