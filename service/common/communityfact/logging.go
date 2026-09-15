package communityfact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var processVersion = regexp.MustCompile(`^[0-9a-f]{40}$`)

type ProcessMetadata struct {
	Service     string
	Environment string
	Version     string
	InstanceID  string
	Component   string
}

func (m ProcessMetadata) Validate() error {
	if m.Service == "" || m.InstanceID == "" || m.Component == "" || !processVersion.MatchString(m.Version) {
		return errors.New("community fact process identity incomplete")
	}
	switch m.Environment {
	case "local", "test", "staging", "production":
		return nil
	default:
		return errors.New("community fact process environment invalid")
	}
}

// NewProcessLogger provides the OBS-r3 envelope for standalone community fact
// processes and attaches a valid OpenTelemetry context when one is present.
func NewProcessLogger(output io.Writer, meta ProcessMetadata) *slog.Logger {
	known := func(value string) string {
		if value == "" {
			return "unresolved"
		}
		return value
	}
	base := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		switch attr.Key {
		case slog.TimeKey:
			return slog.String("timestamp", attr.Value.Time().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
		case slog.LevelKey:
			return slog.String("level", strings.ToLower(attr.Value.String()))
		case slog.MessageKey:
			return slog.String("message", attr.Value.String())
		}
		return attr
	}})
	handler := traceLogHandler{next: base}.WithAttrs([]slog.Attr{
		slog.String("service", known(meta.Service)), slog.String("environment", known(meta.Environment)),
		slog.String("service_version", known(meta.Version)), slog.String("instance_id", known(meta.InstanceID)),
		slog.String("component", known(meta.Component)), slog.String("log_source", "application"),
	})
	return slog.New(handler)
}

type traceLogHandler struct{ next slog.Handler }

func (h traceLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h traceLogHandler) Handle(ctx context.Context, record slog.Record) error {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		record.AddAttrs(slog.String("trace_id", span.TraceID().String()), slog.String("span_id", span.SpanID().String()),
			slog.Bool("trace_sampled", span.IsSampled()))
	}
	return h.next.Handle(ctx, record)
}

func (h traceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceLogHandler{next: h.next.WithAttrs(attrs)}
}

func (h traceLogHandler) WithGroup(name string) slog.Handler {
	return traceLogHandler{next: h.next.WithGroup(name)}
}

// InstallLocalTracing gives local processes real W3C trace contexts. Export is
// intentionally absent in this slice; the DC child still receives traceparent.
func InstallLocalTracing() *sdktrace.TracerProvider {
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider
}

// InstallFrameworkLogger routes go-zero lifecycle output (including signal
// shutdown) through the same process envelope instead of its global console
// writer. The caller still owns the slog sink.
func InstallFrameworkLogger(logger *slog.Logger) error {
	if logger == nil {
		return errors.New("community framework logger is not configured")
	}
	if err := logx.SetUp(logx.LogConf{Mode: "console", Encoding: "json", Level: "info", Stat: false}); err != nil {
		return err
	}
	logx.SetWriter(frameworkLogWriter{logger: logger.With("component", "go-zero", "log_source", "framework")})
	logx.SetLevel(logx.InfoLevel)
	return nil
}

type frameworkLogWriter struct{ logger *slog.Logger }

func (w frameworkLogWriter) write(level slog.Level, value any, fields ...logx.LogField) {
	attrs := []any{"event", "framework.log." + strings.ToLower(level.String())}
	for _, field := range fields {
		key := field.Key
		switch key {
		case "trace":
			key = "trace_id"
		case "span":
			key = "span_id"
		case "caller":
			key = "code_location"
		case "timestamp", "service", "environment", "service_version", "instance_id", "message", "component", "log_source":
			continue
		}
		attrs = append(attrs, key, field.Value)
	}
	w.logger.Log(context.Background(), level, fmt.Sprint(value), attrs...)
}

func (w frameworkLogWriter) Alert(v any)                     { w.write(slog.LevelError, v) }
func (w frameworkLogWriter) Close() error                    { return nil }
func (w frameworkLogWriter) Debug(v any, f ...logx.LogField) { w.write(slog.LevelDebug, v, f...) }
func (w frameworkLogWriter) Error(v any, f ...logx.LogField) { w.write(slog.LevelError, v, f...) }
func (w frameworkLogWriter) Info(v any, f ...logx.LogField)  { w.write(slog.LevelInfo, v, f...) }
func (w frameworkLogWriter) Severe(v any)                    { w.write(slog.LevelError, v) }
func (w frameworkLogWriter) Slow(v any, f ...logx.LogField)  { w.write(slog.LevelWarn, v, f...) }
func (w frameworkLogWriter) Stack(v any)                     { w.write(slog.LevelError, v) }
func (w frameworkLogWriter) Stat(v any, f ...logx.LogField)  { w.write(slog.LevelInfo, v, f...) }
