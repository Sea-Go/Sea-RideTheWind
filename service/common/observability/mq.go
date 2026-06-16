package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TraceConsumer(ctx context.Context, tracerName, spanName string, attrs []attribute.KeyValue, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracerName == "" {
		tracerName = "mq-consumer"
	}
	if spanName == "" {
		spanName = "message.consume"
	}

	start := time.Now()
	consumeCtx, span := otel.Tracer(tracerName).Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	err := fn(consumeCtx)
	duration := time.Since(start)
	if err != nil {
		if IsTimeoutError(err) {
			MarkTimeout(consumeCtx, err, duration, attrs...)
		} else {
			MarkSystemError(consumeCtx, err, duration, attrs...)
		}
		return err
	}

	MarkDurationAndSlow(consumeCtx, duration, SlowThreshold(), attrs...)
	return nil
}
