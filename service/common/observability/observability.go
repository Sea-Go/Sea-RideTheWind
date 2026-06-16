package observability

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	SuccessBizCode       = 200
	DefaultSlowThreshold = 500 * time.Millisecond

	AttrError     = "error"
	AttrErrorKind = "sea.error.kind"
	AttrBizCode   = "sea.biz_code"
	AttrBizMsg    = "sea.biz_msg"
	AttrTimeout   = "sea.timeout"
	AttrSlow      = "sea.slow"
	AttrDuration  = "sea.duration_ms"
)

const (
	ErrorKindBusiness = "business"
	ErrorKindSystem   = "system"
	ErrorKindTimeout  = "timeout"
)

// SlowThreshold returns the configured slow-call threshold.
// SEA_OBSERVABILITY_SLOW_THRESHOLD accepts either a Go duration like "750ms"
// or a plain millisecond value like "750".
func SlowThreshold() time.Duration {
	for _, key := range []string{"SEA_OBSERVABILITY_SLOW_THRESHOLD", "SEA_OBSERVABILITY_SLOW_THRESHOLD_MS"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
		if ms, err := strconv.ParseInt(raw, 10, 64); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return DefaultSlowThreshold
}

func TimeoutFromMillis(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func MarkBusinessError(ctx context.Context, code int, msg string, duration time.Duration, attrs ...attribute.KeyValue) {
	if code == SuccessBizCode {
		MarkDurationAndSlow(ctx, duration, SlowThreshold(), attrs...)
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	base := []attribute.KeyValue{
		attribute.Bool(AttrError, true),
		attribute.String(AttrErrorKind, ErrorKindBusiness),
		attribute.Int(AttrBizCode, code),
		attribute.String(AttrBizMsg, msg),
		attribute.Float64(AttrDuration, durationMilliseconds(duration)),
	}
	span.SetAttributes(append(base, attrs...)...)
	span.SetStatus(otelcodes.Error, msg)
}

func MarkSystemError(ctx context.Context, err error, duration time.Duration, attrs ...attribute.KeyValue) {
	msg := "system error"
	if err != nil {
		msg = err.Error()
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	base := []attribute.KeyValue{
		attribute.Bool(AttrError, true),
		attribute.String(AttrErrorKind, ErrorKindSystem),
		attribute.String(AttrBizMsg, msg),
		attribute.Float64(AttrDuration, durationMilliseconds(duration)),
	}
	span.RecordError(errors.New(msg))
	span.SetAttributes(append(base, attrs...)...)
	span.SetStatus(otelcodes.Error, msg)
}

func MarkTimeout(ctx context.Context, err error, duration time.Duration, attrs ...attribute.KeyValue) {
	msg := "timeout"
	if err != nil {
		msg = err.Error()
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	base := []attribute.KeyValue{
		attribute.Bool(AttrError, true),
		attribute.String(AttrErrorKind, ErrorKindTimeout),
		attribute.Bool(AttrTimeout, true),
		attribute.String(AttrBizMsg, msg),
		attribute.Float64(AttrDuration, durationMilliseconds(duration)),
	}
	span.RecordError(errors.New(msg))
	span.SetAttributes(append(base, attrs...)...)
	span.SetStatus(otelcodes.Error, msg)
}

func MarkDurationAndSlow(ctx context.Context, duration, slowThreshold time.Duration, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	base := []attribute.KeyValue{
		attribute.Float64(AttrDuration, durationMilliseconds(duration)),
	}
	if slowThreshold > 0 && duration > slowThreshold {
		base = append(base, attribute.Bool(AttrSlow, true))
	}
	span.SetAttributes(append(base, attrs...)...)
}

func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.DeadlineExceeded {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
