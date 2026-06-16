package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func withRecordedSpan(t *testing.T, name string, fn func(context.Context)) sdktrace.ReadOnlySpan {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	ctx, span := otel.Tracer("observability-test").Start(context.Background(), name)
	fn(ctx)
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	return spans[0]
}

func TestHTTPBusinessErrorMarksSpan(t *testing.T) {
	span := withRecordedSpan(t, "http-business-error", func(ctx context.Context) {
		handler := NewHTTPMiddleware("admin-api", 0, time.Hour)(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":40101,"msg":"password wrong","data":null}`))
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/login", nil).WithContext(ctx)
		handler(httptest.NewRecorder(), req)
	})

	assertBoolAttr(t, span, AttrError, true)
	assertStringAttr(t, span, AttrErrorKind, ErrorKindBusiness)
	assertIntAttr(t, span, AttrBizCode, 40101)
}

func TestHTTPTimeoutMarksSpanBeforeReturn(t *testing.T) {
	span := withRecordedSpan(t, "http-timeout", func(ctx context.Context) {
		handler := NewHTTPMiddleware("article-api", 5*time.Millisecond, time.Hour)(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
		})

		req := httptest.NewRequest(http.MethodGet, "/slow", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected timeout status %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	assertBoolAttr(t, span, AttrError, true)
	assertStringAttr(t, span, AttrErrorKind, ErrorKindTimeout)
	assertBoolAttr(t, span, AttrTimeout, true)
}

func TestHTTPSlowRequestMarksSpan(t *testing.T) {
	span := withRecordedSpan(t, "http-slow", func(ctx context.Context) {
		handler := NewHTTPMiddleware("favorite-api", 0, time.Millisecond)(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(3 * time.Millisecond)
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
		})

		req := httptest.NewRequest(http.MethodGet, "/favorite", nil).WithContext(ctx)
		handler(httptest.NewRecorder(), req)
	})

	assertBoolAttr(t, span, AttrSlow, true)
}

func TestRPCDeadlineExceededMarksSpan(t *testing.T) {
	span := withRecordedSpan(t, "rpc-timeout", func(ctx context.Context) {
		interceptor := NewUnaryServerInterceptor(5*time.Millisecond, time.Hour)
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/article.Article/Get"},
			func(ctx context.Context, req any) (any, error) {
				time.Sleep(30 * time.Millisecond)
				return nil, nil
			},
		)
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("expected DeadlineExceeded, got %v", err)
		}
	})

	assertBoolAttr(t, span, AttrError, true)
	assertStringAttr(t, span, AttrErrorKind, ErrorKindTimeout)
	assertBoolAttr(t, span, AttrTimeout, true)
}

func TestRPCBusinessErrorMarksSpan(t *testing.T) {
	span := withRecordedSpan(t, "rpc-business-error", func(ctx context.Context) {
		interceptor := NewUnaryServerInterceptor(0, time.Hour)
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/admin.Admin/Login"},
			func(ctx context.Context, req any) (any, error) {
				return nil, status.Error(codes.InvalidArgument, "bad request")
			},
		)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	assertBoolAttr(t, span, AttrError, true)
	assertStringAttr(t, span, AttrErrorKind, ErrorKindBusiness)
	assertIntAttr(t, span, AttrBizCode, int(codes.InvalidArgument))
}

func TestRPCSlowCallMarksSpan(t *testing.T) {
	span := withRecordedSpan(t, "rpc-slow", func(ctx context.Context) {
		interceptor := NewUnaryServerInterceptor(0, time.Millisecond)
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/favorite.Favorite/List"},
			func(ctx context.Context, req any) (any, error) {
				time.Sleep(3 * time.Millisecond)
				return "ok", nil
			},
		)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	assertBoolAttr(t, span, AttrSlow, true)
}

func assertBoolAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, expected bool) {
	t.Helper()
	value, ok := attrValue(span, key)
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if value.AsBool() != expected {
		t.Fatalf("attr %s expected %v, got %v", key, expected, value.AsBool())
	}
}

func assertStringAttr(t *testing.T, span sdktrace.ReadOnlySpan, key, expected string) {
	t.Helper()
	value, ok := attrValue(span, key)
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if value.AsString() != expected {
		t.Fatalf("attr %s expected %q, got %q", key, expected, value.AsString())
	}
}

func assertIntAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, expected int) {
	t.Helper()
	value, ok := attrValue(span, key)
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if value.AsInt64() != int64(expected) {
		t.Fatalf("attr %s expected %d, got %d", key, expected, value.AsInt64())
	}
}

func attrValue(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value, true
		}
	}
	return attribute.Value{}, false
}
