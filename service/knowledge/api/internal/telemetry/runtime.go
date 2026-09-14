package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	Version               string  `json:",optional"`
	InstanceID            string  `json:",optional"`
	LogQueueSize          int     `json:",default=512"`
	Endpoint              string  `json:",optional"`
	Insecure              bool    `json:",default=false"`
	SampleRatio           float64 `json:",default=1"`
	QueueSize             int     `json:",default=512"`
	ExportTimeoutMillis   int     `json:",default=1000"`
	ShutdownTimeoutMillis int     `json:",default=3000"`
}
type Runtime struct {
	Writer         *Writer
	Registry       *prometheus.Registry
	Provider       *sdktrace.TracerProvider
	Tracer         trace.Tracer
	Config         Config
	operations     *prometheus.CounterVec
	committed      *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	inFlight       *prometheus.GaugeVec
	requests       *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	backlog        prometheus.Gauge
	exportFailures prometheus.Counter
}

func New(ctx context.Context, cfg Config, writer *Writer, meta Metadata) (*Runtime, error) {
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 512
	}
	if cfg.ExportTimeoutMillis == 0 {
		cfg.ExportTimeoutMillis = 1000
	}
	if cfg.ShutdownTimeoutMillis == 0 {
		cfg.ShutdownTimeoutMillis = 3000
	}
	if writer == nil || cfg.QueueSize < 1 || cfg.QueueSize > 65536 || cfg.SampleRatio < 0 || cfg.SampleRatio > 1 || cfg.ExportTimeoutMillis < 1 || cfg.ShutdownTimeoutMillis < 1 {
		return nil, errors.New("invalid observability bounds")
	}
	r := &Runtime{Writer: writer, Config: cfg, Registry: prometheus.NewRegistry()}
	r.operations = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sea_knowledge_operations_total", Help: "Completed command attempts; includes replay, rejection and cancellation."}, []string{"operation", "outcome", "error_code"})
	r.committed = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sea_knowledge_commits_total", Help: "Committed domain transitions; replay never increments this counter."}, []string{"operation"})
	r.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sea_knowledge_operation_duration_seconds", Help: "Wall duration per bounded operation and outcome.", Buckets: prometheus.DefBuckets}, []string{"operation", "outcome"})
	r.inFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "sea_knowledge_operations_in_flight", Help: "Currently running commands."}, []string{"operation"})
	r.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sea_knowledge_http_requests_total", Help: "Completed HTTP requests by registered route and status class."}, []string{"route", "method", "status_class"})
	r.httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sea_knowledge_http_duration_seconds", Help: "HTTP request duration by registered route.", Buckets: prometheus.DefBuckets}, []string{"route", "method"})
	r.backlog = prometheus.NewGauge(prometheus.GaugeOpts{Name: "sea_knowledge_outbox_pending", Help: "Undelivered durable events at the latest dispatcher observation."})
	r.exportFailures = prometheus.NewCounter(prometheus.CounterOpts{Name: "sea_knowledge_trace_export_failures_total", Help: "Failed OTLP export batches."})
	r.Registry.MustRegister(r.operations, r.committed, r.duration, r.inFlight, r.requests, r.httpDuration, r.backlog, r.exportFailures,
		prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "sea_knowledge_log_records_dropped_total", Help: "Records rejected by the bounded log queue or closed writer."}, func() float64 { return float64(writer.dropped.Load()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "sea_knowledge_log_write_failures_total", Help: "JSON encoding or sink write failures."}, func() float64 { return float64(writer.failures.Load()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "sea_knowledge_log_queue_records", Help: "Queued log records."}, func() float64 { return float64(len(writer.queue)) }))
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", meta.Service), attribute.String("service.version", meta.Version), attribute.String("deployment.environment.name", meta.Environment), attribute.String("service.instance.id", meta.Instance))), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio)))}
	if cfg.Endpoint != "" {
		options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint), otlptracegrpc.WithTimeout(time.Duration(cfg.ExportTimeoutMillis) * time.Millisecond)}
		if cfg.Insecure {
			options = append(options, otlptracegrpc.WithInsecure())
		}
		exporter, err := otlptracegrpc.New(ctx, options...)
		if err != nil {
			return nil, err
		}
		wrapped := &exporterMetrics{SpanExporter: exporter, runtime: r}
		batch := sdktrace.NewBatchSpanProcessor(wrapped, sdktrace.WithMaxQueueSize(cfg.QueueSize), sdktrace.WithMaxExportBatchSize(min(128, cfg.QueueSize)), sdktrace.WithBatchTimeout(200*time.Millisecond), sdktrace.WithExportTimeout(time.Duration(cfg.ExportTimeoutMillis)*time.Millisecond))
		opts = append(opts, sdktrace.WithSpanProcessor(batch))
	}
	r.Provider = sdktrace.NewTracerProvider(opts...)
	r.Tracer = r.Provider.Tracer("sea.ridethewind.knowledge")
	return r, nil
}

// Install is invoked once before go-zero constructs its request chain. Its
// Telemetry.Disabled flag prevents a second framework-owned provider.
func (r *Runtime) Install() {
	otel.SetTracerProvider(r.Provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logx.Errorw("trace export failed", logx.Field("component", "telemetry"), logx.Field("log_source", "runtime"), logx.Field("event", "telemetry.trace.export_failed"), logx.Field("outcome", "failed"), logx.Field("duration_ms", 0), logx.Field("error_code", "TRACE_EXPORT_FAILED"), logx.Field("error_type", fmt.Sprintf("%T", err)), logx.Field("error_message", err.Error()))
	}))
}
func (r *Runtime) Metrics() http.Handler {
	return promhttp.HandlerFor(r.Registry, promhttp.HandlerOpts{})
}
func (r *Runtime) Close(ctx context.Context) error { return r.Provider.Shutdown(ctx) }
func (r *Runtime) Backlog(count int64) {
	if r != nil {
		r.backlog.Set(float64(count))
	}
}

type exporterMetrics struct {
	sdktrace.SpanExporter
	runtime *Runtime
}

func (e *exporterMetrics) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	if err != nil {
		e.runtime.exportFailures.Inc()
	}
	return err
}
