package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

var testMetadata = Metadata{
	Service: "ridethewind-knowledge", Environment: "test",
	Version: "0123456789abcdef", Instance: "test-instance",
}

func TestCaptureResumeExportsNewRootWithSpanLink(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	collector := &traceCollector{}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, collector)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })

	var output bytes.Buffer
	writer, err := NewWriter(&output, testMetadata, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Install("info"); err != nil {
		t.Fatal(err)
	}
	config := Config{
		Endpoint: listener.Addr().String(), Insecure: true, SampleRatio: 1,
		QueueSize: 8, ExportTimeoutMillis: 2000, ShutdownTimeoutMillis: 2000,
	}
	runtime, err := New(context.Background(), config, writer, testMetadata)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Install()
	ctx, source := runtime.Tracer.Start(RequestContext(context.Background(), "request-42", "knowledge.source.create"), "source-request")
	sourceContext := source.SpanContext()
	correlation := Capture(ctx)
	source.End()
	if correlation.TraceParent == "" || correlation.RequestID != "request-42" {
		t.Fatalf("capture lost trace or request: %+v", correlation)
	}
	if !sourceContext.IsValid() || !sourceContext.IsSampled() {
		t.Fatalf("source span was not a sampled real span: %v", sourceContext)
	}

	resumed := Resume(context.Background(), correlation, "knowledge.outbox.deliver")
	operation, finish := runtime.Begin(resumed, "knowledge.outbox.deliver", "event-7", nil)
	targetContext := trace.SpanContextFromContext(operation)
	finish(nil, ErrorInfo{}, map[string]any{"event_id": "event-7"})
	if !targetContext.IsValid() || !targetContext.IsSampled() {
		t.Fatalf("resumed operation did not create a sampled real span: %v", targetContext)
	}
	if targetContext.TraceID() == sourceContext.TraceID() {
		t.Fatalf("async operation reused source trace: %s", targetContext.TraceID())
	}
	if targetContext.SpanID() == sourceContext.SpanID() {
		t.Fatal("async operation reused source span ID")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Close(closeCtx); err != nil {
		t.Fatalf("OTLP shutdown/export: %v", err)
	}
	if err := writer.CloseContext(closeCtx); err != nil {
		t.Fatalf("log drain: %v", err)
	}

	spans := collector.spans()
	sourceSpan := findSpan(spans, "source-request")
	targetSpan := findSpan(spans, "knowledge.outbox.deliver")
	if sourceSpan == nil || targetSpan == nil {
		t.Fatalf("collector did not receive both source and async operation: source=%v target=%v total=%d", sourceSpan != nil, targetSpan != nil, len(spans))
	}
	sourceTraceID := sourceContext.TraceID()
	targetTraceID := targetContext.TraceID()
	if !bytes.Equal(sourceSpan.TraceId, sourceTraceID[:]) {
		t.Fatalf("exported source trace differs from live span: %x", sourceSpan.TraceId)
	}
	if !bytes.Equal(targetSpan.TraceId, targetTraceID[:]) {
		t.Fatalf("exported target trace differs from live span: %x", targetSpan.TraceId)
	}
	if len(targetSpan.ParentSpanId) != 0 {
		t.Fatalf("resumed span should be a new root, parent=%x", targetSpan.ParentSpanId)
	}
	if len(targetSpan.Links) != 1 || !bytes.Equal(targetSpan.Links[0].TraceId, sourceSpan.TraceId) || !bytes.Equal(targetSpan.Links[0].SpanId, sourceSpan.SpanId) {
		t.Fatalf("async span must link to the captured source: %+v", targetSpan.Links)
	}
	if !strings.Contains(output.String(), `"event":"knowledge.outbox.deliver.started"`) || !strings.Contains(output.String(), targetContext.TraceID().String()) {
		t.Fatalf("operation log not correlated with exported trace: %s", output.String())
	}
}

func TestWriterProducesOneJSONLineWithContextTrace(t *testing.T) {
	var output bytes.Buffer
	writer, err := NewWriter(&output, testMetadata, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Install("info"); err != nil {
		t.Fatal(err)
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := provider.Tracer("test").Start(RequestContext(context.Background(), "req-1", "knowledge.wiki.create"), "request")
	spanContext := span.SpanContext()
	logx.WithContext(ctx).Infow("created", logx.Field("event", "knowledge.wiki.created"), logx.Field("component", "knowledge"), logx.Field("log_source", "application"))
	span.End()
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("wanted exactly one JSON line, got %d: %q", len(lines), output.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("invalid JSON line: %v: %q", err, lines[0])
	}
	for field, expected := range map[string]string{
		"service": testMetadata.Service, "environment": testMetadata.Environment,
		"service_version": testMetadata.Version, "instance_id": testMetadata.Instance,
		"trace_id": spanContext.TraceID().String(), "span_id": spanContext.SpanID().String(),
		"request_id": "req-1", "event": "knowledge.wiki.created", "message": "created",
	} {
		if actual := record[field]; actual != expected {
			t.Errorf("%s = %v, want %s", field, actual, expected)
		}
	}
}

func TestWriterBoundsBackpressureAndDrainsOnClose(t *testing.T) {
	sink := &blockingSink{started: make(chan struct{}), release: make(chan struct{})}
	writer, err := NewWriter(sink, testMetadata, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.unblock()
	writer.Info("first")
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start flushing the first record")
	}
	writer.Info("second")
	writer.Info("third")
	if got := writer.dropped.Load(); got != 1 {
		t.Fatalf("bounded queue should drop the third record, got %d drops", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := writer.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close must respect blocked sink deadline, got %v", err)
	}
	sink.unblock()
	if err := writer.CloseContext(context.Background()); err != nil {
		t.Fatalf("close must finish draining after sink recovers: %v", err)
	}
	writer.Info("after-close")
	if got := writer.dropped.Load(); got != 2 {
		t.Fatalf("closed writer must reject later records, got %d drops", got)
	}
	lines := strings.Split(strings.TrimSuffix(sink.contents(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("close did not drain both queued records: %q", sink.contents())
	}
	for i, want := range []string{"first", "second"} {
		var record map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &record); err != nil {
			t.Fatalf("record %d is not JSON: %v", i, err)
		}
		if record["message"] != want {
			t.Fatalf("record %d message = %v, want %s", i, record["message"], want)
		}
	}
}

func TestWriterFlushReportsSinkFailureBeforeSuccessfulStop(t *testing.T) {
	writer, err := NewWriter(failedSink{}, testMetadata, 2)
	if err != nil {
		t.Fatal(err)
	}
	writer.Info("shutdown checkpoint")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Flush(ctx); err == nil || !strings.Contains(err.Error(), "log sink writes failed") {
		t.Fatalf("flush must surface sink failure before terminal success log: %v", err)
	}
	if err := writer.CloseContext(ctx); err == nil {
		t.Fatal("close must not discard sink failure")
	}
}

type failedSink struct{}

func (failedSink) Write([]byte) (int, error) { return 0, errors.New("synthetic sink failure") }

type traceCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	mu       sync.Mutex
	received []*tracepb.Span
}

func (c *traceCollector) Export(_ context.Context, req *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, resource := range req.ResourceSpans {
		for _, scope := range resource.ScopeSpans {
			c.received = append(c.received, scope.Spans...)
		}
	}
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func (c *traceCollector) spans() []*tracepb.Span {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*tracepb.Span(nil), c.received...)
}

func findSpan(spans []*tracepb.Span, name string) *tracepb.Span {
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	return nil
}

type blockingSink struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSink) Write(data []byte) (int, error) {
	s.once.Do(func() { close(s.started) })
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.Write(data)
}

func (s *blockingSink) unblock() {
	select {
	case <-s.release:
	default:
		close(s.release)
	}
}

func (s *blockingSink) contents() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.String()
}
