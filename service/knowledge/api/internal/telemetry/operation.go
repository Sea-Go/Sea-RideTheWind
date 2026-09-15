package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type fieldsKey struct{}
type requestKey struct{}
type linkKey struct{}
type requestState struct {
	mu            sync.Mutex
	id, operation string
	logged        atomic.Bool
}
type stageState struct {
	mu     sync.Mutex
	fields map[string]any
	replay bool
}
type ErrorInfo struct{ Code, Type, Message, Outcome, Level string }
type Finish func(error, ErrorInfo, map[string]any)

var operations = map[string]bool{
	"knowledge.module.create": true, "knowledge.source.create": true, "knowledge.wiki.create": true, "knowledge.release.create": true,
	"knowledge.build.create": true, "knowledge.build.claim": true, "knowledge.build.accept": true, "knowledge.build.cancel": true,
	"knowledge.compile.create": true, "knowledge.compile.claim": true, "knowledge.compile.accept": true, "knowledge.compile.cancel": true,
	"knowledge.compile.job.submit": true, "knowledge.compile.job.cancel": true,
	"knowledge.release.activate": true, "knowledge.content.withdraw": true, "knowledge.outbox.deliver": true,
	"knowledge.outbox.poll":             true,
	"knowledge.search.snapshot.current": true, "knowledge.search.source.read": true, "knowledge.search.citations.accept": true, "knowledge.search.citations.get": true,
	"knowledge.answer.accept": true, "knowledge.answer.get": true, "knowledge.answer.list": true,
	"knowledge.answer.v2.get": true, "knowledge.answer.v2.list": true,
	"knowledge.product.search": true, "knowledge.product.search.get": true,
	"knowledge.reviewer.key.register": true, "knowledge.reviewer.key.revoke": true, "knowledge.reviewer.key.get": true,
	"knowledge.answer.grounding.review.submit": true, "knowledge.answer.grounding.review.get": true,
	"knowledge.search.judgment.record": true, "knowledge.search.judgment.withdraw": true,
	"knowledge.search.judgment.event.read": true,
}

var readOnlyOperations = map[string]bool{
	"knowledge.search.snapshot.current": true, "knowledge.search.source.read": true, "knowledge.search.citations.get": true,
	"knowledge.answer.get": true, "knowledge.answer.list": true,
	"knowledge.answer.v2.get": true, "knowledge.answer.v2.list": true, "knowledge.outbox.poll": true,
	"knowledge.product.search.get": true,
	"knowledge.reviewer.key.get":   true, "knowledge.answer.grounding.review.get": true,
	"knowledge.search.judgment.event.read": true,
}

// DC Jobs delivery commits a technical receipt, not a Wiki business revision.
// It still has operation counters and spans, but cannot increment the domain
// transition counter used for Compile/Revision acceptance.
var technicalOperations = map[string]bool{
	"knowledge.compile.job.submit": true,
	"knowledge.compile.job.cancel": true,
}

func (r *Runtime) PollFailure(ctx context.Context, err error) {
	if r == nil || err == nil {
		return
	}
	_, finish := r.Begin(ctx, "knowledge.outbox.poll", "outbox:poll", nil)
	info := Unexpected(err)
	if errors.Is(err, context.Canceled) {
		info.Code, info.Type, info.Outcome, info.Level = "CANCELLED", "context.Canceled", "cancelled", "warn"
	} else if errors.Is(err, context.DeadlineExceeded) {
		info.Code, info.Type, info.Outcome, info.Level = "TIMEOUT", "context.DeadlineExceeded", "timeout", "warn"
	}
	finish(err, info, nil)
}

func RequestContext(ctx context.Context, id, operation string) context.Context {
	ctx = context.WithValue(ctx, requestKey{}, &requestState{id: id, operation: operation})
	fields := []logx.LogField{logx.Field("request_id", id)}
	return logx.ContextWithFields(ctx, fields...)
}
func RequestID(ctx context.Context) string {
	r, _ := ctx.Value(requestKey{}).(*requestState)
	if r == nil {
		return ""
	}
	return r.id
}
func OperationID(ctx context.Context) string {
	r, _ := ctx.Value(requestKey{}).(*requestState)
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.operation
}
func MarkError(ctx context.Context) {
	if r, ok := ctx.Value(requestKey{}).(*requestState); ok {
		r.logged.Store(true)
	}
}
func ErrorRecorded(ctx context.Context) bool {
	r, _ := ctx.Value(requestKey{}).(*requestState)
	return r != nil && r.logged.Load()
}
func Add(ctx context.Context, fields map[string]any) {
	if s, ok := ctx.Value(fieldsKey{}).(*stageState); ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range fields {
			s.fields[k] = v
		}
	}
}
func Replay(ctx context.Context) {
	if s, ok := ctx.Value(fieldsKey{}).(*stageState); ok {
		s.mu.Lock()
		s.replay = true
		s.mu.Unlock()
	}
}
func (r *Runtime) Begin(ctx context.Context, operation, operationID string, fields map[string]any) (context.Context, Finish) {
	if r == nil {
		return ctx, func(error, ErrorInfo, map[string]any) {}
	}
	if !operations[operation] {
		operation = "knowledge.unknown"
	}
	if state, ok := ctx.Value(requestKey{}).(*requestState); ok {
		state.mu.Lock()
		state.operation = operationID
		state.mu.Unlock()
	}
	s := &stageState{fields: map[string]any{"operation_id": operationID, "component": "knowledge", "log_source": "application"}}
	if requestID := RequestID(ctx); requestID != "" {
		s.fields["request_id"] = requestID
	}
	for k, v := range fields {
		s.fields[k] = v
	}
	options := []trace.SpanStartOption{trace.WithAttributes(attribute.String("operation.name", operation), attribute.String("operation.id", operationID))}
	if source, ok := ctx.Value(linkKey{}).(trace.SpanContext); ok && source.IsValid() {
		options = append(options, trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: source}))
	}
	ctx, span := r.Tracer.Start(ctx, operation, options...)
	ctx = context.WithValue(ctx, fieldsKey{}, s)
	ctx = logx.ContextWithFields(ctx, logx.Field("operation_id", operationID))
	started := time.Now()
	r.inFlight.WithLabelValues(operation).Inc()
	emitStage(ctx, operation+".started", "operation started", "info", s.fields)
	return ctx, func(err error, info ErrorInfo, results map[string]any) {
		defer span.End()
		r.inFlight.WithLabelValues(operation).Dec()
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range results {
			s.fields[k] = v
		}
		outcome, level, code := "succeeded", "info", "NONE"
		if err != nil {
			outcome, level, code = info.Outcome, info.Level, info.Code
			if outcome == "" {
				outcome = "failed"
			}
			if level == "" {
				level = "error"
			}
			if code == "" {
				code = "INTERNAL"
			}
			s.fields["error_code"] = code
			s.fields["error_type"] = info.Type
			s.fields["error_message"] = info.Message
			MarkError(ctx)
			span.RecordError(err)
			span.SetStatus(codes.Error, code)
		} else if s.replay {
			outcome = "replayed"
		} else if operation == "knowledge.build.cancel" || operation == "knowledge.compile.cancel" {
			outcome = "cancelled"
		}
		elapsed := time.Since(started)
		s.fields["outcome"] = outcome
		s.fields["duration_ms"] = float64(elapsed.Microseconds()) / 1000
		r.operations.WithLabelValues(operation, outcome, code).Inc()
		r.duration.WithLabelValues(operation, outcome).Observe(elapsed.Seconds())
		if err == nil && !s.replay && !readOnlyOperations[operation] && !technicalOperations[operation] {
			r.committed.WithLabelValues(operation).Inc()
		}
		for key, value := range s.fields {
			switch v := value.(type) {
			case string:
				span.SetAttributes(attribute.String(key, v))
			case int64:
				span.SetAttributes(attribute.Int64(key, v))
			case int:
				span.SetAttributes(attribute.Int(key, v))
			case bool:
				span.SetAttributes(attribute.Bool(key, v))
			}
		}
		emitStage(ctx, operation+"."+outcome, "operation "+outcome, level, s.fields)
	}
}
func emitStage(ctx context.Context, event, message, level string, values map[string]any) {
	fields := make([]logx.LogField, 0, len(values)+2)
	for k, v := range values {
		fields = append(fields, logx.Field(k, v))
	}
	fields = append(fields, logx.Field("event", event), logx.Field("level", level))
	if level == "error" {
		logx.WithContext(ctx).Errorw(message, fields...)
	} else {
		logx.WithContext(ctx).Infow(message, fields...)
	}
}

// Correlation is persisted beside the H04 event, not added to its wire schema.
type Correlation struct {
	TraceParent string `json:"traceparent,omitempty"`
	TraceState  string `json:"tracestate,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

func Capture(ctx context.Context) Correlation {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return Correlation{TraceParent: carrier.Get("traceparent"), TraceState: carrier.Get("tracestate"), RequestID: RequestID(ctx)}
}
func Resume(ctx context.Context, c Correlation, operation string) context.Context {
	source := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": c.TraceParent, "tracestate": c.TraceState})
	ctx = RequestContext(ctx, c.RequestID, operation)
	return context.WithValue(ctx, linkKey{}, trace.SpanContextFromContext(source))
}
func Failure(ctx context.Context, err error, info ErrorInfo) {
	if ErrorRecorded(ctx) {
		return
	}
	MarkError(ctx)
	emitStage(ctx, "knowledge.request.rejected", "request rejected", info.Level, map[string]any{"component": "knowledge", "log_source": "application", "request_id": RequestID(ctx), "operation_id": OperationID(ctx), "outcome": info.Outcome, "duration_ms": 0, "error_code": info.Code, "error_type": info.Type, "error_message": info.Message})
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, info.Code)
}
func Unexpected(err error) ErrorInfo {
	return ErrorInfo{Code: "INTERNAL", Type: fmt.Sprintf("%T", err), Message: err.Error(), Outcome: "failed", Level: "error"}
}
